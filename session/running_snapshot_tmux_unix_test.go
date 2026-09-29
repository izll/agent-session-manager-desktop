//go:build !windows

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// isolatedTmux points the package at a tmux server of the test's own, on a
// socket in a temporary directory: the real one the user works in is never
// asked anything. The server is killed when the test ends.
func isolatedTmux(t *testing.T) (tmux func(args ...string) ([]byte, error)) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	restoreTmuxBinary(t)
	dir := t.TempDir()
	socket := filepath.Join(dir, "s")
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nexec '" + real + "' -S '" + socket + "' -f /dev/null \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "")
	SetTmuxBinary(shim)
	resetMultiplexerLookup()
	t.Cleanup(func() {
		_ = exec.Command(shim, "kill-server").Run()
		resetMultiplexerLookup()
	})
	return func(args ...string) ([]byte, error) {
		return exec.Command(shim, args...).Output()
	}
}

func resetMultiplexerLookup() {
	multiplexerLookup.Lock()
	multiplexerLookup.binary = ""
	multiplexerLookup.Unlock()
}

// The whole point, end to end on a real multiplexer: a session with five tabs,
// three of them running when the machine went down, comes back with those
// three running and the other two parked — not all five, and not none.
func TestStartWithRunningTabsBringsBackOnlyWhatWasRunning(t *testing.T) {
	isolatedTmux(t)
	ctx := context.Background()

	inst := &Instance{
		ID:     "reopen-test",
		Name:   "reopen-test",
		Path:   t.TempDir(),
		Status: StatusStopped,
		Agent:  AgentTerminal,
		FollowedWindows: []FollowedWindow{
			{ID: "a", Name: "a", Agent: AgentTerminal, Index: 1},
			{ID: "b", Name: "b", Agent: AgentTerminal, Index: 2},
			{ID: "c", Name: "c", Agent: AgentTerminal, Index: 3},
			{ID: "d", Name: "d", Agent: AgentTerminal, Index: 4},
		},
	}
	record := RunningRecord{Tabs: []string{MainTabID, "a", "c"}}

	if err := inst.StartWithRunningTabs("", record); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer inst.Stop()

	live, ok := inst.LiveTabsContext(ctx)
	if !ok {
		t.Fatal("the reopened session cannot be listed")
	}
	sort.Strings(live)
	if want := []string{"a", "c", MainTabID}; !reflect.DeepEqual(live, want) {
		t.Errorf("running after the reopen = %v, want %v", live, want)
	}
	stopped := map[string]bool{}
	for _, window := range inst.FollowedWindows {
		stopped[window.ID] = window.Stopped
	}
	if want := map[string]bool{"a": false, "b": true, "c": false, "d": true}; !reflect.DeepEqual(stopped, want) {
		t.Errorf("stored stopped marks = %v, want %v", stopped, want)
	}
	if inst.MainWindowStopped {
		t.Error("the main window was running and came back parked")
	}
}

// The main window can be the one that was not running: it has to exist — the
// session is built around it — but comes back parked.
func TestStartWithRunningTabsParksAMainWindowThatWasStopped(t *testing.T) {
	isolatedTmux(t)

	inst := &Instance{
		ID: "reopen-main", Name: "reopen-main", Path: t.TempDir(),
		Status: StatusStopped, Agent: AgentTerminal,
		FollowedWindows: []FollowedWindow{{ID: "a", Name: "a", Agent: AgentTerminal, Index: 1}},
	}
	if err := inst.StartWithRunningTabs("", RunningRecord{Tabs: []string{"a"}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer inst.Stop()

	live, _ := inst.LiveTabsContext(context.Background())
	if !reflect.DeepEqual(live, []string{"a"}) {
		t.Errorf("running = %v, want only the tab", live)
	}
	if !inst.MainWindowStopped {
		t.Error("the main window is not marked parked")
	}
}

// A relaunch finds the same server; a reboot (here: the server killed) does
// not. That difference is what keeps the prompt away from an ordinary restart.
func TestMultiplexerIdentityTellsARestartedServerApart(t *testing.T) {
	tmux := isolatedTmux(t)
	ctx := context.Background()
	inst := &Instance{ID: "identity-test", Name: "identity-test", Path: t.TempDir(), Status: StatusStopped, Agent: AgentTerminal}

	if got := inst.MultiplexerIdentityContext(ctx); got != "" {
		t.Fatalf("no server running, yet an identity %q", got)
	}
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	before := inst.MultiplexerIdentityContext(ctx)
	if before == "" {
		t.Fatal("a running server gave no identity")
	}
	if again := inst.MultiplexerIdentityContext(ctx); again != before {
		t.Errorf("the same server answered %q then %q", before, again)
	}

	// The machine goes down: the server and everything in it.
	_, _ = tmux("kill-server")
	if JudgeInterruption(inst.IsAlive(), true, before, inst.MultiplexerIdentityContext(ctx)) != Interrupted {
		t.Error("a lost server did not read as an interruption")
	}
	// A new server comes up (the user's terminal, say) before the app does.
	if _, err := tmux("new-session", "-d", "-s", "other"); err != nil {
		t.Fatal(err)
	}
	after := inst.MultiplexerIdentityContext(ctx)
	if after == "" || after == before {
		t.Fatalf("a new server gave identity %q (was %q)", after, before)
	}
	if JudgeInterruption(inst.IsAlive(), true, before, after) != Interrupted {
		t.Error("a replaced server did not read as an interruption")
	}
}
