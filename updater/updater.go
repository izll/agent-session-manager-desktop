package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	RepoOwner     = "izll"
	RepoName      = "agent-session-manager-desktop"
	BinaryName    = "asmgr-desktop"
	CheckTimeout  = 5 * time.Second
	DownloadLimit = 512 << 20 // 512 MiB, including compressed release assets.
	BinaryLimit   = 256 << 20 // 256 MiB uncompressed executable limit.
	BundleLimit   = 1 << 30   // 1 GiB cumulative uncompressed app-bundle limit.
	BundleEntries = 100000    // refuse archives designed to exhaust inode space.
	// Package-manager probes and installs run behind a UI binding. Keep a
	// broken helper or package script from blocking that binding forever, and
	// cap captured output so a noisy script cannot exhaust the process memory.
	packageProbeTimeout   = 10 * time.Second
	packageInstallTimeout = 15 * time.Minute
	packageCommandWait    = 2 * time.Second
	packageOutputLimit    = 1 << 20
	stageCleanupAge       = 24 * time.Hour

	// Automatic checks are throttled to once a day, matching the TUI version.
	CheckInterval = 24 * time.Hour
	LastCheckFile = "last_update_check"
	// AvailableUpdateFile caches the last "there is a newer version" answer.
	// Without it a launch that falls inside the daily throttle shows nothing,
	// even though an update is still waiting.
	AvailableUpdateFile = "available_update"
	staleUpdateFile     = "stale_update_files.json"
	failedUpdateFile    = "failed_update_backups.json"
	installLockFile     = "update-install.lock"
	updateDownloadDir   = "updates"
	updateStageMarker   = ".asmgr-updater-stage"
	updateStageOwner    = "asmgr-desktop updater stage v1\n"
)

var windowsRuntimeDLLs = []string{
	"libportaudio.dll",
	"libgcc_s_seh-1.dll",
	"libstdc++-6.dll",
	"libwinpthread-1.dll",
}

// The executable links PortAudio dynamically on every supported Windows
// release build. Other MinGW runtime DLLs vary with the toolchain and are
// installed when present, but this one is never optional: accepting an archive
// without it produces an update that succeeds and then cannot start.
var requiredWindowsRuntimeDLLs = []string{"libportaudio.dll"}

var (
	apiBaseURL      = "https://api.github.com"
	downloadBaseURL = "https://github.com"
	checkClient     = &http.Client{Timeout: CheckTimeout}
	downloadClient  = &http.Client{Timeout: 5 * time.Minute}
	installMu       sync.Mutex
)

type boundedCommandOutput struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func (w *boundedCommandOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - len(w.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		w.data = append(w.data, p[:remaining]...)
	}
	if remaining < len(p) {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedCommandOutput) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]byte(nil), w.data...)
	if w.truncated {
		out = append(out, []byte("\n[output truncated]")...)
	}
	return out
}

func runPackageCommand(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	configurePackageCommand(cmd)
	// If a helper deliberately detaches from the process group but keeps an
	// inherited output descriptor open, Cmd.Wait would otherwise still wait for
	// that descriptor after the deadline and direct-process cancellation.
	cmd.WaitDelay = packageCommandWait
	output := &boundedCommandOutput{limit: packageOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.Bytes(), fmt.Errorf("%s timed out after %s: %w", filepath.Base(name), timeout, ctx.Err())
	}
	return output.Bytes(), err
}

// ExpectedMacTeamID is embedded by the signed macOS release build. It pins
// automatic updates to this publisher rather than accepting any application
// that happens to have a valid Developer ID signature and notarization ticket.
// Development builds leave it empty and therefore cannot replace an installed
// .app automatically; the safe fallback is a manual update.
var ExpectedMacTeamID string

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

// configDir is where the last-check timestamp lives, alongside the app's
// other state.
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agent-session-manager-desktop")
}

// ShouldCheckForUpdate reports whether enough time has passed since the last
// automatic check. Anything unreadable or unparseable means "check" — missing
// an update is worse than one extra request.
func ShouldCheckForUpdate() bool {
	dir := configDir()
	if dir == "" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(dir, LastCheckFile))
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	// A timestamp in the future (clock change, edited file) would otherwise
	// suppress checks indefinitely.
	if last.After(time.Now()) {
		return true
	}
	return time.Since(last) >= CheckInterval
}

// SaveLastCheckTime records that an automatic check just happened. Failures
// are ignored: the only consequence is checking again sooner than needed.
func SaveLastCheckTime() {
	dir := configDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(
		filepath.Join(dir, LastCheckFile),
		[]byte(time.Now().Format(time.RFC3339)),
		0o644,
	)
}

// CachedAvailableUpdate returns the version found by an earlier check, or ""
// when there is none. The UI shows this immediately at startup so a pending
// update stays visible on days when the throttle skips the network check.
//
// A cached version that is no longer newer than the running one is discarded:
// it survives the update itself, and would otherwise keep nagging.
func CachedAvailableUpdate(currentVersion string) string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, AvailableUpdateFile))
	if err != nil {
		return ""
	}
	cached := strings.TrimSpace(string(data))
	if cached == "" {
		return ""
	}
	if !isNewerVersion(cached, currentVersion) {
		ClearAvailableUpdate()
		return ""
	}
	return cached
}

// SaveAvailableUpdate records a pending update, or clears the record when the
// argument is empty.
func SaveAvailableUpdate(version string) {
	dir := configDir()
	if dir == "" {
		return
	}
	if version == "" {
		ClearAvailableUpdate()
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, AvailableUpdateFile), []byte(version), 0o644)
}

// ClearAvailableUpdate forgets a pending update — used once it is installed.
func ClearAvailableUpdate() {
	dir := configDir()
	if dir == "" {
		return
	}
	_ = os.Remove(filepath.Join(dir, AvailableUpdateFile))
}

// isNewerVersion reports whether candidate is a valid release newer than
// current. Anything unparseable counts as not newer.
func isNewerVersion(candidate, current string) bool {
	c, ok := parseSemver(strings.TrimPrefix(candidate, "v"))
	if !ok || len(c.prerelease) > 0 {
		return false
	}
	cur, ok := parseSemver(strings.TrimPrefix(current, "v"))
	if !ok {
		return true // unknown running version: trust the cached tag
	}
	return compareSemver(c, cur) > 0
}

