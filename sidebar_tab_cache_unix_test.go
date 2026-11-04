//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// loggingTmuxServer is a server of this test's own that writes down what it is asked.
func loggingTmuxServer(t *testing.T) (run func(args ...string), calls func() []string) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	logFile := filepath.Join(dir, "log")
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> '" + logFile + "'\nexec '" + real + "' -S '" + filepath.Join(dir, "s") + "' -f /dev/null \"$@\"\n"
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
	return func(args ...string) {
			t.Helper()
			if output, err := exec.Command(shim, args...).CombinedOutput(); err != nil {
				t.Fatalf("tmux %v: %v: %s", args, err, output)
			}
		}, func() []string {
			data, _ := os.ReadFile(logFile)
			_ = os.Truncate(logFile, 0)
			return strings.Fields(string(data))
		}
}

func countCalls(calls []string, name string) int {
	count := 0
	for _, call := range calls {
		if call == name {
			count++
		}
	}
	return count
}

// On a real server: quiet tabs are not read, and what a tab prints gets it read.
func TestAQuietTabIsNotReadAndAPrintingOneIs(t *testing.T) {
	run, calls := loggingTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{
		ID: "quiet", Name: "quiet", Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentAider,
		FollowedWindows: []session.FollowedWindow{{ID: "t", Index: 1, Agent: session.AgentAider, Name: "tab"}},
	}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	name := inst.TmuxSessionName()
	run("new-session", "-d", "-s", name, "cat")
	run("new-window", "-d", "-t", name+":1", "cat")
	ctx := context.Background()

	// A reading counts once it began in a later second than the last output.
	time.Sleep(1100 * time.Millisecond)
	first := app.getSidebarUpdates(ctx)
	if first.Activities["quiet"] == "" {
		t.Fatalf("no reading: %+v", first.Activities)
	}
	calls()
	quiet := app.getSidebarUpdates(ctx)
	seen := calls()
	if got := countCalls(seen, "capture-pane"); got != 0 {
		t.Fatalf("read %d times with nothing printed: %v", got, seen)
	}
	if quiet.Activities["quiet"] != first.Activities["quiet"] {
		t.Fatalf("activity = %q, was %q", quiet.Activities["quiet"], first.Activities["quiet"])
	}

	time.Sleep(1100 * time.Millisecond)
	run("send-keys", "-t", name+":1", "hello", "Enter")
	time.Sleep(100 * time.Millisecond)
	calls()
	app.getSidebarUpdates(ctx)
	if got := countCalls(calls(), "capture-pane"); got == 0 {
		t.Fatal("a tab that printed was not read")
	}
}

// What a tab says changes on the sidebar, both ways, as it did before any of this.
func TestAStatusChangeStillArrives(t *testing.T) {
	run, calls := loggingTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "shows", Name: "shows", Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentAider}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	name := inst.TmuxSessionName()
	run("new-session", "-d", "-x", "100", "-y", "40", "-s", name, "cat")
	ctx := context.Background()
	time.Sleep(1100 * time.Millisecond)
	app.getSidebarUpdates(ctx)
	if got := app.getSidebarUpdates(ctx).Activities["shows"]; got != "idle" {
		t.Fatalf("activity = %q", got)
	}
	run("send-keys", "-t", name+":0", "do you want to proceed", "Enter")
	time.Sleep(200 * time.Millisecond)
	if got := app.getSidebarUpdates(ctx).Activities["shows"]; got != "waiting" {
		t.Fatalf("activity = %q after asking", got)
	}
	// Waiting stands from memory, with nothing printed since.
	time.Sleep(1100 * time.Millisecond)
	app.getSidebarUpdates(ctx)
	calls()
	if got := app.getSidebarUpdates(ctx).Activities["shows"]; got != "waiting" {
		t.Fatalf("activity = %q while still asking", got)
	}
	if got := countCalls(calls(), "capture-pane"); got != 0 {
		t.Fatalf("read %d times with nothing printed", got)
	}
	// Answered: the question scrolls out of what is looked at.
	for range 20 {
		run("send-keys", "-t", name+":0", "next", "Enter")
	}
	time.Sleep(200 * time.Millisecond)
	if got := app.getSidebarUpdates(ctx).Activities["shows"]; got != "idle" {
		t.Fatalf("activity = %q after answering", got)
	}
}

