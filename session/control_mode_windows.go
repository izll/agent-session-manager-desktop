//go:build windows

package session

import (
	"fmt"
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
	// the lookup failed; writes then fall back to attachTarget, which still
	// reaches the session's active pane.
	pane string

	// attachTarget is the session this client attached to — the fallback
	// send-keys target when the pane lookup came back empty.
	attachTarget string

	// writeMu serialises command writes. A command is a whole line and two
	// interleaved writes would produce one corrupt line — and Write is called
	// from the WebSocket goroutine while Close may run from another.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

func (c *controlModeStream) Read(p []byte) (int, error) { return c.out.Read(p) }

// Write delivers the keystrokes with a separate send-keys process rather than
// down the control-mode command channel, because that channel mangles the
// payload — see keystrokeArgs for the measurements.
//
// The byte count reported is len(p): the caller is told how much of ITS input
// was consumed, not how many wire bytes the command expanded to, or
// io.Copy-style callers would see a short-write error on every keypress.
func (c *controlModeStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Still serialised: two concurrent send-keys processes would interleave
	// their keystrokes in the pane, so a burst could arrive out of order.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	target := c.pane
	if target == "" {
		target = c.attachTarget
	}
	// One input burst can need several commands: Enter has to be sent by key
	// name, so a payload containing CR splits around it (see keystrokeCommands).
	for _, args := range keystrokeCommands(target, p) {
		// Bounded, because this runs under writeMu: a send-keys that never
		// returns would hold the lock forever and every later keystroke would
		// block behind it — a terminal that takes focus and accepts clicks
		// while silently swallowing everything typed into it, with the socket
		// still healthy and nothing logged.
		cmd, cancel := TmuxCommandTimed(args...)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return 0, fmt.Errorf("send-keys: %w (%s)", err, strings.TrimSpace(string(out)))
		}
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

	pane := resolveActivePane(target)
	reader := newControlModeReader(stdout)
	// Control mode sends no initial repaint, so an already-painted session would
	// render as a blank terminal until its next redraw — see primeWithScreen.
	reader.primeWithScreen(captureScreen(pane))

	return &controlModeStream{
		out:          reader,
		in:           stdin,
		stop:         killProcess(cmd),
		pane:         pane,
		attachTarget: target,
	}, nil
}

// captureScreen returns the pane's current contents with escape sequences
// intact, for use as the terminal's opening frame.
//
// -e keeps the SGR sequences, so colours and box drawing arrive as the agent
// drew them; without it the snapshot would be plain text and the UI would
// flash from monochrome to colour on the first live update.
//
// A failure here is not fatal: an empty snapshot simply means the terminal
// starts blank and fills in on the next redraw, which is strictly better than
// refusing to attach at all.
func captureScreen(pane string) []byte {
	if pane == "" {
		return nil
	}
	out, err := TmuxCommand("capture-pane", "-e", "-p", "-t", pane).Output()
	if err != nil {
		return nil
	}
	return out
}

// SetTerminalSize tells the multiplexer how large this client's terminal is,
// over the control-mode channel.
//
// A pipe carries no window size and there is no ioctl to call, so psmux falls
// back to a default 120x30 — and a session stuck at 120 columns inside a wider
// xterm.js wraps every line in the wrong place, which is the staircased,
// doubled-up layout this fixes.
//
// It has to be `refresh-client -C <cols>,<rows>` sent DOWN THE CONTROL-MODE
// CHANNEL. Both halves of that matter, and both were measured:
//
//   - psmux has no resize-window at all; the command exits 0 and changes
//     nothing. resize-pane -x/-y is documented but equally inert here.
//   - the same refresh-client run as a separate process is ignored, because
//     the size belongs to a client and an outside process is not this client.
//     Issued on our own channel it takes effect immediately: 100,25 and
//     137,42 both applied exactly.
//
// A resize failure is reported but not fatal to the stream: the terminal keeps
// working at its previous size, which beats tearing down a live session.
func SetTerminalSize(s TerminalStream, cols, rows int) error {
	c, ok := s.(*controlModeStream)
	if !ok || cols <= 0 || rows <= 0 {
		return nil
	}
	// Shares writeMu with keystrokes: this is a command on the same channel,
	// and a half-written line would corrupt whichever command follows.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.in.Write([]byte(fmt.Sprintf("refresh-client -C %d,%d\n", cols, rows)))
	return err
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

// resolveActivePane returns the send-keys target for a session.
//
// It deliberately does NOT return a bare pane id. Pane ids are only unique
// within one server, and psmux runs a server per SESSION — so every session
// numbers its panes from scratch and the first pane of each is %1. Measured:
// with two sessions open, `send-keys -t %1` succeeded, reported no error, and
// delivered into the OTHER session's pane. Nothing failed; the keystrokes
// simply arrived somewhere else, leaving one terminal accepting focus and
// clicks while swallowing everything typed into it.
//
// A session-qualified target resolves unambiguously — verified on the same two
// sessions: `$61:0.0` landed in $61 and not in $62 — so the session's own
// window.pane coordinates are used instead of the global id.
//
// The lookup is best-effort: on failure the caller falls back to the attach
// target itself, which the multiplexer resolves to that session's active pane.
func resolveActivePane(target string) string {
	if target == "" {
		return ""
	}
	// window_index.pane_index are per-session coordinates; prefixed with the
	// session they cannot collide with an identically-numbered pane elsewhere.
	out, err := TmuxCommand("display-message", "-p", "-t", target,
		"#{window_index}.#{pane_index}").Output()
	if err != nil {
		return target
	}
	if coords := strings.TrimSpace(string(out)); coords != "" && coords != "." {
		return target + ":" + coords
	}
	return target
}
