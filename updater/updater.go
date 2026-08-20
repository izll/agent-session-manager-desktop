package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
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
)

var windowsRuntimeDLLs = []string{
	"libportaudio.dll",
	"libgcc_s_seh-1.dll",
	"libstdc++-6.dll",
	"libwinpthread-1.dll",
}

var (
	apiBaseURL      = "https://api.github.com"
	downloadBaseURL = "https://github.com"
	checkClient     = &http.Client{Timeout: CheckTimeout}
	downloadClient  = &http.Client{Timeout: 5 * time.Minute}
	installMu       sync.Mutex
)

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
func CheckForUpdate(currentVersion string) string {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBaseURL, RepoOwner, RepoName)
	resp, err := checkClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release GitHubRelease
	limited := io.LimitReader(resp.Body, 1<<20)
	if err := json.NewDecoder(limited).Decode(&release); err != nil {
		return ""
	}
	current, ok := parseSemver(currentVersion)
	if !ok {
		return ""
	}
	latest, ok := parseSemver(release.TagName)
	if !ok || latest.prerelease != "" {
		return ""
	}
	if compareSemver(latest, current) > 0 {
		return release.TagName
	}
	return ""
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
	resp, err := downloadClient.Get(url + ".sha256")
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
	if err := validateReleaseVersion(version); err != nil {
		return "", err
	}
	url := releaseURL(version, filename)
	expected, err := readChecksum(url, filename)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > DownloadLimit {
		return "", fmt.Errorf("download is too large: %d bytes", resp.ContentLength)
	}

	out, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return "", fmt.Errorf("failed to create secure temporary file: %w", err)
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
		return "", fmt.Errorf("failed to save download: %w", err)
	}
	if n > DownloadLimit {
		return "", fmt.Errorf("download exceeds %d byte limit", DownloadLimit)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return "", fmt.Errorf("checksum mismatch: got %s, expected %s", actual, expected)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync download: %w", err)
	}
	return path, nil
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
		output, queryErr := exec.Command("dpkg-query", "--search", execPath).Output()
		if queryErr == nil && strings.HasPrefix(strings.TrimSpace(string(output)), BinaryName+":") {
			return true
		}
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		return exec.Command("rpm", "-qf", execPath).Run() == nil
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
func installBundleUpdate(version string) error {
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
	archivePath, err := downloadVerifiedAsset(version, filename, BinaryName+"-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	// Unpack beside the installed bundle: same filesystem, so the swap below is
	// a rename rather than a copy, and /Applications stays untouched until the
	// new bundle is complete on disk.
	parent := filepath.Dir(bundle)
	stageDir, err := os.MkdirTemp(parent, "."+BinaryName+"-update-*")
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
	if _, err := os.Stat(staged); err != nil {
		found, findErr := findBundleIn(stageDir)
		if findErr != nil {
			return fmt.Errorf("the archive did not contain an .app bundle: %w", findErr)
		}
		staged = found
	}

	// Install under the archive's own name, so an update also carries the
	// rename across: macOS shows the directory name in Finder and Launchpad, so
	// keeping the old path would leave the app displayed as "asmgr-desktop"
	// forever. Same directory, so this stays a rename within one filesystem.
	target := filepath.Join(filepath.Dir(bundle), filepath.Base(staged))

	return withInstallLock(func() error {
		// Revalidate under the install lock. Downloading and extraction are safe to
		// overlap across processes; only this filesystem mutation must serialize.
		if info, err := os.Stat(bundle); err != nil || !info.IsDir() {
			return fmt.Errorf("installed application bundle is no longer available")
		}
		if info, err := os.Stat(staged); err != nil || !info.IsDir() {
			return fmt.Errorf("staged application bundle is no longer available")
		}
		return swapBundle(bundle, staged, target)
	})
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
		execPath, err := os.Executable()
		if err != nil {
			return nil
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return nil
		}
		cleanStaleUpdateFilesIn(filepath.Dir(execPath))
		_ = os.Remove(execPath + ".old")

		bundle := bundleRootFor(execPath)
		if bundle != "" {
			_ = os.RemoveAll(bundle + ".old")
		}
		cleanRecordedUpdateFiles(execPath, bundle)
		return nil
	})
}

// cleanStaleUpdateFilesIn removes the exact legacy names produced by older
// updater versions. It deliberately does not enumerate the directory by suffix.
func cleanStaleUpdateFilesIn(dir string) {
	names := []string{BinaryName + ".old", BinaryName + ".exe.old", BinaryName + ".app.old"}
	for _, dll := range windowsRuntimeDLLs {
		names = append(names, dll+".old")
	}
	for _, name := range names {
		_ = os.RemoveAll(filepath.Join(dir, name))
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
		_ = os.Rename(tmpPath, manifest)
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
	stageDir, err := os.MkdirTemp(destDir, "."+BinaryName+"-dll-stage-*")
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
func installPackageUpdate(version string) error {
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return fmt.Errorf("this installation is managed by the system package manager, and pkexec is not available to ask for permission; install %s %s with: sudo %s",
			BinaryName, version, manualInstallHint(version))
	}

	pkgPath, install, err := downloadPackageFor(version)
	if err != nil {
		return err
	}
	defer os.Remove(pkgPath)

	args := append(install, pkgPath)
	return withInstallLock(func() error {
		// The package type was selected before the download. Confirm under the
		// mutation lock that this executable is still package-owned before asking
		// for elevation and changing system files.
		if !IsPackageManaged() {
			return fmt.Errorf("installation type changed while the update was downloading; retry the update")
		}
		cmd := exec.Command(pkexec, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			// 126/127 are pkexec's own codes for "dismissed" and "not authorised".
			if code := cmd.ProcessState.ExitCode(); code == 126 || code == 127 {
				return fmt.Errorf("the update was not authorised")
			}
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("package installation failed: %s", msg)
		}
		return nil
	})
}

// downloadPackageFor fetches the package matching this installation and
// returns it with the command needed to install it.
func downloadPackageFor(version string) (path string, installCmd []string, err error) {
	if isDpkgInstall() {
		p, derr := DownloadDeb(version)
		if derr != nil {
			return "", nil, derr
		}
		// --force-confold keeps any config the user edited.
		return p, []string{"dpkg", "-i", "--force-confold"}, nil
	}
	p, rerr := DownloadRpm(version)
	if rerr != nil {
		return "", nil, rerr
	}
	return p, []string{"rpm", "-U", "--replacepkgs"}, nil
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
	out, qerr := exec.Command("dpkg-query", "--search", execPath).Output()
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
	if err := validateReleaseVersion(version); err != nil {
		return err
	}
	return downloadAndInstall(version)
}

func downloadAndInstall(version string) error {
	// macOS ships as an .app bundle: a directory tree, so swapping the single
	// executable inside it would leave the bundle's resources, Info.plist and
	// code signature describing the old version. It gets replaced whole.
	if runtime.GOOS == "darwin" {
		return installBundleUpdate(version)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("automatic updates are not supported on %s; download %s %s from the release page, close the app, and replace the complete application bundle", runtime.GOOS, BinaryName, version)
	}
	if IsPackageManaged() {
		return installPackageUpdate(version)
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
	archivePath, err := downloadVerifiedAsset(version, filename, BinaryName+"-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	staged, err := os.CreateTemp(filepath.Dir(execPath), "."+BinaryName+"-update-*")
	if err != nil {
		return fmt.Errorf("cannot create update beside executable: %w", err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
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
		return installTransaction(files)
	})
}
