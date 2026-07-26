package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	RepoOwner     = "izll"
	RepoName      = "agent-session-manager-desktop"
	BinaryName    = "asmgr-desktop"
	CheckTimeout  = 5 * time.Second
	DownloadLimit = 512 << 20 // 512 MiB, including compressed release assets.
	BinaryLimit   = 256 << 20 // 256 MiB uncompressed executable limit.

	// Automatic checks are throttled to once a day, matching the TUI version.
	CheckInterval = 24 * time.Hour
	LastCheckFile = "last_update_check"
	// AvailableUpdateFile caches the last "there is a newer version" answer.
	// Without it a launch that falls inside the daily throttle shows nothing,
	// even though an update is still waiting.
	AvailableUpdateFile = "available_update"
)

var (
	apiBaseURL      = "https://api.github.com"
	downloadBaseURL = "https://github.com"
	checkClient     = &http.Client{Timeout: CheckTimeout}
	downloadClient  = &http.Client{Timeout: 5 * time.Minute}
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
	tarReader := tar.NewReader(gz)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("failed to read archive: %w", nextErr)
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

func replaceExecutable(execPath, stagedPath string) error {
	oldPath := execPath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(execPath, oldPath); err != nil {
		return fmt.Errorf("failed to back up old executable: %w", err)
	}
	if err := os.Rename(stagedPath, execPath); err != nil {
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("failed to install new executable: %w", err)
	}
	_ = os.Remove(oldPath)
	return nil
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
	if runtime.GOOS != "linux" {
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
	if err := extractExecutable(archivePath, BinaryName, staged); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("cannot close staged executable: %w", err)
	}
	return replaceExecutable(execPath, stagedPath)
}
