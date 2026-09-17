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

	if err := session.Start(attachCommand(target, extraPath)); err != nil {
		session.Close()
		return nil, fmt.Errorf("could not attach to the session: %w", err)
	}

	return &TerminalStream{session: session, stdin: stdin, stdout: stdout}, nil
}

// attachCommand builds the command line that attaches to a window.
//
// The locale is set on the command line rather than through the SSH protocol's
// environment request: servers accept only what their AcceptEnv allows, which
// by default is nothing useful, and a rejected variable is not reported. This
// way it either works or the shell says why.
//
// UTF-8 is not optional here. Without it, tmux mangles every accented
// character and every box-drawing character an agent draws — which is most of
// what an agent draws.
func attachCommand(target, extraPath string) string {
	var builder strings.Builder
	if extra := strings.TrimSpace(extraPath); extra != "" {
		builder.WriteString("export PATH=")
		builder.WriteString(shellQuote(extra))
		builder.WriteString(":$PATH; ")
	}
	builder.WriteString("env LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 tmux attach-session -t ")
	builder.WriteString(shellQuote(target))
	return builder.String()
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