// CheckForUpdate returns the latest tag when it is a valid semantic version
// newer than currentVersion. Invalid and pre-release "latest" tags are ignored.
// Transport and response errors are returned so callers do not mistake a
// failed check for a successful "up to date" result.
func CheckForUpdate(currentVersion string) (string, error) {
	return CheckForUpdateContext(context.Background(), currentVersion)
}

// CheckForUpdateContext is CheckForUpdate with caller-owned cancellation for
// startup checks that must not outlive application shutdown.
func CheckForUpdateContext(ctx context.Context, currentVersion string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBaseURL, RepoOwner, RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("update check request failed: %w", err)
	}
	resp, err := checkClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("update check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check failed: GitHub returned HTTP %d", resp.StatusCode)
	}

	var release GitHubRelease
	limited := io.LimitReader(resp.Body, 1<<20)
	if err := json.NewDecoder(limited).Decode(&release); err != nil {
		return "", fmt.Errorf("decode update response: %w", err)
	}
	current, ok := parseSemver(currentVersion)
	if !ok {
		return "", fmt.Errorf("invalid current version %q", currentVersion)
	}
	latest, ok := parseSemver(release.TagName)
	if !ok || latest.prerelease != "" {
		return "", fmt.Errorf("invalid stable release tag %q", release.TagName)
	}
	if compareSemver(latest, current) > 0 {
		return release.TagName, nil
	}
	return "", nil
}

// RefreshAvailableUpdate performs a check and persists its result only after a
// successful response. A transient GitHub/network failure therefore preserves
// both a previously advertised update and the ability to retry immediately.
func RefreshAvailableUpdate(currentVersion string) (string, error) {
	return RefreshAvailableUpdateContext(context.Background(), currentVersion)
}

// RefreshAvailableUpdateContext is RefreshAvailableUpdate with caller-owned
// cancellation. State is persisted only after the check completes normally.
func RefreshAvailableUpdateContext(ctx context.Context, currentVersion string) (string, error) {
	latest, err := CheckForUpdateContext(ctx, currentVersion)
	if err != nil {
		return "", err
	}
	SaveLastCheckTime()
	SaveAvailableUpdate(latest)
	return latest, nil
}

type semVersion struct {
	major, minor, patch string
	prerelease          string
}

func parseSemver(value string) (semVersion, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if plus := strings.IndexByte(v, '+'); plus >= 0 {
		if plus == len(v)-1 || !validIdentifiers(v[plus+1:], false) {
			return semVersion{}, false
		}
		v = v[:plus]
	}
	pre := ""
	if dash := strings.IndexByte(v, '-'); dash >= 0 {
		pre = v[dash+1:]
		v = v[:dash]
		if !validIdentifiers(pre, true) {
			return semVersion{}, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semVersion{}, false
	}
	for _, part := range parts {
		if !validNumericIdentifier(part) {
			return semVersion{}, false
		}
	}
	return semVersion{major: parts[0], minor: parts[1], patch: parts[2], prerelease: pre}, true
}

func validNumericIdentifier(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validIdentifiers(s string, enforceNumericLeadingZero bool) bool {
	for _, identifier := range strings.Split(s, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, r := range identifier {
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-') {
				return false
			}
			if r < '0' || r > '9' {
				numeric = false
			}
		}
		if enforceNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func compareSemver(a, b semVersion) int {
	for _, pair := range [][2]string{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if cmp := compareNumeric(pair[0], pair[1]); cmp != 0 {
			return cmp
		}
	}
	if a.prerelease == b.prerelease {
		return 0
	}
	if a.prerelease == "" {
		return 1
	}
	if b.prerelease == "" {
		return -1
	}
	aParts, bParts := strings.Split(a.prerelease, "."), strings.Split(b.prerelease, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] == bParts[i] {
			continue
		}
		aNum, bNum := allDigits(aParts[i]), allDigits(bParts[i])
		switch {
		case aNum && bNum:
			return compareNumeric(aParts[i], bParts[i])
		case aNum:
			return -1
		case bNum:
			return 1
		default:
			return strings.Compare(aParts[i], bParts[i])
		}
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	return 1
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func validateReleaseVersion(version string) error {
	parsed, ok := parseSemver(version)
	if !ok || parsed.prerelease != "" {
		return fmt.Errorf("invalid stable release version %q", version)
	}
	return nil
}

func releaseURL(version, filename string) string {
	return fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", downloadBaseURL, RepoOwner, RepoName, version, filename)
}

func readChecksum(url, filename string) (string, error) {
	return readChecksumContext(context.Background(), url, filename)
}

func readChecksumContext(ctx context.Context, url, filename string) (string, error) {
	return readChecksumContextWithClient(ctx, downloadClient, url, filename)
}

func readChecksumContextWithClient(ctx context.Context, client *http.Client, url, filename string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+".sha256", nil)
	if err != nil {
		return "", fmt.Errorf("checksum request failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checksum download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: HTTP %d", resp.StatusCode)
	}
	line, err := bufio.NewReader(io.LimitReader(resp.Body, 4097)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("checksum read failed: %w", err)
	}
	fields := strings.Fields(line)
	if len(fields) < 1 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("invalid checksum file")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", fmt.Errorf("invalid checksum: %w", err)
	}
	if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") != filename {
		return "", fmt.Errorf("checksum is for %q, expected %q", fields[1], filename)
	}
	return strings.ToLower(fields[0]), nil
}

func downloadVerifiedAsset(version, filename, tempPattern string) (path string, err error) {
	return downloadVerifiedAssetContext(context.Background(), version, filename, tempPattern)
}

func downloadVerifiedAssetContext(ctx context.Context, version, filename, tempPattern string) (path string, err error) {
	path, _, err = downloadVerifiedAssetContextWithChecksum(ctx, version, filename, tempPattern)
	return path, err
}

// downloadVerifiedAssetContextWithChecksum returns the checksum obtained over
// HTTPS alongside the verified file. The package-update privilege boundary
// needs this original value: hashing the user-writable temporary file again
// immediately before pkexec would merely trust an attacker's replacement.
func downloadVerifiedAssetContextWithChecksum(ctx context.Context, version, filename, tempPattern string) (path, trustedChecksum string, err error) {
	if err := validateReleaseVersion(version); err != nil {
		return "", "", err
	}
	url := releaseURL(version, filename)
	expected, err := readChecksumContext(ctx, url, filename)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("download request failed: %w", err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > DownloadLimit {
		return "", "", fmt.Errorf("download is too large: %d bytes", resp.ContentLength)
	}

	downloadDir, err := secureUpdateDownloadDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to create secure download directory: %w", err)
	}
	out, err := os.CreateTemp(downloadDir, tempPattern)
	if err != nil {
		return "", "", fmt.Errorf("failed to create secure temporary file: %w", err)
	}
	path = out.Name()
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, DownloadLimit+1))
	if err != nil {
		return "", "", fmt.Errorf("failed to save download: %w", err)
	}
	if n > DownloadLimit {
		return "", "", fmt.Errorf("download exceeds %d byte limit", DownloadLimit)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return "", "", fmt.Errorf("checksum mismatch: got %s, expected %s", actual, expected)
	}
	if err := out.Sync(); err != nil {
		return "", "", fmt.Errorf("failed to sync download: %w", err)
	}
	return path, expected, nil
}

func packageArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func DownloadDeb(version string) (string, error) {
	filename := fmt.Sprintf("%s_%s_linux_%s.deb", BinaryName, strings.TrimPrefix(version, "v"), packageArch())
	return downloadVerifiedAsset(version, filename, BinaryName+"-*.deb")
}

func DownloadRpm(version string) (string, error) {
	filename := fmt.Sprintf("%s_%s_linux_%s.rpm", BinaryName, strings.TrimPrefix(version, "v"), packageArch())
	return downloadVerifiedAsset(version, filename, BinaryName+"-*.rpm")
}

// IsPackageManaged detects the Linux packages produced by this repository.
func IsPackageManaged() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return false
	}
	if _, err := exec.LookPath("dpkg-query"); err == nil {
		output, queryErr := runPackageCommand(packageProbeTimeout, "dpkg-query", "--search", execPath)
		if queryErr == nil && strings.HasPrefix(strings.TrimSpace(string(output)), BinaryName+":") {
			return true
		}
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		_, err := runPackageCommand(packageProbeTimeout, "rpm", "-qf", execPath)
		return err == nil
	}
	return false
}

func extractExecutable(archivePath, expected string, out *os.File) error {
	return extractExecutableWithLimits(archivePath, expected, out, archiveLimits{bytes: BundleLimit, entries: BundleEntries})
}

func extractExecutableWithLimits(archivePath, expected string, out *os.File, limits archiveLimits) error {
	in, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	defer gz.Close()

	found := false
	entryCount := 0
	var total int64
	tarReader := tar.NewReader(gz)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("failed to read archive: %w", nextErr)
		}
		entryCount++
		if entryCount > limits.entries {
			return fmt.Errorf("archive contains too many entries")
		}
		if header.FileInfo().Mode().IsRegular() {
			if header.Size < 0 || header.Size > limits.bytes-total {
				return fmt.Errorf("archive exceeds the cumulative uncompressed size limit")
			}
			total += header.Size
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != expected {
			continue
		}
		if found || !header.FileInfo().Mode().IsRegular() || header.Size < 0 || header.Size > BinaryLimit {
			return fmt.Errorf("invalid executable entry in archive")
		}
		if _, err := io.CopyN(out, tarReader, header.Size); err != nil {
			return fmt.Errorf("failed to extract executable: %w", err)
		}
		found = true
	}
	if !found {
		return fmt.Errorf("executable %q not found in archive", expected)
	}
	return out.Sync()
}

// bundleRootFor returns the .app directory containing an executable, or "" if
// the executable is not inside one.
//
// A bundle's layout is fixed: Foo.app/Contents/MacOS/foo. Rather than trusting
// that shape blindly, this walks up and checks for the .app suffix — a binary
// run straight out of a build directory has no bundle, and must not have three
// unrelated parent directories mistaken for one.
func bundleRootFor(execPath string) string {
	dir := filepath.Dir(execPath) // .../Contents/MacOS
	for i := 0; i < 3 && dir != "" && dir != string(filepath.Separator); i++ {
		if strings.EqualFold(filepath.Ext(dir), ".app") {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// findBundleIn returns the single .app directory at the top of dir.
//
// Used when the archive's bundle is not named the same as the installed one,
// which happens across the rename. Refuses to guess when there is more than
// one: replacing the wrong directory is worse than failing with a message.
func findBundleIn(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found string
	for _, e := range entries {
		if !e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".app") {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("the archive holds more than one .app bundle")
		}
		found = filepath.Join(dir, e.Name())
	}
	if found == "" {
		return "", fmt.Errorf("no .app bundle in the archive")
	}
	return found, nil
}

// installBundleUpdate replaces a macOS .app bundle in its entirety.
//
// The bundle is a directory tree — executable, Info.plist, resources and a code
// signature that covers all of it — so replacing only the binary would leave a
// bundle whose signature no longer matches its contents, which Gatekeeper
// rejects. The archive already contains the whole .app, so the update unpacks
// it beside the installed one and swaps the two directories.
//
// The swap is two renames within the same parent, which is as close to atomic
// as this gets: if the second fails the first is undone, so the worst case
// leaves the previous version in place rather than no version at all.
func installBundleUpdate(ctx context.Context, version string, critical func(func() error) error) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}
	bundle := bundleRootFor(execPath)
	if bundle == "" {
		return fmt.Errorf("this build is not running from an .app bundle; download %s %s from the release page and replace the application", BinaryName, version)
	}

	arch := runtime.GOARCH
	filename := fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, strings.TrimPrefix(version, "v"), runtime.GOOS, arch)
	archivePath, err := downloadVerifiedAssetContext(ctx, version, filename, BinaryName+"-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	// Unpack beside the installed bundle: same filesystem, so the swap below is
	// a rename rather than a copy, and /Applications stays untouched until the
	// new bundle is complete on disk.
	parent := filepath.Dir(bundle)
	stageDir, err := createUpdateStageDir(parent, "."+BinaryName+"-update-*")
	if err != nil {
		return fmt.Errorf("cannot stage update beside the application: %w", err)
	}
	defer os.RemoveAll(stageDir)

	if err := extractBundle(archivePath, stageDir); err != nil {
		return err
	}
	// Find the .app in the archive rather than assuming it matches the one on
	// disk. The bundle was renamed from asmgr-desktop.app to the product name
	// (macOS shows the DIRECTORY name in Finder and Launchpad, whatever
	// CFBundleDisplayName says), so an older install updating to a newer
	// release is looking for a name the archive no longer uses.
	staged := filepath.Join(stageDir, filepath.Base(bundle))
	if _, err := os.Lstat(staged); os.IsNotExist(err) {
		found, findErr := findBundleIn(stageDir)
		if findErr != nil {
			return fmt.Errorf("the archive did not contain an .app bundle: %w", findErr)
		}
		staged = found
	} else if err != nil {
		return fmt.Errorf("cannot inspect staged application bundle: %w", err)
	}
	if err := validateBundleDirectory(staged); err != nil {
		return err
	}
	if err := verifyStagedBundle(staged); err != nil {
		return err
	}

	// Install under the archive's own name, so an update also carries the
	// rename across: macOS shows the directory name in Finder and Launchpad, so
	// keeping the old path would leave the app displayed as "asmgr-desktop"
	// forever. Same directory, so this stays a rename within one filesystem.
	target := filepath.Join(filepath.Dir(bundle), filepath.Base(staged))

	return withInstallLock(func() error {
		// Revalidate under the install lock. Downloading and extraction are safe to
		// overlap across processes; only this filesystem mutation must serialize.
		if err := validateBundleDirectory(bundle); err != nil {
			return fmt.Errorf("installed application bundle is no longer available")
		}
		if err := validateBundleDirectory(staged); err != nil {
			return fmt.Errorf("staged application bundle is no longer available")
		}
		return critical(func() error {
			return swapBundle(bundle, staged, target)
		})
	})
}

