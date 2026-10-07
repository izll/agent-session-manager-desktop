package session

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Starting and stopping the one command a runner tab is the home of.
//
// Why stopping is not just killing the pane: a pane holds a shell, the shell
// holds the command, and the command holds whatever it started — a Gradle
// wrapper holds a JVM. Killing the pane takes the shell, and anything that
// did not notice keeps running with the port still bound, which shows up
// later as a build that cannot start for no visible reason. So Ctrl+C first,
// which is what a person would press, and the hard stop only for what is
// still there afterwards.

// How long a command is given to stop on its own after Ctrl+C.
//
// Variables so a test can shorten them rather than wait.
var (
	runnerInterruptGrace = 5 * time.Second
	runnerPollInterval   = 200 * time.Millisecond
)

// RunnerCommandFor returns the command a tab is the home of, and whether it
// has one at all.
func (i *Instance) RunnerCommandFor(windowIdx int) (string, bool) {
	for at := range i.FollowedWindows {
		if i.FollowedWindows[at].Index == windowIdx {
			command := strings.TrimSpace(i.FollowedWindows[at].RunCommand)
			return command, command != ""
		}
	}
	return "", false
}

// RunnerCommandRunning reports whether a runner tab's command is still
// going, as opposed to its pane merely being alive.
//
// The distinction is the whole difficulty. A runner tab holds a shell, and
// the command runs inside it: when the command ends — on its own or on
// Ctrl+C — the shell is still there, so the pane never dies and pane_dead
// stays 0. What changes is which program the pane is running, so that is
// what gets asked.
//
// A dead pane counts as not running, which is where a tab stopped by ■ ends
// up.
func (i *Instance) RunnerCommandRunning(windowIdx int) bool {
	if !i.paneRunning(windowIdx) {
		return false
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	server := i.serverForWindow(windowIdx)

	// What the shell has started, where that can be asked. This is the
	// reliable half: a shell script — which is what a Gradle or Maven
	// wrapper is — reports itself as "sh", so the command name alone cannot
	// tell a running wrapper from an idle prompt, but a child process can.
	//
	// Only for a tab on this computer: the PID belongs to the machine the
	// pane runs on, and reading this one's process table for it would
	// answer about an unrelated process.
	if paneChildrenKnown && (server == "" || server == i.ServerID) {
		if pid, ok := i.panePID(windowIdx); ok {
			return paneHasChildProcess(pid)
		}
	}

	// Fallback: what the pane is running. Right whenever the command is a
	// real program, and wrong only for a script wrapper, where it reads as
	// the shell.
	output, err := i.tmuxOutputOn(server,
		"display-message", "-p", "-t", target, "#{pane_current_command}")
	if err != nil {
		// Unanswerable: treat as not running, which only ever leads to the
		// command being started rather than something being killed.
		return false
	}
	return !isShellCommand(strings.TrimSpace(string(output)))
}

// panePID returns the PID of the process a tab's pane is running — the
// shell, for a tab at its prompt.
func (i *Instance) panePID(windowIdx int) (int, bool) {
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	output, err := i.tmuxOutputOn(i.serverForWindow(windowIdx),
		"display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// isShellCommand reports whether a pane's current command is just the shell
// waiting at its prompt, meaning nothing is running in it.
//
// Matched by name against the shells a tab can be given, with the login
// dash form ("-bash") included: a login shell reports itself that way and
// would otherwise look like a running command called "-bash".
func isShellCommand(name string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "-")
	if name == "" {
		return true
	}
	switch name {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "tcsh", "csh", "ash",
		"powershell", "pwsh", "cmd", "cmd.exe", "powershell.exe", "pwsh.exe":
		return true
	}
	return false
}

// PaneRunning reports whether a tab's pane is still alive, for callers
// outside this package that need the live answer rather than the stored one.
func (i *Instance) PaneRunning(windowIdx int) bool {
	return i.paneRunning(windowIdx)
}

// StartRunnerCommand runs a runner tab's command in its own tab.
//
// The tab is reused rather than replaced: it is the home of this command, and
// its scrollback is the record of the last run. A tab whose pane has exited —
// which is where a finished or failed run leaves it — is respawned first,
// because a dead pane cannot be typed into.
func (i *Instance) StartRunnerCommand(ctx context.Context, windowIdx int) error {
	command, ok := i.RunnerCommandFor(windowIdx)
	if !ok {
		return fmt.Errorf("this tab has no command to run")
	}
	// Already running: starting again would stack a second command on the
	// first, which is never what ▶ means on a tab that is already going.
	if i.RunnerCommandRunning(windowIdx) {
		return fmt.Errorf("the command is already running")
	}
	// A dead pane cannot be typed into, so it is respawned first. A live
	// one — a shell sitting at its prompt after the last run — is kept, and
	// with it the scrollback of what happened last time.
	if !i.paneRunning(windowIdx) {
		if err := i.RestartWindow(windowIdx); err != nil {
			return fmt.Errorf("could not restart the tab: %w", err)
		}
	}
	// The respawned shell needs its prompt before it can be typed into.
	return i.TypeIntoFreshShell(windowIdx, command, runnerStartTimeout)
}

var runnerStartTimeout = 10 * time.Second

// StopRunnerCommand asks a runner tab's command to stop, and makes sure it
// did.
//
// Ctrl+C first and then the pane, in that order, because the two answer
// different failures: a well-behaved command stops on the interrupt and gets
// to close its files and release its port, while one that ignores it — or a
// child of one — would otherwise be left running after the tab says stopped.
//
// Returns whether the interrupt alone was enough, which is only for the
// caller's logging: either way the command is stopped when this returns.
func (i *Instance) StopRunnerCommand(ctx context.Context, windowIdx int) (gentle bool, err error) {
	if _, ok := i.RunnerCommandFor(windowIdx); !ok {
		return false, fmt.Errorf("this tab has no command to stop")
	}
	if !i.RunnerCommandRunning(windowIdx) {
		// Nothing to stop. Not an error: ■ on a finished command is a
		// no-op, not a failure.
		return true, nil
	}

	if err := i.interruptWindow(windowIdx); err != nil {
		// The interrupt could not even be sent; go straight to the pane.
		if stopErr := i.StopWindowContext(ctx, windowIdx); stopErr != nil {
			return false, fmt.Errorf("could not stop the command: %w", stopErr)
		}
		return false, nil
	}

	if i.waitForPaneToStop(ctx, windowIdx, runnerInterruptGrace) {
		return true, nil
	}
	// Still running after the grace period: take the pane, which takes the
	// shell and everything under it.
	if err := i.StopWindowContext(ctx, windowIdx); err != nil {
		return false, fmt.Errorf("could not stop the command: %w", err)
	}
	return false, nil
}

// interruptWindow sends Ctrl+C to a tab, on the machine the tab runs on.
//
// Without -l, so "C-c" is read as the key rather than typed as two letters —
// the opposite of SendTextToWindow, which sends a saved command literally.
func (i *Instance) interruptWindow(windowIdx int) error {
	run := i.tmuxRun
	if server := i.serverForWindow(windowIdx); server != i.ServerID {
		run = func(args ...string) error { return i.tmuxRunOn(server, args...) }
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	return run("send-keys", "-t", target, "C-c")
}

// waitForPaneToStop polls until a tab's command has ended, or the deadline
// passes. Reports whether it stopped in time.
func (i *Instance) waitForPaneToStop(ctx context.Context, windowIdx int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if !i.RunnerCommandRunning(windowIdx) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		timer := time.NewTimer(runnerPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
