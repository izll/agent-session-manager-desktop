//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A saved command marked to run in the background gets a terminal tab of its
// own. Against a real multiplexer of the test's own (isolatedTmux), since what
// matters is which window tmux makes current and whether a shell runs what is
// typed into it before it has finished starting.

func TestABackgroundTabLeavesTheCurrentWindowAlone(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := startedTerminalSession(t, "bg-current")
	before := paneFormat(t, tmux, inst.TmuxSessionName(), "#{window_index}")

	index, err := inst.NewBackgroundWindowOn("", "build", "")
	if err != nil {
		t.Fatalf("background tab: %v", err)
	}
	if got := paneFormat(t, tmux, inst.TmuxSessionName(), "#{window_index}"); got != before {
		t.Errorf("current window = %s, want it left at %s", got, before)
	}
	if got := paneFormat(t, tmux, inst.TmuxSessionName()+":"+itoa(index), "#{window_name}"); got != "build" {
		t.Errorf("new window is named %q, want %q", got, "build")
	}
	if tab := tabNamed(t, inst, "build"); tab.Index != index || tab.Agent != AgentTerminal {
		t.Errorf("recorded tab = %+v, want a terminal at %d", tab, index)
	}
}

func TestACommandTypedIntoAFreshBackgroundTabRuns(t *testing.T) {
	isolatedTmux(t)
	inst := startedTerminalSession(t, "bg-runs")
	marker := filepath.Join(t.TempDir(), "ran")

	index, err := inst.NewBackgroundWindowOn("", "build", "")
	if err != nil {
		t.Fatalf("background tab: %v", err)
	}
	// Straight after opening, as RunCommand does: the shell has not finished
	// its startup files yet, and what arrives before that can be flushed.
	// The test runs the user's own shell configuration, which is the point.
	if err := inst.TypeIntoFreshShell(index, "echo ok > '"+marker+"'", 10*time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForFile(t, marker)
}

// A tab on a server is typed into on the server. The session's own
// multiplexer has no window of that number, so the text used to go nowhere.
func TestTextForAServerTabIsTypedOnTheServer(t *testing.T) {
	isolatedTmux(t)
	server := fakeServerTmux(t)
	inst := startedTerminalSession(t, "bg-server")
	SetTabExecutor(inst.ID, "srv1", server)
	t.Cleanup(func() { ClearTabExecutor(inst.ID, "srv1") })
	marker := filepath.Join(t.TempDir(), "ran")

	index, err := inst.NewBackgroundWindowOn("srv1", "remote", "/")
	if err != nil {
		t.Fatalf("server tab: %v", err)
	}
	if err := inst.TypeIntoFreshShell(index, "echo ok > '"+marker+"'", 10*time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForFile(t, marker)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the command never ran: %s was not created", path)
}
