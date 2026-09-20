package remote

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Attaching to a terminal on a server.
//
// This does not go through the helper's request-response protocol: a terminal
// is a stream, not a question. It gets its own SSH channel, which is what the
// protocol is for — several independent channels over one connection.
//
// What comes back satisfies the same interface a local terminal does, so
// everything above this — the WebSocket server, the throttling, the hold-while-
// hidden logic, the xterm on the other end — is unchanged and untouched.

// TerminalStream is a remote terminal: bytes in, bytes out, and a size.
//
// The shape is dictated by what the local terminal already provides, because
// the code that consumes it must not need to know which it has.
type TerminalStream struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader

	mu     sync.Mutex
	closed bool
}

// AttachTerminal opens a terminal on the server and attaches it to a session.
//
// The size is given up front because tmux draws to it immediately; a terminal
// that starts at the wrong size and is corrected afterwards shows a redraw the
// user did not ask for.
func AttachTerminal(client *Client, target string, extraPath string,
	columns, rows int) (*TerminalStream, error) {

	session, err := client.SSH().NewSession()
	if err != nil {
		return nil, err
	}

	// A real pty, because tmux attach-client requires one: given a pipe it
	// writes a short refusal and exits. This is the same constraint that made
	// the Windows port need control mode.
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, columns, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("the server refused a terminal: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	// One mirror per window, named after it, so two attaches to the same tab
	// reuse one rather than stacking up.
	mirror := mirrorNameFor(target)
	if err := session.Start(mirrorAttachCommand(target, mirror, extraPath, columns, rows)); err != nil {
		session.Close()
		return nil, fmt.Errorf("could not attach to the session: %w", err)
	}

	return &TerminalStream{session: session, stdin: stdin, stdout: stdout}, nil
}

// mirrorAttachCommand builds a command that attaches through a mirror session
// of this window alone.
//
// Why a mirror rather than attaching to the session directly: every tab of a
// session attaches to the SAME multiplexer session, and a multiplexer sizes a
// window to the smallest client watching it. With three tabs open the smallest
// one decided the size for all of them, and the extra rows came back as a band
// of dots the terminal could not use — measured on a live server, clients at
// 223x60, 223x60 and 223x66 leaving six rows dead.
//
// The mirror holds one linked window — the same window object, so the agent
// inside it is untouched — and nothing else attaches to it, so its size is
// this tab's size alone. The local terminal solves the same problem the same
// way; this is that device on the far side.
//
// The mirror is named after the window so a reconnection reuses it rather than
// accumulating one per attach: creating it is allowed to fail, which is what
// happens when it is already there.
func mirrorAttachCommand(target, mirror, extraPath string, columns, rows int) string {
	var builder strings.Builder
	if extra := strings.TrimSpace(extraPath); extra != "" {
		builder.WriteString("export PATH=")
		builder.WriteString(shellQuote(extra))
		builder.WriteString(":$PATH; ")
	}
	builder.WriteString("env LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 sh -c ")

	// Built as one shell command so the whole sequence runs on the far side in
	// a single round trip: create-or-reuse the mirror, link this window into
	// it, then attach. A link that fails — the window vanished between the
	// listing and now — falls back to attaching to the session itself, which
	// is what this did before and is still better than no terminal.
	inner := fmt.Sprintf(
		"tmux new-session -d -s %s -x %d -y %d 2>/dev/null; "+
			"tmux link-window -k -s %s -t %s 2>/dev/null; "+
			"tmux set-option -t %s status off 2>/dev/null; "+
			"exec tmux attach-session -t %s 2>/dev/null || exec tmux attach-session -t %s",
		shellQuote(mirror), columns, rows,
		shellQuote(target), shellQuote(mirror+":"+windowPart(target)),
		shellQuote(mirror),
		shellQuote(mirror+":"+windowPart(target)),
		shellQuote(target))
	builder.WriteString(shellQuote(inner))
	return builder.String()
}

// windowPart returns the window index from a "session:index" target.
func windowPart(target string) string {
	if at := strings.LastIndex(target, ":"); at >= 0 {
		return target[at+1:]
	}
	return target
}


// mirrorNameFor names the mirror belonging to one window.
//
// Derived from the target rather than randomised so a reattach finds the same
// mirror: a new name per attach would leave the old ones behind, each still
// counted as a client watching the window.
func mirrorNameFor(target string) string {
	return "asmgr_view_" + strings.NewReplacer(":", "_", ".", "_", "$", "_").Replace(target)
}

// Read carries the server's output towards the browser.
func (t *TerminalStream) Read(buffer []byte) (int, error) {
	return t.stdout.Read(buffer)
}

// Write carries the user's keystrokes to the server.
func (t *TerminalStream) Write(data []byte) (int, error) {
	return t.stdin.Write(data)
}

// SetSize tells the far end the window changed.
//
// The SSH protocol has a message for exactly this, so a resize costs one
// packet rather than a command.
func (t *TerminalStream) SetSize(columns, rows int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	return t.session.WindowChange(rows, columns)
}

// Close detaches.
//
// Closing this ends the attachment, not the session: the tmux session on the
// server carries on, which is the entire reason for running it there.
func (t *TerminalStream) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	t.stdin.Close()
	return t.session.Close()
}

// AttachTerminalTo opens a terminal on this connection.
//
// A method on Client rather than a loose function so the caller does not have
// to reach inside for the SSH connection. The helper is taken but unused: it
// is what proves the connection has been set up, and keeping it in the
// signature stops a caller attaching to a server whose helper never started.
func (c *Client) AttachTerminalTo(helper *Helper, target, extraPath string,
	columns, rows int) (*TerminalStream, error) {

	if helper == nil {
		return nil, fmt.Errorf("this server has no helper running")
	}
	return AttachTerminal(c, target, extraPath, columns, rows)
}
