//go:build windows

package session

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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

	// closed is shut when Close runs, so background helpers stop with the
	// stream instead of outliving it.
	closed chan struct{}

	// keys carries keystroke batches to the single goroutine that delivers
	// them. See the Write comment for why delivery is not done inline.
	keys chan []byte

	// hidden is 1 while this tab is not on screen. Set by the WebSocket layer,
	// which is the only place that knows. Kept as an atomic because the size
	// watcher reads it from another goroutine.
	hidden int32
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
	// Hand off rather than deliver inline.
	//
	// Delivery means launching send-keys, and waiting for that process is the
	// entire cost: measured on Windows, 31ms per call waiting for it against
	// 6ms when not. At 31ms a burst of input — a mouse-motion flood is 13 bytes
	// every few milliseconds — queues up behind the launches and typing feels
	// sluggish.
	//
	// A single goroutine drains this channel, so keystrokes still reach the pane
	// in the order they were typed. Doing it without the channel, by simply not
	// waiting, would let two launches overlap and interleave a burst.
	buf := append([]byte(nil), p...)
	select {
	case c.keys <- buf:
		return len(p), nil
	case <-c.closed:
		return 0, io.ErrClosedPipe
	}
}

// deliverKeys sends queued keystrokes, one batch at a time, for the life of the
// stream. Running in exactly one goroutine is what keeps input in order.
func (c *controlModeStream) deliverKeys() {
	for {
		select {
		case <-c.closed:
			return
		case p := <-c.keys:
			// Take anything else already queued: each extra batch would
			// otherwise be another process launch, and a burst is common.
			for len(p) < 4096 {
				select {
				case more := <-c.keys:
					p = append(p, more...)
					continue
				default:
				}
				break
			}
			c.sendKeys(p)
		}
	}
}

// sendKeys delivers one batch to the pane.
func (c *controlModeStream) sendKeys(p []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	target := c.pane
	if target == "" {
		target = c.attachTarget
	}
	// One input burst can need several commands: Enter has to be sent by key
	// name, so a payload containing CR splits around it (see keystrokeCommands).
	for _, args := range keystrokeCommands(target, p) {
		// Bounded: a send-keys that never returns would block every later
		// keystroke behind it — a terminal that accepts focus and clicks while
		// silently swallowing everything typed into it.
		cmd, cancel := TmuxCommandTimed(args...)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			// Logged unconditionally, not behind --debug: input vanishing with
			// no trace is exactly the failure that took days to find once, and
			// the caller can no longer see this error — Write returns as soon
			// as the batch is queued.
			log.Printf("[control] send-keys failed for %s: %v (%s)",
				target, err, strings.TrimSpace(string(out)))
			return
		}
	}
}

// Close shuts the client down in the order the protocol expects: closing stdin
// ends the command channel, which is what makes the multiplexer emit %exit and
// detach cleanly. Only then is the process killed, so a client that already
// left of its own accord is not killed needlessly — but the kill is not
// optional either, because a pipe sends no SIGHUP and an orphaned attach client
// would hold a client slot on the session forever.
func (c *controlModeStream) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
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

	// Forward only this pane's output. A control-mode client is sent %output for
	// every pane on the server, so without this a new tab's startup — including
	// its screen erases — is painted into the tab the user is looking at.
	//
	// The filter needs the %output form of the id (%20), which is not the
	// session-qualified target used for send-keys ($61:0.0), so it is resolved
	// separately. If it cannot be resolved the filter stays off: showing another
	// pane's output is bad, showing nothing at all is worse.
	if id := panePrimaryID(pane); id != "" {
		reader.setPaneFilter(id)
	}

	stream := &controlModeStream{
		closed:       make(chan struct{}),
		keys:         make(chan []byte, 256),
		out:          reader,
		in:           stdin,
		stop:         killProcess(cmd),
		pane:         pane,
		attachTarget: target,
	}

	// One goroutine owns keystroke delivery, so order is preserved without the
	// caller waiting for a process to finish. See Write.
	go stream.deliverKeys()

	// Control mode sends no repaint on attach, so the first screen has to be
	// asked for. See primeFromDumpState — the agent draws it, we do not.
	go primeFromDumpState(stream, pane)

	// Keep the pane matched to its window for as long as this terminal lives.
	go watchPaneSize(stream, pane)

	return stream, nil
}

