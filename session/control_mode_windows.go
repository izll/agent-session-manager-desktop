//go:build windows

package session

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
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
	//
	// An established session captures immediately. A tab opened moments ago has
	// not drawn anything yet, and that case is retried in the background rather
	// than here: blocking the attach would delay every terminal for the sake of
	// the one that is still starting.
	if screen := captureScreen(pane); len(bytes.TrimSpace(screen)) > 0 {
		reader.primeWithScreen(screen)
	} else {
		go func() {
			if late := captureScreenWhenReady(pane); len(late) > 0 {
				reader.primeWithScreen(late)
			}
		}()
	}

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
	screen := trimCaptureTrailer(out)
	if len(bytes.TrimSpace(screen)) == 0 {
		return nil
	}
	// Drawn from the top-left, so the snapshot lands where the pane's rows
	// actually are rather than wherever the cursor happened to be. Without this
	// the picture starts at the current cursor row and the whole screen is
	// offset — and the trailing cursor-home leaves the cursor somewhere
	// harmless until the agent's next redraw places it properly.
	out = make([]byte, 0, len(screen)+8)
	out = append(out, "\033[H"...)
	out = append(out, screen...)
	return out
}

// captureScreenWhenReady returns the pane's contents, waiting briefly for a
// pane that has not drawn anything yet.
//
// A tab opened moments ago is still starting its agent, so the first capture
// comes back blank — and blank is unrecoverable here, because control mode only
// reports CHANGES: with no opening frame and no redraw, the terminal shows an
// empty pane indefinitely while input still works. Measured on a freshly opened
// tab: zero bytes on attach, zero after a refresh-client, and output only once
// a keystroke forced a change.
//
// Waiting is bounded and only costs anything when the pane really is empty; an
// established session captures on the first try and returns immediately.
func captureScreenWhenReady(pane string) []byte {
	const attempts = 12
	for i := 0; i < attempts; i++ {
		if screen := captureScreen(pane); len(bytes.TrimSpace(screen)) > 0 {
			return screen
		}
		if i < attempts-1 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	return nil
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

	// Nudge the size before setting it, so the pane is redrawn in full.
	//
	// Only a CHANGE of size makes psmux repaint. Measured on an attached client
	// whose screen had gone stale:
	//
	//   refresh-client                 ->     0 bytes
	//   refresh-client -C <same size>  ->     0 bytes
	//   refresh-client -C <different>  -> 27510 bytes
	//   select-window                  ->     0 bytes
	//
	// That is why resizing the window by hand was the only thing that fixed a
	// blank tab, and why re-sending the same dimensions never did: attaching
	// another tab resizes the whole session (psmux sizes per session), leaving
	// the other client's screen invalid with nothing to trigger a repaint.
	//
	// One column narrower is enough and is never seen: the corrected size lands
	// immediately after, and the pane is only ever rendered by us.
	if cols > 2 {
		if _, err := c.in.Write([]byte(fmt.Sprintf("refresh-client -C %d,%d\n", cols-1, rows))); err != nil {
			return err
		}
	}
	if _, err := c.in.Write([]byte(fmt.Sprintf("refresh-client -C %d,%d\n", cols, rows))); err != nil {
		return err
	}

	// Clear the terminal ahead of the repaint that is now on its way.
	//
	// The repaint psmux sends begins with ESC[H — cursor home — but never
	// ESC[2J, so it paints over whatever is on screen without clearing it.
	// Measured on a redraw triggered this way: the payload opens with
	// `\033[?25l\033[38;5;174m\033[H` and no erase anywhere in it. Any row the
	// new frame does not happen to overwrite survives, which offsets everything
	// below it — the one-line shift left in a tab that was already open.
	//
	// Injected into the READ side, not written to the command channel: this is
	// a sequence for the terminal emulator, not a command for the multiplexer.
	//
	// It has to lead the repaint, so it is queued to go out BEFORE the next
	// %output rather than appended behind whatever is buffered now — appending
	// would clear the frame already on screen and then let the stale rows come
	// back underneath the new one.
	c.out.clearBeforeNextOutput()
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
	// A target that already names a window (session:index) is left alone, only
	// gaining the pane. Re-deriving it from the session would replace the
	// caller's window with whichever one is ACTIVE, and opening a tab changes
	// that — which is how input for one tab ended up addressed to another.
	if strings.Contains(target, ":") {
		out, err := TmuxCommand("display-message", "-p", "-t", target,
			"#{pane_index}").Output()
		if err != nil {
			return target
		}
		if pane := strings.TrimSpace(string(out)); pane != "" {
			return target + "." + pane
		}
		return target
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
