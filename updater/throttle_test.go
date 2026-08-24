package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ShouldCheckForUpdate reads the timestamp from the real config dir, so point
// HOME at a temp dir to keep the test self-contained.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	return filepath.Join(home, ".config", "agent-session-manager-desktop")
}

func writeStamp(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LastCheckFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShouldCheckForUpdateThrottles(t *testing.T) {
	dir := withTempHome(t)

	// Never checked before.
	if !ShouldCheckForUpdate() {
		t.Error("first run should check")
	}

	// Checked just now → wait.
	writeStamp(t, dir, time.Now().Format(time.RFC3339))
	if ShouldCheckForUpdate() {
		t.Error("a check minutes ago should not trigger another")
	}

	// Checked over a day ago → check again.
	writeStamp(t, dir, time.Now().Add(-25*time.Hour).Format(time.RFC3339))
	if !ShouldCheckForUpdate() {
		t.Error("a check 25 hours ago should trigger a new one")
	}

	// Just under the interval → still wait.
	writeStamp(t, dir, time.Now().Add(-23*time.Hour).Format(time.RFC3339))
	if ShouldCheckForUpdate() {
		t.Error("23 hours is inside the 24h interval")
	}
}

// Anything unreadable must fall back to checking: missing an update is worse
// than one extra request.
func TestShouldCheckForUpdateOnBadTimestamp(t *testing.T) {
	dir := withTempHome(t)
	for _, bad := range []string{"", "not-a-date", "0", "   "} {
		writeStamp(t, dir, bad)
		if !ShouldCheckForUpdate() {
			t.Errorf("an unparseable timestamp (%q) should still check", bad)
		}
	}
}

// A clock change could otherwise park the timestamp in the future and
// suppress checks forever.
func TestShouldCheckForUpdateOnFutureTimestamp(t *testing.T) {
	dir := withTempHome(t)
	writeStamp(t, dir, time.Now().Add(48*time.Hour).Format(time.RFC3339))
	if !ShouldCheckForUpdate() {
		t.Error("a future timestamp must not disable update checks")
	}
}

func TestSaveLastCheckTimeRoundTrips(t *testing.T) {
	withTempHome(t)
	if !ShouldCheckForUpdate() {
		t.Fatal("precondition: should check on a clean home")
	}
	SaveLastCheckTime()
	if ShouldCheckForUpdate() {
		t.Error("after saving, the next check should be throttled")
	}
}

// A package-managed install must not fall through to the user-local path,
// which would try to overwrite a root-owned binary.
func TestManualInstallHintNamesARealCommand(t *testing.T) {
	hint := manualInstallHint("v1.2.3")
	if !strings.Contains(hint, "1.2.3") {
		t.Errorf("hint %q does not mention the version", hint)
	}
	if !strings.Contains(hint, "dpkg -i") && !strings.Contains(hint, "rpm -U") {
		t.Errorf("hint %q names neither dpkg nor rpm", hint)
	}
	// The version must appear without the leading "v" — that's how the
	// release assets are named.
	if strings.Contains(hint, "v1.2.3") {
		t.Errorf("hint %q kept the v prefix; asset names don't have it", hint)
	}
}

// DownloadAndInstall validates the version before touching the network, so a
// malformed tag can't reach the download or the installer.
func TestDownloadAndInstallRejectsBadVersions(t *testing.T) {
	for _, bad := range []string{"", "latest", "../etc", "v1.2.3; rm -rf /", "1.2.3-rc1'"} {
		if err := DownloadAndInstall(bad); err == nil {
			t.Errorf("DownloadAndInstall(%q) was accepted; it must be rejected", bad)
		}
	}
}

// The user-local (tar.gz) update path is the one people who didn't install a
// distro package rely on. It downloads, verifies, extracts and swaps the
// binary, so exercise all four against a real release rather than trusting it.
func TestUserLocalUpdatePathWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads a release asset")
	}
	dir := t.TempDir()
	installed := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(installed, []byte("old binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive, err := downloadVerifiedAsset(
		"v0.7.9", BinaryName+"_0.7.9_linux_amd64.tar.gz", BinaryName+"-*.tar.gz")
	if err != nil {
		t.Skipf("release asset unavailable: %v", err)
	}
	defer os.Remove(archive)

	staged, err := os.CreateTemp(dir, "."+BinaryName+"-update-*")
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if err := staged.Chmod(0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractExecutable(archive, BinaryName, staged); err != nil {
		t.Fatalf("extract: %v", err)
	}
	staged.Close()

	if err := replaceExecutable(installed, stagedPath); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "old binary\n" {
		t.Fatal("the binary was not replaced")
	}
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no executable bit — what a file can do there is decided by its
	// extension and the ACL, and Go reports 0666 regardless.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Error("the replacement is not executable")
	}
}

// The cached "an update is waiting" answer is what the UI shows at startup, so
// it has to survive a restart, disappear once installed, and never outlive its
// usefulness.
func TestCachedAvailableUpdateLifecycle(t *testing.T) {
	withTempHome(t)

	if got := CachedAvailableUpdate("0.7.7"); got != "" {
		t.Errorf("clean install returned %q, want empty", got)
	}

	SaveAvailableUpdate("v0.7.9")
	if got := CachedAvailableUpdate("0.7.7"); got != "v0.7.9" {
		t.Errorf("cached update = %q, want v0.7.9 (it must survive a restart)", got)
	}

	// After updating, the same cache entry must not keep advertising a version
	// we are already running — or an older one.
	if got := CachedAvailableUpdate("0.7.9"); got != "" {
		t.Errorf("after updating to 0.7.9 the cache still offered %q", got)
	}
	if got := CachedAvailableUpdate("0.8.0"); got != "" {
		t.Errorf("a newer running version still saw %q offered", got)
	}

	SaveAvailableUpdate("v0.8.1")
	ClearAvailableUpdate()
	if got := CachedAvailableUpdate("0.7.7"); got != "" {
		t.Errorf("after clearing, the cache still returned %q", got)
	}

	// An empty save is the "nothing pending" signal and must clear too.
	SaveAvailableUpdate("v0.9.0")
	SaveAvailableUpdate("")
	if got := CachedAvailableUpdate("0.7.7"); got != "" {
		t.Errorf("saving an empty version left %q behind", got)
	}
}

// A corrupt or nonsensical cache file must not be shown as an update.
func TestCachedAvailableUpdateIgnoresGarbage(t *testing.T) {
	dir := withTempHome(t)
	for _, bad := range []string{"", "   ", "not-a-version", "v", "0.7.7-rc1"} {
		writeCache(t, dir, bad)
		if got := CachedAvailableUpdate("0.7.7"); got != "" {
			t.Errorf("cache %q was offered as %q", bad, got)
		}
	}
}

func writeCache(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, AvailableUpdateFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
