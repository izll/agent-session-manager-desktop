package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// resetStartSizeForTest clears the remembered size for one test and restores
// the state afterwards, so no test leaks a size into session creation
// elsewhere.
func resetStartSizeForTest(t *testing.T) {
	t.Helper()
	startSize.mu.Lock()
	previousCols, previousRows, previousPath := startSize.cols, startSize.rows, startSize.path
	startSize.cols, startSize.rows, startSize.path = 0, 0, ""
	startSize.mu.Unlock()
	previousDelay := startSizeSaveDelay
	t.Cleanup(func() {
		FlushStartSize()
		startSize.mu.Lock()
		startSize.cols, startSize.rows, startSize.path = previousCols, previousRows, previousPath
		startSize.mu.Unlock()
		startSizeSaveDelay = previousDelay
	})
}

func TestANewSessionWithNoKnownSizeIsCreatedAsBefore(t *testing.T) {
	resetStartSizeForTest(t)
	got := strings.Join(newSessionArgs("s", "/work", false), " ")
	if got != "new-session -d -s s -c /work" {
		t.Fatalf("args = %q", got)
	}
}

func TestANewSessionStartsAtTheViewersSize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a local psmux session is created without a size")
	}
	resetStartSizeForTest(t)
	SetStartSize(222, 60)
	got := strings.Join(newSessionArgs("s", "/work", false), " ")
	if got != "new-session -d -s s -x 222 -y 60 -c /work" {
		t.Fatalf("args = %q", got)
	}
}

// A server runs real tmux whatever the app runs on.
func TestASessionOnAServerStartsAtTheViewersSize(t *testing.T) {
	resetStartSizeForTest(t)
	SetStartSize(180, 50)
	got := strings.Join(newSessionArgs("s", "/work", true), " ")
	if got != "new-session -d -s s -x 180 -y 50 -c /work" {
		t.Fatalf("args = %q", got)
	}
}

// A hidden or half laid-out pane measures small. Taken for the viewer's size,
// it would start sessions narrower than the default it replaces.
func TestAnImplausibleSizeIsIgnored(t *testing.T) {
	resetStartSizeForTest(t)
	SetStartSize(222, 60)
	for _, size := range [][2]int{{5, 60}, {39, 60}, {222, 2}, {0, 0}, {5000, 60}} {
		SetStartSize(size[0], size[1])
	}
	if cols, rows := StartSize(); cols != 222 || rows != 60 {
		t.Fatalf("StartSize = %dx%d, want 222x60", cols, rows)
	}
}

// Reopening interrupted sessions runs as the app starts, before a terminal has
// given a size, so the last one has to survive a restart.
func TestTheSizeIsRememberedAcrossARestart(t *testing.T) {
	resetStartSizeForTest(t)
	startSizeSaveDelay = 10 * time.Millisecond
	path := filepath.Join(t.TempDir(), "terminal-size")
	ConfigureStartSize(path)
	SetStartSize(200, 55)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == "200x55" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the size was never written")
		}
		time.Sleep(10 * time.Millisecond)
	}

	startSize.mu.Lock()
	startSize.cols, startSize.rows, startSize.path = 0, 0, ""
	startSize.mu.Unlock()
	ConfigureStartSize(path)
	if cols, rows := StartSize(); cols != 200 || rows != 55 {
		t.Fatalf("after a restart StartSize = %dx%d, want 200x55", cols, rows)
	}
}

// Quitting right after a resize must not lose it to the save delay.
func TestFlushWritesAPendingSize(t *testing.T) {
	resetStartSizeForTest(t)
	startSizeSaveDelay = time.Hour
	path := filepath.Join(t.TempDir(), "terminal-size")
	ConfigureStartSize(path)
	SetStartSize(150, 40)
	FlushStartSize()
	if data, err := os.ReadFile(path); err != nil || strings.TrimSpace(string(data)) != "150x40" {
		t.Fatalf("after flush the file holds %q, %v", data, err)
	}
}

func TestAnUnreadableRememberedSizeIsIgnored(t *testing.T) {
	resetStartSizeForTest(t)
	path := filepath.Join(t.TempDir(), "terminal-size")
	for _, content := range []string{"", "wide", "222x", "10x5", "222x60x1"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		startSize.mu.Lock()
		startSize.cols, startSize.rows = 0, 0
		startSize.mu.Unlock()
		ConfigureStartSize(path)
		if cols, _ := StartSize(); cols != 0 {
			t.Errorf("%q gave a start size of %d columns", content, cols)
		}
	}
}

// Against tmux itself: a session nobody is attached to keeps the size it was
// created at, and a tab opened in it later, out of view, gets the same — not
// tmux's 80x24, which is what an agent reopened at start used to run at.
func TestASessionOutOfViewRunsAtTheViewersWidth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("psmux sessions are created without a size")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	resetStartSizeForTest(t)
	SetStartSize(222, 60)

	socket := fmt.Sprintf("asmgr-start-size-%d", os.Getpid())
	tm := func(args ...string) (string, error) {
		out, err := exec.Command("tmux", append([]string{"-L", socket, "-f", os.DevNull}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	defer tm("kill-server")

	if out, err := tm(append(newSessionArgs("s", t.TempDir(), false), "sleep 60")...); err != nil {
		t.Fatalf("new-session: %v: %s", err, out)
	}
	tm("set-option", "-t", "s", "window-size", "latest")
	if out, err := tm("new-window", "-d", "-t", "s:", "sleep 60"); err != nil {
		t.Fatalf("new-window: %v: %s", err, out)
	}
	for _, window := range []string{"s:0", "s:1"} {
		if got, _ := tm("display", "-p", "-t", window, "#{window_width}x#{window_height}"); got != "222x60" {
			t.Errorf("%s is %s, want 222x60", window, got)
		}
	}
}
