package session

import (
	"io"
	"os/exec"
)

// TerminalStream is the app's handle on an attached multiplexer process: a
// bidirectional byte stream carrying the raw ANSI the frontend feeds to
// xterm.js. It exists because the two platforms cannot supply that stream the
// same way — see StartTerminal in terminal_stream_unix.go /
// terminal_stream_windows.go — and every caller only ever reads, writes and
// closes, so nothing beyond io.ReadWriteCloser needs to be exposed.
type TerminalStream interface {
	io.ReadWriteCloser
}

// StartTerminal runs cmd with its standard streams wired to a terminal-ish
// byte stream and returns that stream. The caller keeps ownership of cmd: it
// still has to Wait (or Kill) the process, exactly as it did when this was a
// bare pty.Start.
//
// Implemented per platform; the doc comment lives here so both builds share it.

// SetTerminalSize tells the terminal stream its new dimensions, where the
// platform has a channel for that (a PTY ioctl on Unix). It is best-effort and
// deliberately silent about "not supported": on Windows there is no such
// channel, and the multiplexer's own resize-window command — which every
// caller already issues alongside this — is the whole mechanism there. Callers
// must therefore never treat a nil return as "the pane was resized".
//
// Implemented per platform.

// terminalPipes joins a child process's stdin and stdout into the single
// ReadWriteCloser the callers expect. Only the Windows build constructs one,
// but it lives here so it is covered by the ordinary (Linux) test run —
// nothing in it is OS-specific, and a joiner that is only compiled on the
// platform nobody develops on is a joiner that silently rots.
type terminalPipes struct {
	out io.ReadCloser
	in  io.WriteCloser
	// stop releases the child process. Close must call it: closing the pipes
	// alone leaves the multiplexer attached and running with nobody reading
	// it, which is exactly what happens when the WebSocket goes away
	// mid-stream — the browser tab vanishes, our reader returns, and without
	// this the attach client would linger forever holding a client slot on the
	// session.
	stop func()
}

func (t *terminalPipes) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *terminalPipes) Write(p []byte) (int, error) { return t.in.Write(p) }

// Close releases both halves and the process, and reports the first real
// error. Both pipes are closed even if the first close fails, so a failure on
// one handle cannot leak the other.
func (t *terminalPipes) Close() error {
	errIn := t.in.Close()
	errOut := t.out.Close()
	if t.stop != nil {
		t.stop()
	}
	if errIn != nil {
		return errIn
	}
	return errOut
}

// newTerminalPipes is the constructor used by the Windows build and by the
// tests; taking the halves as interfaces is what lets the test drive it with
// io.Pipe instead of a real process.
func newTerminalPipes(out io.ReadCloser, in io.WriteCloser, stop func()) *terminalPipes {
	return &terminalPipes{out: out, in: in, stop: stop}
}

// killProcess is the default stop func. It kills but deliberately does NOT
// Wait: every caller of StartTerminal already reaps its own cmd, and
// exec.Cmd.Wait is not safe to call twice, so reaping here would race the
// caller's teardown instead of helping it.
//
// The kill itself is not optional. With a PTY, closing the master sends the
// child a SIGHUP and it exits on its own; a plain pipe has no such courtesy,
// so without this an attach client survives its own closed WebSocket and sits
// on the session forever.
func killProcess(cmd *exec.Cmd) func() {
	return func() {
		if cmd == nil || cmd.Process == nil {
			return
		}
		_ = cmd.Process.Kill()
	}
}
