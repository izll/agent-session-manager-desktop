package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These run against a real tmux: the thing under test is what happens to a
// process tree when a pane is interrupted, which a shim cannot tell us.
func requireTmux(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell and tmux")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
}

// liveRunnerSession starts a real tmux session with one runner tab holding
// command, and returns the instance.
func liveRunnerSession(t *testing.T, command string) *Instance {
	t.Helper()
	requireTmux(t)
	restoreTmuxBinary(t)

	inst := &Instance{
		ID:     "asm_runner_" + strings.ReplaceAll(t.Name(), "/", "_"),
		Name:   "runner",
		Path:   t.TempDir(),
		Status: StatusRunning,
		Agent:  AgentTerminal,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Name: "run", Agent: AgentTerminal, RunCommand: command},
		},
	}
	name := inst.TmuxSessionName()
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name,
		"-x", "80", "-y", "24", "/bin/sh").Run(); err != nil {
		t.Skipf("could not start tmux: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })

	if err := exec.Command("tmux", "new-window", "-d", "-t", name+":1",
		"-n", "run", "/bin/sh").Run(); err != nil {
		t.Skipf("could not create the runner window: %v", err)
	}
	// Dead panes have to stay, or a stopped tab's window would vanish and
	// there would be nothing to restart. Set per window with -w: the
	// session-level option does not reach a window that already exists,
	// which is the same trap the app itself hit.
	_ = exec.Command("tmux", "set-option", "-t", name, "remain-on-exit", "on").Run()
	_ = exec.Command("tmux", "set-option", "-w", "-t", name+":1", "remain-on-exit", "on").Run()
	return inst
}

func waitUntil(t *testing.T, within time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// The case this function exists for: a command that ignores Ctrl+C must
// still be stopped, rather than left running behind a tab that says stopped.
//
// Measured, not assumed — a shell that traps INT and keeps going is exactly
// what a wrapper script around a server looks like from the outside.
func TestStopRunnerCommandTakesTheStubbornOne(t *testing.T) {
	inst := liveRunnerSession(t, "")
	defer func() { runnerInterruptGrace = 5 * time.Second }()
	runnerInterruptGrace = 1500 * time.Millisecond

	// Trap INT and keep running: Ctrl+C alone will not end this.
	script := filepath.Join(t.TempDir(), "stubborn.sh")
	body := "#!/bin/sh\ntrap '' INT\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	inst.FollowedWindows[0].RunCommand = "sh " + script

	if err := inst.TypeIntoFreshShell(1, "sh "+script, 5*time.Second); err != nil {
		t.Fatalf("could not start the command: %v", err)
	}
	if !waitUntil(t, 3*time.Second, func() bool { return inst.RunnerCommandRunning(1) }) {
		t.Fatal("the command never started")
	}

	gentle, err := inst.StopRunnerCommand(context.Background(), 1)
	if err != nil {
		t.Fatalf("StopRunnerCommand: %v", err)
	}
	if gentle {
		t.Error("a command that ignores Ctrl+C was reported as stopping on its own")
	}
	if inst.RunnerCommandRunning(1) {
		t.Error("the command is still running after being stopped")
	}
}

// A well-behaved command stops on the interrupt, and must not be reported as
// needing the hard stop — the difference is whether it got to shut down.
func TestStopRunnerCommandIsGentleWhenItCan(t *testing.T) {
	inst := liveRunnerSession(t, "sleep 300")

	if err := inst.TypeIntoFreshShell(1, "sleep 300", 5*time.Second); err != nil {
		t.Fatalf("could not start the command: %v", err)
	}
	if !waitUntil(t, 3*time.Second, func() bool { return inst.RunnerCommandRunning(1) }) {
		t.Fatal("the command never started")
	}

	gentle, err := inst.StopRunnerCommand(context.Background(), 1)
	if err != nil {
		t.Fatalf("StopRunnerCommand: %v", err)
	}
	if !gentle {
		t.Error("sleep stopped only for the hard stop; Ctrl+C should have been enough")
	}
	// Ctrl+C ends the command, not the shell: the pane stays, ready for the
	// next run, which is the whole point of reusing the tab.
	if !inst.paneRunning(1) {
		t.Error("the pane was taken although the command stopped on its own")
	}
	if inst.RunnerCommandRunning(1) {
		t.Error("the command is still running after Ctrl+C")
	}
}

// Stopping something that already finished is a no-op, not a failure.
func TestStopRunnerCommandAcceptsAFinishedCommand(t *testing.T) {
	inst := liveRunnerSession(t, "true")
	name := inst.TmuxSessionName()
	// Kill the pane so nothing is running in it.
	_ = exec.Command("tmux", "respawn-pane", "-k", "-t", name+":1", "--", "exit", "0").Run()
	if !waitUntil(t, 3*time.Second, func() bool { return !inst.paneRunning(1) }) {
		t.Skip("the pane did not exit; remain-on-exit may not be honoured here")
	}
	if inst.RunnerCommandRunning(1) {
		t.Fatal("a dead pane was reported as running a command")
	}

	gentle, err := inst.StopRunnerCommand(context.Background(), 1)
	if err != nil {
		t.Errorf("stopping a finished command failed: %v", err)
	}
	if !gentle {
		t.Error("a finished command should need no hard stop")
	}
}

// A tab with no command of its own is not a runner, and both controls must
// say so rather than acting on the pane.
func TestRunnerControlsRefuseANonRunnerTab(t *testing.T) {
	inst := &Instance{
		ID: "asm_plain", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 1, Name: "shell", Agent: AgentTerminal}},
	}
	if command, ok := inst.RunnerCommandFor(1); ok || command != "" {
		t.Errorf("RunnerCommandFor = %q, %v; want empty, false", command, ok)
	}
	if err := inst.StartRunnerCommand(context.Background(), 1); err == nil {
		t.Error("StartRunnerCommand accepted a tab with no command")
	}
	if _, err := inst.StopRunnerCommand(context.Background(), 1); err == nil {
		t.Error("StopRunnerCommand accepted a tab with no command")
	}
	// Whitespace is not a command either.
	inst.FollowedWindows[0].RunCommand = "   "
	if _, ok := inst.RunnerCommandFor(1); ok {
		t.Error("blank space was taken for a command")
	}
}
