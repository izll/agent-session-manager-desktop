package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ShouldCheckForUpdate reads the timestamp from the real config dir, so point
// HOME at a temp dir to keep the test self-contained.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
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
