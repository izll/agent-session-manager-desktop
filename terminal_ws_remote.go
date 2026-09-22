package main

import (
	"context"
	"fmt"
	"log"

	"asmgr-desktop/session"
)

// Attaching a terminal that lives on a server.
//
// The only difference from a local attach is where the bytes come from. What
// this returns satisfies the same interface, so the throttling, the
// hold-while-hidden buffering and the WebSocket plumbing above are untouched —
// they never learn which kind of terminal they have.

// remoteAttach is the outcome of trying to attach over SSH.
type remoteAttach struct {
	stream session.TerminalStream
	err    error
}

// attachSetupRun issues one of the attach-time multiplexer options where the
// session lives.
//
// These options — manual window sizing, no status bar, focus events — are
// applied to the session being attached to, so they have to reach the machine
// that holds it. Sent locally for a remote session they would configure
// nothing, or something else of the same name on this computer.
//
// Failures stay ignored here, as they were before: every one of these is a
// preference, and a multiplexer that rejects one still attaches.
func (ts *TerminalServer) attachSetupRun(ctx context.Context, inst *session.Instance, winIdx int, args ...string) {
	if inst == nil {
		_ = terminalTmuxRun(ctx, args...)
		return
	}
	serverID := inst.ServerForWindow(winIdx)
	if serverID == "" {
		_ = terminalTmuxRun(ctx, args...)
		return
	}
	_ = inst.ExecutorOn(serverID).Run(ctx, args...)
}

// sessionAliveProbe asks whether the multiplexer session exists, on the machine
// where it would exist.
//
// The check itself is not new; asking the right machine is. It used to run
// against this computer's multiplexer unconditionally, so a session living on a
// server was asked about here, found missing — it genuinely is not here — and
// the attach was refused with "session not running" before the remote path was
// ever reached. Nothing that runs on a server could be opened at all.
//
// A session with no server keeps the local probe exactly as it was.
func (ts *TerminalServer) sessionAliveProbe(ctx context.Context, inst *session.Instance, winIdx int, tmuxSession string) error {
	if inst == nil {
		return terminalTmuxRun(ctx, "has-session", "-t", tmuxSession)
	}
	// The tab's own machine, not the session's: a tab may sit on a server
	// while its session runs here, and it is that server's multiplexer that
	// holds the window being attached to.
	serverID := inst.ServerForWindow(winIdx)
	if serverID == "" {
		if err := terminalTmuxRun(ctx, "has-session", "-t", tmuxSession); err != nil {
			return err
		}
		// The window too, for the same reason as the remote branch below: a
		// local tab whose window died — an agent that is not installed dies
		// the instant it opens — passed on the strength of the session
		// existing, and the attach then printed the multiplexer's own "can't
		// find window N" into the pane. It reappeared on every reconnect,
		// which is what made it flicker.
		return terminalTmuxRun(ctx, "has-session", "-t",
			fmt.Sprintf("%s:%d", tmuxSession, winIdx))
	}
	if err := inst.ExecutorOn(serverID).Run(ctx, "has-session", "-t", tmuxSession); err != nil {
		return err
	}

	// The window too, not only the session.
	//
	// A tab whose window is gone — killed on the server, or created before
	// the multiplexer was told to keep dead panes — passed this check on the
	// strength of the session existing, and the attach then failed with the
	// multiplexer's own "can't find window N" printed into the pane. Asking
	// here turns that into the ordinary "not running" path, which is what the
	// placeholder is for.
	return inst.ExecutorOn(serverID).Run(ctx, "has-session", "-t",
		fmt.Sprintf("%s:%d", tmuxSession, winIdx))
}

// attachRemote attaches to a window on a server, if that is where the session
// lives.
//
// The second return value says whether this was a remote session at all, so
// the caller can fall through to the local path without having to ask twice.
func (ts *TerminalServer) attachRemote(inst *session.Instance, winIdx int, windowTarget string) (remoteAttach, bool) {
	if inst == nil {
		return remoteAttach{}, false
	}
	// Which machine holds this particular tab. A session running here can
	// still have a tab on a server, and that tab is attached over SSH like any
	// other remote one.
	serverID := inst.ServerForWindow(winIdx)
	if serverID == "" {
		return remoteAttach{}, false
	}

	app := ts.app
	if app == nil {
		// Without the app there is no connection pool to ask. Reported rather
		// than falling back to a local attach, which would open a terminal on
		// the wrong machine — or on nothing at all.
		return remoteAttach{err: errNoRemoteSupport}, true
	}

	// Blocking here on purpose: this is the one place with nothing useful to
	// do until the connection exists, because the user has just opened this
	// tab and is looking at it. Session loading takes the opposite path — it
	// routes with whatever is already connected — so the app never waits on a
	// server just to draw its own window.
	connection, err := app.connectionFor(serverID)
	if err != nil {
		return remoteAttach{err: err}, true
	}
	// The executor may not have been registered yet, if this tab was routed
	// before the connection finished building.
	session.SetTabExecutor(inst.ID, serverID, connection.executor)

	server, err := app.storage.FindServer(serverID)
	extraPath := ""
	if err == nil {
		extraPath = server.ExtraPath
	}

	// The initial size matters: tmux draws to it as soon as the client
	// attaches, and starting at the wrong size shows the user a redraw they
	// did not ask for. The browser sends its real size immediately afterwards.
	stream, err := connection.client.AttachTerminalTo(
		connection.helper, windowTarget, extraPath, defaultRemoteColumns, defaultRemoteRows)
	if err != nil {
		return remoteAttach{err: err}, true
	}

	log.Printf("[terminal] attached to %s on %s", windowTarget, connection.executor.Describe())
	return remoteAttach{stream: stream}, true
}

// The size a remote terminal starts at, before the browser reports its own.
//
// Chosen to match what the mirror sessions use locally, so a pane that is
// redrawn at the real size a moment later moves as little as possible.
const (
	defaultRemoteColumns = 221
	defaultRemoteRows    = 44
)

// errNoRemoteSupport is returned when a remote session is opened by something
// that has no access to the connection pool.
var errNoRemoteSupport = &remoteUnsupportedError{}

type remoteUnsupportedError struct{}

func (*remoteUnsupportedError) Error() string {
	return "this session runs on a server, and the connection to it is not available here"
}
