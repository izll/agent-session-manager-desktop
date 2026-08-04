package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// Ctrl-L is the only thing that makes a bottom-aligned TUI lay itself out again
// — resize-window and refresh-client repaint the pane buffer, not the program's
// own idea of its geometry. It is also input: an agent prompt reads it as text,
// and Claude Code turned stray ones into "/clear" in the composer.
//
// What produced those was never one keystroke but several close together, from
// a resize, a tab switch, and the resize that follows a tab switch. Two paths
// send one now: the queue, which coalesces a run of requests into one; and a
// tab becoming visible, which sends at once because its repaint is otherwise
// partial — and which clears the queue and records the send so the resize
// following it cannot pair with it.
func TestAutomaticRedrawsAreCoalesced(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}

	src := string(b)
	// The queue's own send is the one legitimate keystroke; everything outside
	// queueRedraw bypasses the coalescing that keeps them from pairing.
	queueStart := strings.Index(src, "func queueRedraw(")
	if queueStart < 0 {
		t.Fatal("queueRedraw not found; if it was renamed, update this test")
	}
	queueEnd := queueStart + strings.Index(src[queueStart:], "\n}\n")

	showStart := strings.Index(src, "wasHidden := atomic.SwapInt32(&tc.hidden, 0) == 1")
	showEnd := showStart
	if showStart >= 0 {
		showEnd = showStart + strings.Index(src[showStart:], "} else {")
	}

	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, `"send-keys"`) || strings.Contains(line, "//") {
			continue
		}
		at := strings.Index(src, line)
		if at >= queueStart && at <= queueEnd {
			continue // inside queueRedraw
		}
		if showStart >= 0 && at >= showStart && at <= showEnd {
			continue // the immediate redraw when a tab becomes visible
		}
		t.Errorf("terminal_ws.go:%d sends a keystroke outside the queue; several "+
			"arriving close together are what put a stray \"/clear\" into the "+
			"agent's composer:\n  %s", i+1, strings.TrimSpace(line))
	}
}

// The quiet period has to outlast the bursts that produced paired keystrokes.
// Measured gaps between redraw requests in ordinary use ran up to ~930ms.
func TestRedrawQuietPeriodOutlastsObservedBursts(t *testing.T) {
	observed := []int{16, 156, 675, 761, 929}
	for _, period := range []time.Duration{redrawQuietPeriod, redrawQuietPeriodClaude} {
		for _, gap := range observed {
			if period <= time.Duration(gap)*time.Millisecond {
				t.Errorf("a quiet period of %v lets a request %dms after another start "+
					"a second keystroke instead of joining the first", period, gap)
			}
		}
	}
}

// Claude Code waits longer than the rest.
//
// A stray keystroke is only expensive where it turns into a command: Claude
// Code reads one as "/clear" in the composer. Other agents have no such
// command, so a keystroke arriving at an awkward moment is at worst ignored,
// and their panes can come right sooner.
func TestClaudeWaitsLongerThanOtherAgents(t *testing.T) {
	claude := redrawQuietFor(session.AgentClaude)
	codex := redrawQuietFor(session.AgentCodex)

	if claude <= codex {
		t.Errorf("Claude waits %v and Codex %v; Claude is the one that turns a stray "+
			"keystroke into a command, so it must not wait less", claude, codex)
	}
	// An unknown agent must get the cautious end of the range rather than no
	// wait at all.
	if got := redrawQuietFor(session.AgentType("something-new")); got < redrawQuietPeriod {
		t.Errorf("an unrecognised agent waits %v, less than the %v default", got, redrawQuietPeriod)
	}
}

// A burst collapses to one keystroke: the timer restarts on every request, so
// the send happens once nothing new has been asked for.
func TestQueuedRedrawsCoalesce(t *testing.T) {
	const sid = "queue-coalesce-test"
	defer cancelQueuedRedraw(sid, 0)

	for i := 0; i < 5; i++ {
		queueRedraw(sid, "no-such-session", 0, "test", redrawQuietPeriod)
	}

	redrawQueue.Lock()
	n := len(redrawQueue.timers)
	redrawQueue.Unlock()
	if n != 1 {
		t.Errorf("%d timers are pending for one tab, want 1 — each would send its own "+
			"keystroke, which is the pairing this avoids", n)
	}
}