// validateBundleDirectory rejects a symlink even when it resolves to a real
// directory. swapBundle renames the path itself: accepting a staged symlink
// would install only that link while its relative target remains in stageDir
// and is deleted immediately afterwards, leaving a broken application.
func validateBundleDirectory(bundle string) error {
	info, err := os.Lstat(bundle)
	if err != nil {
		return fmt.Errorf("cannot inspect application bundle: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("application bundle is not a real directory")
	}
	return nil
}

type bundleVerificationRunner func(name string, args ...string) ([]byte, error)

// verifyStagedBundle checks the independent trust signal carried by a macOS
// release. The archive and its .sha256 file are downloaded from the same
// release, so the checksum catches corruption but cannot authenticate one if
// both assets were replaced. Developer ID signing and notarization do provide
// that independent signal; refuse to swap the installed app unless both are
// accepted by the host OS.
func verifyStagedBundle(bundle string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return verifyStagedBundleWith(bundle, ExpectedMacTeamID, func(name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	})
}

func verifyStagedBundleWith(bundle, expectedTeamID string, run bundleVerificationRunner) error {
	if !validMacTeamID(expectedTeamID) {
		return fmt.Errorf("automatic macOS updates are disabled because the expected publisher identity is not configured; install this update manually")
	}
	checks := []struct {
		name string
		args []string
		what string
	}{
		{
			name: "/usr/bin/codesign",
			args: []string{"--verify", "--deep", "--strict", bundle},
			what: "Developer ID signature",
		},
	}
	for _, check := range checks {
		out, err := run(check.name, check.args...)
		if err == nil {
			continue
		}
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("staged application failed %s: %s", check.what, detail)
	}
	details, err := run("/usr/bin/codesign", "-dv", "--verbose=4", bundle)
	if err != nil {
		detail := strings.TrimSpace(string(details))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("cannot read staged application publisher identity: %s", detail)
	}
	teamID := macTeamIDFromCodesign(details)
	if teamID == "" {
		return fmt.Errorf("staged application has no valid Developer ID TeamIdentifier")
	}
	if teamID != expectedTeamID {
		return fmt.Errorf("staged application publisher mismatch: got TeamIdentifier %q, expected %q", teamID, expectedTeamID)
	}
	if out, err := run("/usr/sbin/spctl", "--assess", "--type", "execute", bundle); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("staged application failed Gatekeeper/notarization assessment: %s", detail)
	}
	return nil
}