// paneSize reports the pane's current dimensions.
func paneSize(pane string) (cols, rows int, ok bool) {
	out, err := TmuxCommand("display-message", "-p", "-t", pane,
		"#{pane_width} #{pane_height}").Output()
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &cols, &rows); err != nil {
		return 0, 0, false
	}
	return cols, rows, true
}

// paneHasContent reports whether the pane has drawn anything yet.
//
// A tab opened moments ago is still starting its agent, and asking for a repaint
// before then paints a blank screen — after which control mode sends nothing
// until the next change, leaving the terminal empty indefinitely.
func paneHasContent(pane string) bool {
	if pane == "" {
		return false
	}
	out, err := TmuxCommand("capture-pane", "-p", "-t", pane).Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(out)) > 0
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

	// Size the PANE as well as the client.
	//
	// A pane does not always follow its window: observed on a live session,
	// window 20 measured 174x45 while the pane inside it was still 228x56 — the
	// agent drawing a 56-row UI into a 45-row window, which is the content
	// ending up in the wrong place. resize-pane aimed at the pane corrects it
	// (verified: 228x56 -> 174x45 immediately).
	//
	// Note this is resize-pane on a PANE target. The same command aimed at a
	// window does nothing on psmux, which is why an earlier attempt at this
	// looked like the command was unsupported.
	if c.pane != "" {
		cmd, cancel := TmuxCommandTimed("resize-pane", "-t", c.pane,
			"-x", fmt.Sprintf("%d", cols), "-y", fmt.Sprintf("%d", rows))
		_ = cmd.Run()
		cancel()
		// The window may still disagree — resizing the app window changes the
		// window without the pane following. Left alone the agent draws at the
		// pane's size inside a differently-sized window, and Ctrl-L cannot help
		// because it redraws at the size the agent believes in.
		_ = reconcilePaneSize(c.pane)
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

// panePrimaryID returns a pane's %-prefixed id, the form used in %output lines.
func panePrimaryID(pane string) string {
	if pane == "" {
		return ""
	}
	out, err := TmuxCommand("display-message", "-p", "-t", pane, "#{pane_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// selectWindow points this client at a window, so commands scoped to "the
// client's window" — a resize repaint, above all — act on the right one.
//
// Sent on the control-mode channel rather than as a separate process: the
// window a client is looking at is a property of that client, and an outside
// invocation would move some other client instead.
func (c *controlModeStream) selectWindow(target string) {
	if target == "" {
		return
	}
	// The pane suffix has to go: select-window takes a window, and passing
	// session:window.pane makes psmux reject the target.
	if dot := strings.LastIndexByte(target, '.'); dot > strings.LastIndexByte(target, ':') {
		target = target[:dot]
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = c.in.Write([]byte(fmt.Sprintf("select-window -t %s\n", target)))
}

// primeFromDumpState paints the terminal's opening frame from the
// multiplexer's own state dump.
//
// Control mode sends no repaint on attach, so the first screen has to come from
// somewhere. dump-state is the only source that is self-consistent: it reports
// the pane's rows, its styled contents and its cursor together, in one answer.
// Everything else disagreed with itself — capture-pane's row count depends on
// how the trailing newline is handled, an induced repaint puts the content a
// row off, and display-message and dump-state gave different cursor rows for
// the same pane (43 versus 45).
//
// It waits for the pane to have drawn something: a tab opened moments ago is
// still starting its agent, and a snapshot taken then is blank.
func primeFromDumpState(s *controlModeStream, pane string) {
	const attempts = 24
	for i := 0; i < attempts; i++ {
		if paneHasContent(pane) {
			// Ask the agent to repaint, and let ITS repaint be the opening
			// frame — do not also inject a snapshot.
			//
			// Ctrl-L is what the Refresh button sends (App.RefreshWindow), and
			// it is the one thing that has always recovered a tab: the agent
			// rebuilds its interface from scratch, and control mode carries that
			// repaint to us like any other output.
			//
			// A snapshot on top of it is worse than useless. It is prepended to
			// the read buffer, so by the time it is ready the agent's repaint
			// has already been handed to the terminal — the snapshot then lands
			// behind the frame it was meant to replace, and never shows.
			// Measured: the recorded stream contained no ESC[2J and no absolute
			// row addressing at all, meaning the snapshot never reached the
			// terminal, while the agent's own repaint did and was correct.
			_ = reconcilePaneSize(pane)
			redrawPane(pane)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// redrawPane asks the agent in a pane to repaint from scratch.
//
// Ctrl-L is the conventional "clear and redraw" key, and TUI applications
// rebuild their whole interface on it. Sent by key name so the multiplexer
// delivers it as a control character rather than as text.
func redrawPane(pane string) {
	if pane == "" {
		return
	}
	cmd, cancel := TmuxCommandTimed("send-keys", "-t", pane, "C-l")
	_ = cmd.Run()
	cancel()
}

// reconcilePaneSize makes a pane match the window it lives in.
//
// The two can drift apart: observed after the user resized the app window,
// window 0 measured 174x45 while the pane inside it was still 228x56. The agent
// then draws a 56-row interface into a 45-row window, and the content sits in
// the wrong place — a state the Refresh button cannot fix, because Ctrl-L makes
// the agent redraw at the size it believes in, which is still the wrong one.
//
// Done here rather than waiting for a resize message from the frontend: the
// frontend only sends a size when ITS size changes, and after a window resize
// the pane is the thing that is stale, not the client.
func reconcilePaneSize(pane string) bool {
	if pane == "" {
		return false
	}
	out, err := TmuxCommand("display-message", "-p", "-t", pane,
		"#{window_width} #{window_height} #{pane_width} #{pane_height}").Output()
	if err != nil {
		return false
	}
	var wc, wr, pc, pr int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d %d %d", &wc, &wr, &pc, &pr); err != nil {
		return false
	}
	if wc <= 1 || wr <= 1 || (wc == pc && wr == pr) {
		return false
	}
	cmd, cancel := TmuxCommandTimed("resize-pane", "-t", pane,
		"-x", fmt.Sprintf("%d", wc), "-y", fmt.Sprintf("%d", wr))
	err = cmd.Run()
	cancel()
	return err == nil
}

// watchPaneSize keeps a pane the same size as the window containing it.
//
// Panes do not reliably follow their window on psmux, and the frontend cannot
// close the gap on its own: it sends a size only for the tab the user is
// looking at, so maximising the app window leaves every other tab's pane behind.
// Measured right after maximising — windows all 228x56, panes 174x45 in the two
// tabs that had not been touched.
//
// A stale pane is not cosmetic: the agent draws its interface at the pane's
// size inside a differently-sized window, so the content lands in the wrong
// place, and Refresh cannot fix it because Ctrl-L only makes the agent redraw
// at the size it already believes in.
//
// Polled, because the multiplexer sends no notification for this — but polled
// cheaply. Each check costs one process launch, measured at 20ms on Windows, so
// a two-second interval across five tabs would burn ~5% of a core doing nothing.
// At ten seconds that falls to ~1%, and the delay is not felt: a size only
// drifts when the user resizes the window, and the tab being looked at is
// corrected immediately by its own resize message.
//
// Ends when the stream is closed.
func watchPaneSize(s *controlModeStream, pane string) {
	if pane == "" {
		return
	}
	// A first check soon after attach catches a window that was resized while
	// this tab was not open; after that the interval is what matters.
	const (
		firstCheck = 3 * time.Second
		interval   = 10 * time.Second
	)
	next := firstCheck
	for {
		select {
		case <-s.closed:
			return
		case <-time.After(next):
			next = interval
		}
		// A hidden tab costs nothing to leave alone: it is not being looked
		// at, and showing it sends a resize that corrects it anyway.
		if atomic.LoadInt32(&s.hidden) == 1 {
			continue
		}
		if reconcilePaneSize(pane) {
			// The agent has to be told to repaint at the new size; without this
			// the pane is the right shape but still holds the old drawing.
			redrawPane(pane)
		}
	}
}

// SetTerminalVisible tells a stream whether its tab is on screen.
//
// Used to skip work that only matters for a tab the user can see: the pane-size
// watcher does not need to run for a hidden tab, since it will be corrected by
// its own resize message the moment it is shown.
func SetTerminalVisible(t TerminalStream, visible bool) {
	c, ok := t.(*controlModeStream)
	if !ok {
		return
	}
	var v int32
	if !visible {
		v = 1
	}
	atomic.StoreInt32(&c.hidden, v)
}
