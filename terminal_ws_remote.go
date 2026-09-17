package main

import (
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

// attachRemote attaches to a window on a server, if that is where the session
// lives.
//
// The second return value says whether this was a remote session at all, so
// the caller can fall through to the local path without having to ask twice.
func (ts *TerminalServer) attachRemote(inst *session.Instance, windowTarget string) (remoteAttach, bool) {
	if inst == nil || inst.ServerID == "" {
		return remoteAttach{}, false
	}

	app := ts.app
	if app == nil {
		// Without the app there is no connection pool to ask. Reported rather
		// than falling back to a local attach, which would open a terminal on
		// the wrong machine — or on nothing at all.
		return remoteAttach{err: errNoRemoteSupport}, true
	}

	connection, err := app.connectionFor(inst.ServerID)
	if err != nil {
		return remoteAttach{err: err}, true
	}

	server, err := app.storage.FindServer(inst.ServerID)
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
