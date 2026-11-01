//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// isolatedTmuxServer points the app at a tmux server of this test's own.
func isolatedTmuxServer(t *testing.T) func(args ...string) ([]byte, error) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nexec '" + real + "' -S '" + filepath.Join(dir, "s") + "' -f /dev/null \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := session.TmuxBinary()
	t.Setenv("TMUX", "")
	session.SetTmuxBinary(shim)
	t.Cleanup(func() {
		_ = exec.Command(shim, "kill-server").Run()
		session.SetTmuxBinary(previous)
	})
	return func(args ...string) ([]byte, error) {
		return exec.Command(shim, args...).Output()
	}
}

// End to end on a real server: only the marked tab whose process ended goes.
func TestAnEndedTabGoesAndTheOthersStay(t *testing.T) {
	tmux := isolatedTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{
		ID: "closing", Name: "closing", Path: t.TempDir(),
		FollowedWindows: []session.FollowedWindow{
			{ID: "ended", Index: 1, Agent: session.AgentTerminal, CloseOnExit: session.TabCloseCommand},
			{ID: "running", Index: 2, Agent: session.AgentTerminal, CloseOnExit: session.TabCloseCommand},
			{ID: "unmarked", Index: 3, Agent: session.AgentTerminal},
		},
	}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "sleep 60"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	for index, command := range map[string]string{"1": "true", "2": "sleep 60", "3": "true"} {
		if _, err := tmux("new-window", "-d", "-t", name+":"+index, command); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for !inst.TabProcessExited(1) || !inst.TabProcessExited(3) {
		if time.Now().After(deadline) {
			t.Fatal("the commands did not end")
		}
		time.Sleep(20 * time.Millisecond)
	}

	app.closeExitedTabs()

	saved, err := app.storage.GetInstance("closing")
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, window := range saved.FollowedWindows {
		left = append(left, window.ID)
	}
	if strings.Join(left, ",") != "running,unmarked" {
		t.Fatalf("left = %v", left)
	}
	windows, err := tmux("list-windows", "-t", name, "-F", "#{window_index}")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(string(windows)), ","); got != "0,2,3" {
		t.Fatalf("windows = %s", got)
	}
}

// Another project is open: its tabs are not reachable, and wait until it is again.
func TestATabIsNotClosedFromAnotherProject(t *testing.T) {
	app := passwordTestApp(t)
	app.closeExitedTab("closing", session.TabToClose{ID: "x", Index: 1}, "another-project")
}

// The index is reused after the user closes a tab: the ID decides.
func TestATabReusingAnIndexIsLeftAlone(t *testing.T) {
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "reuse", Name: "reuse", Path: t.TempDir(), FollowedWindows: []session.FollowedWindow{{ID: "new", Index: 1, Agent: session.AgentTerminal}}}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app.closeExitedTab("reuse", session.TabToClose{ID: "old", Index: 1}, app.storage.GetActiveProjectID())
	// The same tab, moved to another index since it was looked at.
	app.closeExitedTab("reuse", session.TabToClose{ID: "new", Index: 9}, app.storage.GetActiveProjectID())
	saved, err := app.storage.GetInstance("reuse")
	if err != nil || len(saved.FollowedWindows) != 1 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}

// From the command library to a closed tab, on a real shell in a real tab. A command
// that opens a tab to be looked at keeps it.
func TestABackgroundCommandsTabClosesAndANewTabsDoesNot(t *testing.T) {
	tmux := isolatedTmuxServer(t)
	t.Setenv("SHELL", "/bin/sh")
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "cmds", Name: "cmds", Path: t.TempDir(), Status: session.StatusRunning}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	project := app.storage.GetActiveProjectID()
	run := func(name, runIn string) int {
		id, err := app.SaveCommand("", name, "true", "", "", true, runIn)
		if err != nil {
			t.Fatal(err)
		}
		index, err := app.RunCommand(id, "cmds", 0, nil, project)
		if err != nil {
			t.Fatal(err)
		}
		return index
	}
	foreground := run("stays", session.RunInNewTab)
	background := run("goes", session.RunInBackground)

	deadline := time.Now().Add(5 * time.Second)
	for {
		app.closeExitedTabs()
		saved, err := app.storage.GetInstance("cmds")
		if err != nil {
			t.Fatal(err)
		}
		if saved.GetFollowedWindowRecord(background) == nil {
			if saved.GetFollowedWindowRecord(foreground) == nil {
				t.Fatal("the new tab went too")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background tab is still there: %+v", saved.FollowedWindows)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
