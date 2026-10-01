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

// StartTerminalWithSize is StartTerminal with the PTY at the viewer's size
// from the start. Without one the PTY starts at 0x0, which tmux takes for
// 80x24, and with window-size "latest" the window shrank to that until the
// real size arrived a moment later: an agent such as Claude Code redrew its
// whole output at 80 columns in between, and the scrollback kept that narrow
// copy. Zero sizes fall back to StartTerminal.
func StartTerminalWithSize(cmd *exec.Cmd, cols, rows int) (TerminalStream, error) {
	if cols <= 0 || rows <= 0 {
		return StartTerminal(cmd)
	}
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// SetTerminalSize performs the window-size ioctl on the PTY master. tmux
// reacts to the resulting SIGWINCH, which is what makes the pane follow the
// xterm.js viewport.
func SetTerminalSize(s TerminalStream, cols, rows int) error {
	// A stream that knows how to resize itself — a remote terminal over SSH —
	// is asked first: there is no local file descriptor to act on.
	if handled, err := resizeBySelf(s, cols, rows); handled {
		return err
	}
	f, ok := s.(*os.File)
	if !ok {
		return nil
	}
	return pty.Setsize(f, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

// SetTerminalVisible is a no-op on Unix: nothing here changes behaviour based
// on whether a tab is on screen. It exists so callers stay platform-agnostic.
func SetTerminalVisible(t TerminalStream, visible bool) {}