// A window that has gone is not answered for from memory.
func TestAClosedWindowIsNotAnsweredFromMemory(t *testing.T) {
	run, calls := loggingTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{
		ID: "closes", Name: "closes", Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentAider,
		FollowedWindows: []session.FollowedWindow{{ID: "t", Index: 1, Agent: session.AgentAider, Name: "tab"}},
	}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	name := inst.TmuxSessionName()
	run("new-session", "-d", "-s", name, "cat")
	run("new-window", "-d", "-t", name+":1", "cat")
	ctx := context.Background()
	time.Sleep(1100 * time.Millisecond)
	app.getSidebarUpdates(ctx)
	app.getSidebarUpdates(ctx)
	run("kill-window", "-t", name+":1")
	calls()
	app.getSidebarUpdates(ctx)
	seen := calls()
	if countCalls(seen, "capture-pane")+countCalls(seen, "has-session") == 0 {
		t.Fatalf("not looked at again: %v", seen)
	}
}

// However many sessions there are, a quiet sweep asks about the same few things.
func TestAQuietSweepDoesNotGrowWithTheSessions(t *testing.T) {
	run, calls := loggingTmuxServer(t)
	app := passwordTestApp(t)
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		inst := &session.Instance{
			ID: id, Name: id, Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentAider,
			FollowedWindows: []session.FollowedWindow{{ID: "t", Index: 1, Agent: session.AgentTerminal, Name: "tab"}},
		}
		if err := app.storage.AddInstance(inst); err != nil {
			t.Fatal(err)
		}
		run("new-session", "-d", "-s", inst.TmuxSessionName(), "cat")
		run("new-window", "-d", "-t", inst.TmuxSessionName()+":1", "cat")
	}
	ctx := context.Background()
	time.Sleep(1100 * time.Millisecond)
	// What is saved now and then, on its own schedule in the app.
	app.terminalDirSaves.interval = time.Hour
	app.runningSaves.interval = time.Hour
	app.getSidebarUpdates(ctx)
	calls()
	update := app.getSidebarUpdates(ctx)
	if len(update.Activities) != 6 {
		t.Fatalf("activities = %v", update.Activities)
	}
	seen := calls()
	if len(seen) > 4 {
		t.Fatalf("%d calls: %v", len(seen), seen)
	}
}

// Reading tabs that printed asks about each one's screen, not about the session again.
func TestReadingPrintingTabsAsksOnlyWhatItNeeds(t *testing.T) {
	run, calls := loggingTmuxServer(t)
	app := passwordTestApp(t)
	app.terminalDirSaves.interval = time.Hour
	app.runningSaves.interval = time.Hour
	var names []string
	for _, id := range []string{"a", "b", "c"} {
		inst := &session.Instance{ID: id, Name: id, Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentAider}
		if err := app.storage.AddInstance(inst); err != nil {
			t.Fatal(err)
		}
		run("new-session", "-d", "-s", inst.TmuxSessionName(), "cat")
		names = append(names, inst.TmuxSessionName())
	}
	ctx := context.Background()
	time.Sleep(1100 * time.Millisecond)
	app.getSidebarUpdates(ctx)
	time.Sleep(1100 * time.Millisecond)
	for _, name := range names {
		run("send-keys", "-t", name+":0", "x", "Enter")
	}
	time.Sleep(100 * time.Millisecond)
	calls()
	app.getSidebarUpdates(ctx)
	seen := calls()
	if got := countCalls(seen, "has-session"); got != 0 {
		t.Fatalf("has-session %d times: %v", got, seen)
	}
	if got := countCalls(seen, "list-sessions"); got > 2 {
		t.Fatalf("list-sessions %d times: %v", got, seen)
	}
	if got := countCalls(seen, "capture-pane"); got == 0 {
		t.Fatalf("not read: %v", seen)
	}
}