// Each tab queues independently: one tab's activity must not swallow another's
// redraw.
func TestQueuedRedrawsArePerTab(t *testing.T) {
	const sid = "queue-per-tab-test"
	defer cancelQueuedRedraw(sid, 0)
	defer cancelQueuedRedraw(sid, 1)

	queueRedraw(sid, "no-such-session", 0, "test", redrawQuietPeriod)
	queueRedraw(sid, "no-such-session", 1, "test", redrawQuietPeriod)

	redrawQueue.Lock()
	n := len(redrawQueue.timers)
	redrawQueue.Unlock()
	if n != 2 {
		t.Errorf("%d timers are pending for two tabs, want 2", n)
	}
}

// A tab that goes away must not have its redraw fire afterwards: the target no
// longer exists, and the program that was to be redrawn is gone.
func TestQueuedRedrawIsCancelledOnDetach(t *testing.T) {
	const sid = "queue-cancel-test"

	queueRedraw(sid, "no-such-session", 0, "test", redrawQuietPeriod)
	cancelQueuedRedraw(sid, 0)

	redrawQueue.Lock()
	_, pending := redrawQueue.timers[sid+":0"]
	redrawQueue.Unlock()
	if pending {
		t.Error("the redraw survived the detach that dropped its connection")
	}
}

// The multiplexer-side repaint stays as well: it sends no input, and it is what
// recovers output dropped while a tab was hidden.
func TestShowPathStillRepaintsViaMultiplexer(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	at := strings.Index(src, "wasHidden := atomic.SwapInt32(&tc.hidden, 0) == 1")
	if at < 0 {
		t.Fatal("un-hide branch not found; if it moved, update this test")
	}
	window := src[at:]
	if end := strings.Index(window, "} else {"); end > 0 {
		window = window[:end]
	}

	if !strings.Contains(window, "RefreshSessionClients") {
		t.Error("showing a tab no longer repaints it; output produced while it was " +
			"hidden is dropped at the source, so the pane would show a stale frame")
	}
}

// RedrawWindow is the automatic counterpart to the Refresh button and must not
// send input of its own — the queue is the only path that sends a keystroke.
func TestRedrawWindowSendsNoInput(t *testing.T) {
	body := funcBody(t, "app.go", "func (a *App) RedrawWindow(")
	if strings.Contains(body, "send-keys") {
		t.Error("RedrawWindow sends keystrokes into the pane; it runs without the user " +
			"asking, so anything it sends lands in whatever the agent is composing")
	}
	if !strings.Contains(body, "RefreshSessionClients") {
		t.Error("RedrawWindow no longer refreshes the session's clients, so it repaints nothing")
	}
}