func validMacTeamID(teamID string) bool {
	if len(teamID) != 10 {
		return false
	}
	for _, r := range teamID {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func macTeamIDFromCodesign(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		teamID, ok := strings.CutPrefix(strings.TrimSpace(line), "TeamIdentifier=")
		if ok && validMacTeamID(teamID) {
			return teamID
		}
	}
	return ""
}

func swapBundle(bundle, staged, target string) error {
	return swapBundleWithOps(bundle, staged, target, os.Rename, os.RemoveAll)
}

func swapBundleWithOps(bundle, staged, target string, rename func(string, string) error, removeAll func(string) error) error {
	parent := filepath.Dir(bundle)
	backupDir, err := os.MkdirTemp(parent, "."+BinaryName+"-update-rollback-*")
	if err != nil {
		return fmt.Errorf("cannot create application rollback directory: %w", err)
	}
	old := filepath.Join(backupDir, filepath.Base(bundle))
	if err := rename(bundle, old); err != nil {
		_ = removeAll(backupDir)
		return fmt.Errorf("cannot move the old application aside: %w", err)
	}
	if installErr := rename(staged, target); installErr != nil {
		installErr = fmt.Errorf("cannot install the new application: %w", installErr)
		if rollbackErr := rename(old, bundle); rollbackErr != nil {
			recordFailedUpdatePath(backupDir)
			return errors.Join(installErr,
				fmt.Errorf("application rollback failed; backup preserved at %s: %w", backupDir, rollbackErr))
		}
		if err := removeAll(backupDir); err != nil {
			recordStaleUpdatePath(backupDir)
		}
		return installErr
	}
	// The running process still has files open inside the old bundle, so this
	// may fail; CleanStaleUpdateFiles clears it on the next start.
	if err := removeAll(backupDir); err != nil {
		recordStaleUpdatePath(backupDir)
	}
	return nil
}

// extractBundle unpacks the .app tree from a release archive into destDir.
//
// Entries are confined to destDir explicitly: a path traversal in an archive is
// the classic way to have an "update" write anywhere on the filesystem, and the
// checksum only proves the file matches the release, not that the release is
// well-formed.
func extractBundle(archivePath, destDir string) error {
	in, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var totalSize int64
	entryCount := 0
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			return nil
		}
		if nextErr != nil {
			return fmt.Errorf("failed to read archive: %w", nextErr)
		}
		entryCount++
		if entryCount > BundleEntries {
			return fmt.Errorf("archive contains too many entries")
		}
		target, err := safeJoin(destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > BinaryLimit {
				return fmt.Errorf("invalid entry %q in archive", header.Name)
			}
			if header.Size > BundleLimit-totalSize {
				return fmt.Errorf("archive exceeds the uncompressed bundle limit")
			}
			totalSize += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			// Preserve the executable bit: the binary inside Contents/MacOS is
			// unusable without it, and so is anything in Contents/Resources
			// that the app shells out to.
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.CopyN(f, tr, header.Size); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to extract %s: %w", header.Name, err)
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Frameworks are conventionally symlinked inside a bundle. The link
			// target is resolved from the link's actual extraction directory.
			// Do not clean header.Linkname before that check: os.Symlink receives
			// the raw value, so cleaning a leading "../../" only for validation
			// would approve a link that points outside the staging directory.
			if _, err := safeSymlinkTarget(destDir, target, header.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
		// Anything else (devices, fifos) has no place in an app bundle and is
		// skipped rather than trusted.
	}
}

// safeJoin resolves an archive entry against destDir, refusing anything that
// would land outside it.
func safeJoin(destDir, name string) (string, error) {
	name = filepath.FromSlash(name)
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	target := filepath.Join(destDir, name)
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}

// safeSymlinkTarget validates the raw target that will be passed to
// os.Symlink. linkPath is already confined to destDir by safeJoin. Resolving
// from its real parent mirrors the kernel's symlink semantics, including every
// ".." component, instead of validating a separately cleaned path.
func safeSymlinkTarget(destDir, linkPath, linkName string) (string, error) {
	linkName = filepath.FromSlash(linkName)
	if filepath.IsAbs(linkName) {
		return "", fmt.Errorf("archive symlink %q escapes the destination", linkName)
	}
	target := filepath.Join(filepath.Dir(linkPath), linkName)
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive symlink %q escapes the destination", linkName)
	}
	return target, nil
}

// CleanStaleUpdateFiles removes only paths that this updater is known to have
// created. A suffix scan is unsafe here: the executable often lives in ~/bin,
// and a macOS bundle usually lives directly in /Applications, where unrelated
// files and directories ending in ".old" are not ours to delete.
//
// Windows refuses to delete a file that is still mapped into a running process,
// so the update has to leave the previous executable and libraries in place and
// clear them later. "Later" is the next start, when nothing holds them open.
//
// Every failure is ignored on purpose: a leftover file is harmless clutter, and
// there is no version of "could not tidy up" worth interrupting a launch for.
func CleanStaleUpdateFiles() {
	// Cleanup is startup housekeeping, never a reason to hold the application
	// launch behind another process's update download or privilege prompt. If a
	// final install currently owns the lock, that install records anything it
	// leaves behind and the next launch will retry cleanup.
	_, _ = withInstallTryLock(func() error {
		cleanStaleUpdateDownloads()
		execPath, err := os.Executable()
		if err != nil {
			return nil
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return nil
		}
		cleanStaleUpdateFilesFor(execPath)
		return nil
	})
}

func secureUpdateDownloadDir() (string, error) {
	dir := configDir()
	if dir == "" {
		return "", fmt.Errorf("cannot locate updater state directory")
	}
	dir = filepath.Join(dir, updateDownloadDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func cleanStaleUpdateDownloads() {
	base := configDir()
	if base == "" {
		return
	}
	dir := filepath.Join(base, updateDownloadDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, BinaryName+"-") ||
			(!strings.HasSuffix(name, ".deb") && !strings.HasSuffix(name, ".rpm") &&
				!strings.HasSuffix(name, ".tar.gz")) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) < stageCleanupAge {
			continue
		}
		_ = os.Remove(path)
	}
}

func cleanStaleUpdateFilesFor(execPath string) {
	cleanStaleUpdateFilesIn(filepath.Dir(execPath))
	cleanMarkedUpdateStagesIn(filepath.Dir(execPath))

	bundle := bundleRootFor(execPath)
	if bundle != "" {
		cleanMarkedUpdateStagesIn(filepath.Dir(bundle))
	}
	cleanRecordedUpdateFiles(execPath, bundle)
}

// cleanStaleUpdateFilesIn removes regular files with the exact legacy names
// produced by older updater versions. It deliberately does not enumerate by
// suffix and never recursively removes a directory without an ownership marker:
// a user may reasonably keep a manual macOS bundle backup as Foo.app.old.
func cleanStaleUpdateFilesIn(dir string) {
	names := []string{BinaryName + ".old", BinaryName + ".exe.old", BinaryName + ".app.old"}
	for _, dll := range windowsRuntimeDLLs {
		names = append(names, dll+".old")
	}
	for _, name := range names {
		candidate := filepath.Join(dir, name)
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		_ = os.Remove(candidate)
	}
}

func createUpdateStageDir(parent, pattern string) (string, error) {
	dir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, updateStageMarker), []byte(updateStageOwner), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// cleanMarkedUpdateStagesIn recovers staging space after a crash or power
// loss. A name prefix alone is not ownership proof in directories such as
// /Applications or ~/bin, so only a real directory carrying our exact marker
// is eligible for removal.
func cleanMarkedUpdateStagesIn(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "."+BinaryName+"-update-") &&
			!strings.HasPrefix(name, "."+BinaryName+"-dll-stage-") {
			continue
		}
		stagePath := filepath.Join(dir, name)
		info, err := os.Lstat(stagePath)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		markerPath := filepath.Join(stagePath, updateStageMarker)
		markerInfo, err := os.Lstat(markerPath)
		if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		// Downloading and extraction deliberately happen outside the install
		// lock, so another live instance may own a fresh stage directory.
		// A stage is only abandoned after a generous age threshold.
		if age := time.Since(markerInfo.ModTime()); age < stageCleanupAge {
			continue
		}
		marker, err := os.Open(markerPath)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(marker, int64(len(updateStageOwner)+1)))
		closeErr := marker.Close()
		if readErr != nil || closeErr != nil || string(data) != updateStageOwner {
			continue
		}
		_ = os.RemoveAll(stagePath)
	}
}

