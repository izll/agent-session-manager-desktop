//go:build !windows

package session

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// StartTerminal gives the multiplexer a real PTY. tmux is a terminal client:
// it wants a controlling terminal to attach to, negotiates size through the
// TIOCSWINSZ ioctl, and gets its SIGHUP from the master being closed. Nothing
// about this changed when Windows support was added — the *os.File a PTY
// returns already satisfies TerminalStream.
func StartTerminal(cmd *exec.Cmd) (TerminalStream, error) {
	return pty.Start(cmd)
}

// SetTerminalSize performs the window-size ioctl on the PTY master. tmux
// reacts to the resulting SIGWINCH, which is what makes the pane follow the
// xterm.js viewport.
func SetTerminalSize(s TerminalStream, cols, rows int) error {
	f, ok := s.(*os.File)
	if !ok {
		return nil
	}
	return pty.Setsize(f, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}
