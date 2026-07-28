//go:build windows

package session

import (
	"io"
	"os/exec"
	"strings"
	"sync"
)

// controlModeStream is the Windows TerminalStream: a control-mode client
// wearing the shape of a PTY. Reads yield decoded pane output; writes are
// keystrokes, re-encoded as send-keys commands (see encodeKeystrokes).
type controlModeStream struct {
	out  *controlModeReader
	in   io.WriteCloser
	stop func()

	// pane is the send-keys target, resolved once at attach time. Empty means
	// the lookup failed; writes then fall back to the attach target, which
	// still reaches the session's active pane.
	pane string

	// writeMu serialises command writes. A command is a whole line and two
	// interleaved writes would produce one corrupt line — and Write is called
	// from the WebSocket goroutine while Close may run from another.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

func (c *controlModeStream) Read(p []byte) (int, error) { return c.out.Read(p) }

// Write encodes the keystrokes as a send-keys command. The byte count reported
// is len(p) — the caller is told how much of ITS input was consumed, not how
// many wire bytes the command expanded to, or io.Copy-style callers would see a
// short-write error on every keypress.
func (c *controlModeStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.in.Write(encodeKeystrokes(c.pane, p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close shuts the client down in the order the protocol expects: closing stdin
// ends the command channel, which is what makes the multiplexer emit %exit and
// detach cleanly. Only then is the process killed, so a client that already
// left of its own accord is not killed needlessly — but the kill is not
// optional either, because a pipe sends no SIGHUP and an orphaned attach client
// would hold a client slot on the session forever.
func (c *controlModeStream) Close() error {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		c.closeErr = c.in.Close()
		c.writeMu.Unlock()
		if c.stop != nil {
			c.stop()
		}
	})
	return c.closeErr
}

// StartTerminal attaches in control mode.
//
// See control_mode.go for WHY: a plain `psmux attach-session` over a pipe
// prints its version banner and exits, because the attach client requires a
// real console. Control mode is the supported way for a program to drive the
// multiplexer, and it is what actually renders the agent UI on Windows.
//
// The caller hands us an ordinary attach-session command (built in
// terminal_ws.go / app.go, which stay platform-agnostic); the -CC flag is
// injected here so neither caller has to know the transport.
func StartTerminal(cmd *exec.Cmd) (TerminalStream, error) {
	target := attachTargetOf(cmd)
	insertControlFlag(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// Fold stderr into the same pipe. The multiplexer reports its refusals
	// ("no such session") on stderr; with a separate handle nobody reads, a
	// failed attach would look like a blank terminal instead of an error.
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}

	return &controlModeStream{
		out:  newControlModeReader(stdout),
		in:   stdin,
		stop: killProcess(cmd),
		pane: resolveActivePane(target),
	}, nil
}

// SetTerminalSize is a no-op on Windows: a pipe carries no window size and
// there is no ioctl to call. Every caller already issues the multiplexer's own
// resize-window for the target window immediately after this, and that command
// is the whole sizing mechanism on this platform, because psmux owns the
// ConPTY whose size actually matters.
func SetTerminalSize(s TerminalStream, cols, rows int) error {
	return nil
}

// insertControlFlag turns `attach-session -t X` into `-CC attach-session -t X`.
// -CC is a server-level flag, so it belongs before the subcommand, not after.
func insertControlFlag(cmd *exec.Cmd) {
	for _, a := range cmd.Args {
		if a == "-CC" {
			return
		}
	}
	if len(cmd.Args) == 0 {
		cmd.Args = []string{cmd.Path, "-CC"}
		return
	}
	rest := append([]string{"-CC"}, cmd.Args[1:]...)
	cmd.Args = append(cmd.Args[:1:1], rest...)
}

// attachTargetOf recovers the `-t <target>` the caller asked to attach to, so
// keystrokes can be aimed at that session's pane.
func attachTargetOf(cmd *exec.Cmd) string {
	for i, a := range cmd.Args {
		if a == "-t" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}

// resolveActivePane finds the pane send-keys should target.
//
// It must be resolved rather than assumed: pane ids are numbered per SERVER,
// not per session, so the first pane of a fresh session is whatever number the
// server has reached — %657 on a busy server, not %0. Targeting a guessed id
// would deliver the user's keystrokes into an unrelated session's pane, which
// is far worse than dropping them.
//
// The lookup is best-effort: on failure the caller falls back to the attach
// target itself, which the multiplexer resolves to that session's active pane.
func resolveActivePane(target string) string {
	if target == "" {
		return ""
	}
	out, err := TmuxCommand("display-message", "-p", "-t", target, "#{pane_id}").Output()
	if err != nil {
		return target
	}
	if pane := strings.TrimSpace(string(out)); pane != "" {
		return pane
	}
	return target
}