func withInstallLock(action func() error) error {
	installMu.Lock()
	defer installMu.Unlock()
	dir := configDir()
	if dir == "" {
		return fmt.Errorf("cannot locate updater state directory")
	}
	return withCrossProcessFileLock(filepath.Join(dir, installLockFile), action)
}

func withInstallTryLock(action func() error) (bool, error) {
	if !installMu.TryLock() {
		return false, nil
	}
	defer installMu.Unlock()
	dir := configDir()
	if dir == "" {
		return false, fmt.Errorf("cannot locate updater state directory")
	}
	return withCrossProcessFileTryLock(filepath.Join(dir, installLockFile), action)
}

// withInstallLockContext waits for the updater transaction lock without
// making application shutdown wait behind another instance. The blocking lock
// remains appropriate once a mutation has begun; callers use this helper only
// while their work is still safe to cancel.
func withInstallLockContext(ctx context.Context, action func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		locked, err := withInstallTryLock(action)
		if err != nil {
			return err
		}
		if locked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func recordStaleUpdatePath(stalePath string) {
	recordUpdatePath(staleUpdateFile, stalePath)
}

func recordFailedUpdatePath(backupPath string) {
	recordUpdatePath(failedUpdateFile, backupPath)
}

func recordUpdatePath(manifestName, stalePath string) {
	dir := configDir()
	if dir == "" || stalePath == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	manifest := filepath.Join(dir, manifestName)
	var paths []string
	if raw, err := os.ReadFile(manifest); err == nil {
		_ = json.Unmarshal(raw, &paths)
	}
	for _, existing := range paths {
		if existing == stalePath {
			return
		}
	}
	paths = append(paths, stalePath)
	writeStaleUpdateManifest(manifest, paths)
}

func writeStaleUpdateManifest(manifest string, paths []string) {
	raw, err := json.Marshal(paths)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(manifest), ".stale-updates-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// os.Rename cannot replace an existing file on Windows. The manifest is
		// normally updated, not created: every additional rollback directory and
		// every partial-cleanup retry must replace the previous JSON atomically.
		_ = replaceUpdateManifestFile(tmpPath, manifest)
	}
}

func cleanRecordedUpdateFiles(execPath, bundle string) {
	dir := configDir()
	if dir == "" {
		return
	}
	manifest := filepath.Join(dir, staleUpdateFile)
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return
	}
	var paths []string
	if json.Unmarshal(raw, &paths) != nil {
		return
	}
	allowedParents := map[string]bool{filepath.Clean(filepath.Dir(execPath)): true}
	if bundle != "" {
		allowedParents[filepath.Clean(filepath.Dir(bundle))] = true
	}
	remaining := paths[:0]
	for _, stalePath := range paths {
		clean := filepath.Clean(stalePath)
		base := filepath.Base(clean)
		owned := allowedParents[filepath.Dir(clean)] && strings.HasPrefix(base, "."+BinaryName+"-update-rollback-")
		if !owned || os.RemoveAll(clean) != nil {
			remaining = append(remaining, stalePath)
		}
	}
	if len(remaining) == 0 {
		_ = os.Remove(manifest)
		return
	}
	writeStaleUpdateManifest(manifest, remaining)
}

// updateSidecarDLLs refreshes the runtime libraries that ship beside the
// Windows executable, from the same archive the new binary came from.
//
// Windows will not let a loaded DLL be overwritten — measured: writing over one
// fails with "Access is denied" — but it does allow renaming it out of the way
// and dropping a replacement in its place. That is the same move replaceExecutable
// makes for the exe, and it is why the stale copy is left behind as .old rather
// than deleted: deleting it also fails while the process holds it open.
//
// Failures here abort the update, deliberately. A new binary beside stale
// libraries may simply refuse to start, and that failure would surface after a
// restart with no obvious cause; refusing now leaves a working installation.
type archiveLimits struct {
	bytes   int64
	entries int
}

type stagedInstall struct {
	target string
	staged string
}

func stageSidecarDLLs(archivePath, destDir string) ([]stagedInstall, func(), error) {
	return stageSidecarDLLsWithLimits(archivePath, destDir, archiveLimits{bytes: BundleLimit, entries: BundleEntries})
}

func stageSidecarDLLsWithLimits(archivePath, destDir string, limits archiveLimits) ([]stagedInstall, func(), error) {
	in, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decompress: %w", err)
	}
	defer gz.Close()
	stageDir, err := createUpdateStageDir(destDir, "."+BinaryName+"-dll-stage-*")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot stage runtime libraries: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stageDir) }

	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	var total int64
	entryCount := 0
	var staged []stagedInstall
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to read archive: %w", nextErr)
		}
		entryCount++
		if entryCount > limits.entries {
			cleanup()
			return nil, nil, fmt.Errorf("archive contains too many entries")
		}
		if header.FileInfo().Mode().IsRegular() {
			if header.Size < 0 || header.Size > limits.bytes-total {
				cleanup()
				return nil, nil, fmt.Errorf("archive exceeds the cumulative uncompressed size limit")
			}
			total += header.Size
		}
		name := path.Base(filepath.ToSlash(header.Name))
		if !strings.EqualFold(filepath.Ext(name), ".dll") {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			cleanup()
			return nil, nil, fmt.Errorf("archive contains duplicate runtime library %q", name)
		}
		seen[key] = true
		if !header.FileInfo().Mode().IsRegular() || header.Size < 0 || header.Size > BinaryLimit {
			cleanup()
			return nil, nil, fmt.Errorf("invalid library entry %q in archive", name)
		}
		stagedPath := filepath.Join(stageDir, name)
		file, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("cannot stage %s: %w", name, err)
		}
		_, copyErr := io.CopyN(file, tr, header.Size)
		if copyErr == nil {
			copyErr = file.Sync()
		}
		if closeErr := file.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("cannot stage %s: %w", name, copyErr)
		}
		staged = append(staged, stagedInstall{target: filepath.Join(destDir, name), staged: stagedPath})
	}
	return staged, cleanup, nil
}