// funcBody returns the source of a function, from its signature to the closing
// brace at column 0.
func funcBody(t *testing.T, file, signature string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	src := string(b)
	at := strings.Index(src, signature)
	if at < 0 {
		t.Fatalf("%q not found in %s; if it was renamed, update this test", signature, file)
	}
	body := src[at:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	return body
}

// A tab returning from the background is recovered by replaying what it
// produced, not by asking the program to redraw itself.
//
// Output from a hidden tab used to be dropped. tmux then believed the client
// was current and sent only differences against a screen it never received, so
// the pane came back half-repainted with leftovers — recoverable only with a
// Ctrl-L, which is input, and which repeatedly ended up in an agent's composer
// as "/clear". Eight attempts were made to send that keystroke only when safe;
// holding the bytes instead removes the need for it.
func TestReturningTabReplaysHeldOutput(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	at := strings.Index(src, "wasHidden := atomic.SwapInt32(&tc.hidden, 0) == 1")
	if at < 0 {
		t.Fatal("un-hide branch not found; if it moved, update this test")
	}
	window := src[at:]
	if end := strings.Index(window, "} else {"); end > 0 {
		window = window[:end]
	}

	if !strings.Contains(window, "takeHeldWhileHidden") {
		t.Error("a returning tab does not replay what it produced while hidden")
	}
	if strings.Contains(window, `"C-l"`) {
		t.Error("a returning tab still sends a redraw keystroke; the replay makes it " +
			"unnecessary, and the keystroke is what reached agents' composers")
	}
}

// Hidden output must be held, not dropped — that is the whole fix.
func TestHiddenOutputIsHeldNotDropped(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	at := strings.Index(src, "if atomic.LoadInt32(&tc.hidden) == 1 {")
	if at < 0 {
		t.Fatal("the hidden-tab branch was not found")
	}
	branch := src[at:]
	if end := strings.Index(branch, "\n\t\t\t\t}"); end > 0 {
		branch = branch[:end]
	}
	if !strings.Contains(branch, "holdWhileHidden") {
		t.Error("a hidden tab's output is discarded rather than held, so a returning " +
			"tab has nothing to replay and needs a repaint it cannot get cleanly")
	}
}

// The hold has to be bounded: an agent in a loop would otherwise grow it
// without limit while its tab sits in the background.
func TestHeldOutputIsBounded(t *testing.T) {
	tc := &termConn{}
	chunk := make([]byte, 1024*1024)

	for i := 0; i < 10; i++ {
		tc.holdWhileHidden(chunk)
	}

	held, overflowed := tc.takeHeldWhileHidden()
	if !overflowed {
		t.Error("ten megabytes were accepted without reporting an overflow")
	}
	if len(held) != 0 {
		t.Errorf("%d bytes were kept after overflowing; a partial prefix replays the "+
			"start of what happened and then jumps, which reads as corruption",
			len(held))
	}
}

// What is held has to come back in the order it was produced, or the replay
// scrambles the screen it is meant to restore.
func TestHeldOutputKeepsOrder(t *testing.T) {
	tc := &termConn{}
	tc.holdWhileHidden([]byte("first "))
	tc.holdWhileHidden([]byte("second"))

	held, overflowed := tc.takeHeldWhileHidden()
	if overflowed {
		t.Fatal("overflowed on a few bytes")
	}
	if string(held) != "first second" {
		t.Errorf("held %q, want %q", held, "first second")
	}

	// Taking clears it: replaying the same bytes on the next switch would
	// duplicate them.
	again, _ := tc.takeHeldWhileHidden()
	if len(again) != 0 {
		t.Errorf("%d bytes survived being taken", len(again))
	}
}

// A request arriving just after an immediate redraw has nothing to add.
func TestQueuedRedrawDroppedAfterImmediateOne(t *testing.T) {
	const sid = "settled-test"
	defer cancelQueuedRedraw(sid, 0)

	noteRedrawSent(sid, 0)
	queueRedraw(sid, "no-such-session", 0, "resize", time.Second)

	redrawQueue.Lock()
	_, pending := redrawQueue.timers[sid+":0"]
	redrawQueue.Unlock()
	if pending {
		t.Error("a redraw was queued moments after one was sent; the pane had just " +
			"been redrawn in full, and two close together are what produced \"/clear\"")
	}
}

// A plain terminal session gets no redraw keystroke.
//
// Ctrl-L exists here to make a bottom-aligned TUI lay itself out again for a
// new geometry. A shell has no such frame, so the keystroke does nothing useful
// — it just lands on the command line the user is in the middle of typing.
func TestPlainTerminalGetsNoRedraw(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, "tabAgent != session.AgentTerminal") {
		t.Fatal("nothing excludes a plain terminal from the redraw")
	}

	// Only one sender is left — the resize queue. A returning tab is recovered
	// by replaying its held output, which needs no keystroke at all.
	marker := `queueRedraw(sessionID, attachTarget, winIdx, "resize"`
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("could not find %q; if it moved, update this test", marker)
	}
	start := at - 400
	if start < 0 {
		start = 0
	}
	if !strings.Contains(src[start:at], "redrawWanted") {
		t.Error("the resize redraw is not gated on redrawWanted, so a shell session " +
			"receives a keystroke it has no use for")
	}
}

// The immediate redraw is rate-limited too.
//
// It used to clear the queue and record the send, so a queued redraw would not
// follow it — but nothing stopped a second immediate one. Two tab switches in
// quick succession each sent a keystroke: measured in the log at 838ms and
// 968ms apart, which is the pairing that produces the stray "/clear".
func TestImmediateRedrawIsRateLimited(t *testing.T) {
	const sid = "immediate-limit-test"

	if !mayRedrawNow(sid, 0) {
		t.Fatal("the first redraw was refused; a returning tab would never repaint")
	}
	noteRedrawSent(sid, 0)
	if mayRedrawNow(sid, 0) {
		t.Error("a second immediate redraw was allowed right after the first")
	}
	// Per window: switching to a different tab must still repaint it.
	if !mayRedrawNow(sid, 1) {
		t.Error("a different window was refused")
	}
}

// The interval has to clear the gaps measured between tab switches.
func TestRedrawSettledCoversObservedSwitches(t *testing.T) {
	for _, gap := range []int{838, 968} {
		if redrawSettled <= time.Duration(gap)*time.Millisecond {
			t.Errorf("redrawSettled is %v, which lets through two tab switches "+
				"measured %dms apart", redrawSettled, gap)
		}
	}
}
