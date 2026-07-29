package session

import (
	"context"
	"os/exec"
	"strings"
	"sync"
)

// Every tmux invocation in the app is built here, so the multiplexer binary is
// named in exactly one place. tmux itself doesn't run on Windows; psmux is a
// native, tmux-compatible multiplexer that answers the same subcommands, so
// pointing this at "psmux" is all a Windows build should need. That is why the
// name is a variable and not a constant — it has to be swappable at runtime,
// not just at compile time.
//
// The mutex is not about contention (the name is written at most once, during
// start-up) but about the race detector: commands are constructed from the
// WebSocket, status-poller and UI goroutines concurrently.
var (
	tmuxBinaryMu sync.RWMutex
	tmuxBinary   = defaultTmuxBinary
)

// SetTmuxBinary overrides the multiplexer executable, by name (resolved via
// PATH) or absolute path. Passing an empty string restores the default, so a
// cleared setting can't leave the app unable to find any binary at all.
func SetTmuxBinary(name string) {
	tmuxBinaryMu.Lock()
	defer tmuxBinaryMu.Unlock()
	if name == "" {
		name = defaultTmuxBinary
	}
	tmuxBinary = name
}

// TmuxBinary reports the executable that TmuxCommand will run.
func TmuxBinary() string {
	tmuxBinaryMu.RLock()
	defer tmuxBinaryMu.RUnlock()
	return tmuxBinary
}

// TmuxCommand builds a tmux invocation. It is a drop-in for
// exec.Command(TmuxBinary(), args...): the caller still owns Env, Dir, Stdin
// and whether the error is checked.
func TmuxCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(TmuxBinary(), args...)
	HideConsoleWindow(cmd)
	return cmd
}

// TmuxCommandContext is TmuxCommand with a cancellable context, for the probes
// that must not outlive the UI interaction that asked for them.
func TmuxCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, TmuxBinary(), args...)
	HideConsoleWindow(cmd)
	return cmd
}

// SessionIDFor returns the multiplexer's stable id ($61) for a session name,
// or "" if it cannot be determined.
//
// Attaching by name is not reliable on psmux: two clients aimed at two
// different sessions were both bound to the same one, leaving the other with
// no client — a terminal that neither displays nor accepts anything, next to
// one that works. Ids resolve exactly.
//
// The match here is exact and done locally, deliberately. Asking the
// multiplexer to resolve a single name is what cannot be trusted, so this
// lists every session and compares names itself.
func SessionIDFor(name string) string {
	if name == "" {
		return ""
	}
	out, err := TmuxCommand("list-sessions", "-F", "#{session_id} #{session_name}").Output()
	if err != nil {
		return ""
	}
	return lookupSessionID(string(out), name)
}

// lookupSessionID finds a session id in a list-sessions listing, matching the
// name exactly. Split out from the command so the matching — the part that can
// actually be wrong — is testable without a multiplexer.
func lookupSessionID(listing, name string) string {
	if name == "" {
		return ""
	}
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimRight(line, "\r")
		// "$61 name": split once, because a session name may contain spaces.
		sp := strings.IndexByte(line, ' ')
		if sp <= 0 {
			continue
		}
		if line[sp+1:] == name {
			return line[:sp]
		}
	}
	return ""
}