func updateSidecarDLLs(archivePath, destDir string) error {
	files, cleanup, err := stageSidecarDLLs(archivePath, destDir)
	if err != nil {
		return err
	}
	defer cleanup()
	return installTransaction(files)
}

func validateRequiredSidecarDLLs(files []stagedInstall, required []string) error {
	present := make(map[string]bool, len(files))
	for _, file := range files {
		present[strings.ToLower(filepath.Base(file.target))] = true
	}
	for _, name := range required {
		if !present[strings.ToLower(name)] {
			return fmt.Errorf("update archive is missing required runtime library %q", name)
		}
	}
	return nil
}

type installedStep struct {
	file        stagedInstall
	backup      string
	hadOriginal bool
	installed   bool
}

func installTransaction(files []stagedInstall) error {
	return installTransactionWithRename(files, os.Rename)
}

func installTransactionWithRename(files []stagedInstall, rename func(string, string) error) error {
	if len(files) == 0 {
		return nil
	}
	parent := filepath.Clean(filepath.Dir(files[0].target))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if filepath.Clean(filepath.Dir(file.target)) != parent {
			return fmt.Errorf("update transaction spans multiple directories")
		}
		key := strings.ToLower(filepath.Base(file.target))
		if seen[key] {
			return fmt.Errorf("update transaction contains duplicate target %q", filepath.Base(file.target))
		}
		seen[key] = true
		info, err := os.Stat(file.staged)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("staged update file %q is unavailable", filepath.Base(file.target))
		}
		if current, err := os.Stat(file.target); err == nil && !current.Mode().IsRegular() {
			return fmt.Errorf("update target %q is not a regular file", filepath.Base(file.target))
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect update target %q: %w", filepath.Base(file.target), err)
		}
	}

	backupDir, err := os.MkdirTemp(parent, "."+BinaryName+"-update-rollback-*")
	if err != nil {
		return fmt.Errorf("cannot create update rollback directory: %w", err)
	}
	steps := make([]installedStep, 0, len(files))
	rollback := func(cause error) error {
		var rollbackErrs []error
		for i := len(steps) - 1; i >= 0; i-- {
			step := steps[i]
			if step.installed {
				if err := os.Remove(step.file.target); err != nil && !os.IsNotExist(err) {
					rollbackErrs = append(rollbackErrs, err)
				}
			}
			if step.hadOriginal {
				if err := rename(step.backup, step.file.target); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("restore %s from %s: %w", filepath.Base(step.file.target), step.backup, err))
				}
			}
		}
		if len(rollbackErrs) != 0 {
			// At least one original could not be put back. Those backup files are
			// now the only recoverable copy, so never run RemoveAll here and never
			// put this directory on the automatic-cleanup manifest.
			recordFailedUpdatePath(backupDir)
			preserved := fmt.Errorf("update rollback failed; backups preserved at %s", backupDir)
			return errors.Join(append([]error{cause, preserved}, rollbackErrs...)...)
		}
		if err := os.RemoveAll(backupDir); err != nil {
			recordStaleUpdatePath(backupDir)
		}
		return cause
	}

	for i, file := range files {
		step := installedStep{file: file, backup: filepath.Join(backupDir, fmt.Sprintf("%04d-%s", i, filepath.Base(file.target)))}
		if _, err := os.Stat(file.target); err == nil {
			if err := rename(file.target, step.backup); err != nil {
				return rollback(fmt.Errorf("cannot move %s aside: %w", filepath.Base(file.target), err))
			}
			step.hadOriginal = true
		}
		steps = append(steps, step)
		if err := rename(file.staged, file.target); err != nil {
			return rollback(fmt.Errorf("cannot install %s: %w", filepath.Base(file.target), err))
		}
		steps[len(steps)-1].installed = true
	}

	if err := os.RemoveAll(backupDir); err != nil {
		// Windows keeps the old executable and loaded DLLs mapped until this
		// process exits. Record this exact updater-created directory for the next
		// launch; never rediscover it with a broad suffix/prefix deletion scan.
		recordStaleUpdatePath(backupDir)
	}
	return nil
}

func replaceExecutable(execPath, stagedPath string) error {
	return installTransaction([]stagedInstall{{target: execPath, staged: stagedPath}})
}

// installPackageUpdate upgrades a .deb/.rpm installation in place.
//
// The TUI can shell out to `sudo dpkg -i` because it owns a terminal to type a
// password into; a GUI has no controlling TTY, so it asks PolicyKit instead —
// pkexec shows the desktop's own authentication dialog. Without pkexec there
// is no way to prompt, so the user is told what to run.
func installPackageUpdate(ctx context.Context, version string, critical func(func() error) error) error {
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return fmt.Errorf("this installation is managed by the system package manager, and pkexec is not available to ask for permission; install %s %s with: sudo %s",
			BinaryName, version, manualInstallHint(version))
	}

	pkgPath, packageKind, trustedChecksum, err := downloadPackageForContext(ctx, version)
	if err != nil {
		return err
	}
	defer os.Remove(pkgPath)
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the package-owned updater executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(execPath); resolveErr == nil {
		execPath = resolved
	}
	args := privilegedPackageHelperArgs(execPath, pkgPath, trustedChecksum, packageKind, version)
	out, err := runPrivilegedPackageInstall(ctx, pkexec, args, func(action func() error) error {
		// Authentication and root-owned staging are preparation, not mutation.
		// Acquire the global lock only after the helper is verified and ready, and
		// keep lock waiting cancellable so shutdown cannot hang behind another
		// application instance. The lock is retained across the critical package
		// manager transaction itself.
		return withInstallLockContext(ctx, func() error {
			// The package type was selected before the download. Confirm at the
			// mutation boundary that this executable is still package-owned.
			if !IsPackageManaged() {
				return fmt.Errorf("installation type changed while the update was downloading; retry the update")
			}
			return critical(action)
		})
	})
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// 126/127 are pkexec's own codes for "dismissed" and "not authorised".
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 126 || exitErr.ExitCode() == 127) {
			return fmt.Errorf("the update was not authorised")
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("package installation failed: %s", msg)
	}
	return nil
}

