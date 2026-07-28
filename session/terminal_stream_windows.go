//go:build windows

package session

import (
	"os/exec"
)

// StartTerminal wires the multiplexer to ordinary anonymous pipes.
//
// This is NOT a degraded fallback, and it should not be "fixed" back to a
// pseudo-terminal. creack/pty has no Windows implementation at all
// (StartWithSize is literally `return nil, ErrUnsupported`), but more
// importantly we do not need one: the Windows multiplexer is psmux, which
// drives its own ConPTY internally and speaks ANSI down whatever handle it is
// given. Redirecting `psmux attach-session` to a plain file on a real Windows
// machine produces a genuine escape stream — OSC colour queries and a DA
// response among them — with no console attached anywhere. So the bytes
// xterm.js needs already arrive over a pipe; putting a second pseudo-terminal
// in front of psmux's own would only add a layer to translate through.
//
// The two things a pipe cannot do are carried elsewhere: sizing goes through
// the multiplexer's resize-window command (see SetTerminalSize), and process
// teardown is explicit rather than a SIGHUP from closing a PTY master (see
// killProcess).
func StartTerminal(cmd *exec.Cmd) (TerminalStream, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// Fold stderr into the same pipe rather than opening a second one. The
	// multiplexer reports its refusals ("no such session", "open terminal
	// failed") on stderr, and with a separate handle nobody reads those would
	// vanish and the terminal would just look blank. In a terminal stream both
	// belong on the same channel anyway — that is what a PTY does implicitly.
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}

	// os.Pipe handles are unbuffered — bytes are readable as soon as psmux
	// writes them, so the terminal cannot lag a buffer behind. Nothing here
	// wraps them in a bufio, and nothing should.
	return newTerminalPipes(stdout, stdin, killProcess(cmd)), nil
}

// SetTerminalSize is a no-op on Windows: a pipe carries no window size, and
// there is no ioctl to call. Every caller already issues the multiplexer's own
// resize-window for the target window immediately after this — that command is
// the only sizing lever on this platform, and it is enough, because psmux owns
// the ConPTY whose size actually matters.
func SetTerminalSize(s TerminalStream, cols, rows int) error {
	return nil
}
