package session

import (
	"context"
	"os/exec"
	"time"
)

// Command builds any external invocation the app makes, with the Windows
// console window already suppressed. It is a drop-in for exec.Command: the
// caller still owns Env, Dir, Stdin and whether the error is checked.
//
// It exists so a new call site gets the fix by default. Reaching for
// exec.Command directly is the mistake — the console flash is invisible to
// anyone developing on Linux or macOS, so a forgotten call site is only ever
// found by a Windows user.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	HideConsoleWindow(cmd)
	return cmd
}

// CommandContext is Command with a cancellable context, for probes that must
// not outlive the work that asked for them.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	HideConsoleWindow(cmd)
	return cmd
}

// GitCommand builds a git invocation. Git is the worst offender after the
// multiplexer: the branch indicator and the diff run it on a timer, so every
// missed call site is a window flashing open every few seconds.
func GitCommand(args ...string) *exec.Cmd {
	return Command("git", args...)
}

// GitCommandContext is GitCommand with a cancellable context.
func GitCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return CommandContext(ctx, "git", args...)
}

// GitTimeout bounds a single git invocation on a user-facing path.
//
// Generous on purpose: a cold cache over a large repository can legitimately
// take seconds, and killing honest work would be worse than waiting. It exists
// for the case where git does not come back at all — an unreachable network
// share, a stale lock, a filesystem that has stopped answering.
const GitTimeout = 30 * time.Second

// GitCommandTimed builds a git invocation that cannot hang forever.
//
// Every git call behind the UI needs this. The diff view sets a loading flag,
// awaits the backend and clears the flag when it returns — so a git process
// that never exits leaves the spinner turning with no way out, and the call
// blocks the binding it arrived on, which is why other navigation stops
// responding too.
//
// The returned cancel MUST be deferred by the caller: it releases the timer,
// and exec.CommandContext kills the process when the context ends.
func GitCommandTimed(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	return GitCommandContext(ctx, args...), cancel
}