// downloadPackageFor fetches the package matching this installation and
// returns it with the command needed to install it.
func downloadPackageFor(version string) (path, packageKind, trustedChecksum string, err error) {
	return downloadPackageForContext(context.Background(), version)
}

func downloadPackageForContext(ctx context.Context, version string) (path, packageKind, trustedChecksum string, err error) {
	if isDpkgInstall() {
		filename := fmt.Sprintf("%s_%s_linux_%s.deb", BinaryName, strings.TrimPrefix(version, "v"), packageArch())
		p, checksum, derr := downloadVerifiedAssetContextWithChecksum(ctx, version, filename, BinaryName+"-*.deb")
		if derr != nil {
			return "", "", "", derr
		}
		return p, "deb", checksum, nil
	}
	filename := fmt.Sprintf("%s_%s_linux_%s.rpm", BinaryName, strings.TrimPrefix(version, "v"), packageArch())
	p, checksum, rerr := downloadVerifiedAssetContextWithChecksum(ctx, version, filename, BinaryName+"-*.rpm")
	if rerr != nil {
		return "", "", "", rerr
	}
	return p, "rpm", checksum, nil
}

// isDpkgInstall reports whether dpkg owns this executable.
func isDpkgInstall() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, rerr := filepath.EvalSymlinks(execPath); rerr == nil {
		execPath = resolved
	}
	if _, lerr := exec.LookPath("dpkg-query"); lerr != nil {
		return false
	}
	out, qerr := runPackageCommand(packageProbeTimeout, "dpkg-query", "--search", execPath)
	return qerr == nil && strings.HasPrefix(strings.TrimSpace(string(out)), BinaryName+":")
}

// manualInstallHint names the command that would do the job by hand.
func manualInstallHint(version string) string {
	v := strings.TrimPrefix(version, "v")
	if isDpkgInstall() {
		return fmt.Sprintf("dpkg -i %s_%s_linux_%s.deb", BinaryName, v, packageArch())
	}
	return fmt.Sprintf("rpm -U %s_%s_linux_%s.rpm", BinaryName, v, packageArch())
}

// DownloadAndInstall installs an update. A user-local install is replaced in
// place; a distro package is upgraded through PolicyKit (see
// installPackageUpdate).
func DownloadAndInstall(version string) error {
	return DownloadAndInstallContext(context.Background(), version, nil)
}

// DownloadAndInstallContext makes network preparation caller-cancellable and
// delegates only the final mutation to critical. The application uses that
// boundary to cancel a download during shutdown while still waiting for an
// executable, bundle, or package-manager transaction that has already begun.
func DownloadAndInstallContext(ctx context.Context, version string, critical func(func() error) error) error {
	if err := validateReleaseVersion(version); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if critical == nil {
		critical = func(action func() error) error { return action() }
	}
	return downloadAndInstall(ctx, version, critical)
}

func downloadAndInstall(ctx context.Context, version string, critical func(func() error) error) error {
	// macOS ships as an .app bundle: a directory tree, so swapping the single
	// executable inside it would leave the bundle's resources, Info.plist and
	// code signature describing the old version. It gets replaced whole.
	if runtime.GOOS == "darwin" {
		return installBundleUpdate(ctx, version, critical)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("automatic updates are not supported on %s; download %s %s from the release page, close the app, and replace the complete application bundle", runtime.GOOS, BinaryName, version)
	}
	if IsPackageManaged() {
		return installPackageUpdate(ctx, version, critical)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}
	if strings.HasPrefix(execPath, "/usr/") {
		return fmt.Errorf("system-wide installation detected; install %s %s through your package manager", BinaryName, version)
	}

	arch := runtime.GOARCH
	filename := fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, strings.TrimPrefix(version, "v"), runtime.GOOS, arch)
	archivePath, err := downloadVerifiedAssetContext(ctx, version, filename, BinaryName+"-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	stageDir, err := createUpdateStageDir(filepath.Dir(execPath), "."+BinaryName+"-update-*")
	if err != nil {
		return fmt.Errorf("cannot create update beside executable: %w", err)
	}
	defer os.RemoveAll(stageDir)
	stagedPath := filepath.Join(stageDir, filepath.Base(execPath))
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create staged executable: %w", err)
	}
	if err := staged.Chmod(0755); err != nil {
		_ = staged.Close()
		return fmt.Errorf("cannot mark staged executable as runnable: %w", err)
	}
	// The archive names the binary as it is installed: asmgr-desktop on Linux,
	// asmgr-desktop.exe on Windows. Looking for the bare name on Windows finds
	// nothing and fails the update.
	entryName := BinaryName
	if runtime.GOOS == "windows" {
		entryName += ".exe"
	}
	if err := extractExecutable(archivePath, entryName, staged); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("cannot close staged executable: %w", err)
	}

	// Windows links against runtime DLLs shipped beside the executable. A new
	// binary against stale libraries is the kind of breakage that only shows up
	// as a failure to start, so they are refreshed before the swap — while the
	// old executable is still in place and the update can still be abandoned.
	files := make([]stagedInstall, 0, 5)
	cleanupSidecars := func() {}
	if runtime.GOOS == "windows" {
		files, cleanupSidecars, err = stageSidecarDLLs(archivePath, filepath.Dir(execPath))
		if err != nil {
			return err
		}
		if err := validateRequiredSidecarDLLs(files, requiredWindowsRuntimeDLLs); err != nil {
			cleanupSidecars()
			return err
		}
	}
	defer cleanupSidecars()
	// The executable is deliberately last. If any library swap fails, rollback
	// restores every earlier DLL before the old executable is ever disturbed;
	// if the executable swap fails, the same transaction restores all DLLs too.
	files = append(files, stagedInstall{target: execPath, staged: stagedPath})
	return withInstallLock(func() error {
		// installTransaction performs its complete target/staging prevalidation
		// before the first rename. Keeping that check inside this lock closes the
		// gap between validation and the executable/DLL swap.
		return critical(func() error {
			return installTransaction(files)
		})
	})
}
