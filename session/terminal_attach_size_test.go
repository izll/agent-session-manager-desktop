//go:build !windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Attached without a size, the client's PTY was 0x0, which tmux takes for
// 80x24; with window-size "latest" the window shrank to that until the real
// size arrived, and an agent redrew its output at 80 columns in between —
// the scrollback kept that narrow copy. Started at the viewer's size, the
// window keeps its width.
func TestAttachAtTheViewersSizeKeepsTheWindowWide(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("asmgr-attach-size-%d", os.Getpid())
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", socket, "-f", os.DevNull}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	defer tm("kill-server")
	tm("new-session", "-d", "-s", "s", "-x", "222", "-y", "60", "sleep 60")
	tm("set", "-g", "window-size", "latest")
	width := func() string { return tm("display", "-p", "-t", "s", "#{window_width}") }

	attach := func(cols, rows int) func() {
		cmd := exec.Command("tmux", "-L", socket, "-f", os.DevNull, "attach", "-t", "s")
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		stream, err := StartTerminalWithSize(cmd, cols, rows)
		if err != nil {
			t.Fatal(err)
		}
		return func() { _ = stream.Close(); _ = cmd.Wait() }
	}
	settled := func(want string) string {
		deadline := time.Now().Add(2 * time.Second)
		got := width()
		for got != want && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			got = width()
		}
		return got
	}

	done := attach(222, 60)
	time.Sleep(300 * time.Millisecond)
	if got := width(); got != "222" {
		t.Errorf("attached at the viewer's size, the window became %s columns", got)
	}
	done()

	// The old way, for comparison: no size.
	done = attach(0, 0)
	if got := settled("80"); got != "80" {
		t.Skipf("this tmux does not shrink to a sizeless client (%s); nothing to compare", got)
	}
	done()
}

func TestStartTerminalWithSizeWithoutASizeStartsAsBefore(t *testing.T) {
	cmd := exec.Command("true")
	stream, err := StartTerminalWithSize(cmd, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	_ = cmd.Wait()
}
