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
// a resize, a tab switch, and the resize that follows a tab switch. So every
// automatic redraw goes through the queue, which sends one per tab after the
// requests stop.
func TestAutomaticRedrawsGoThroughTheQueue(t *testing.T) {
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

	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, `"send-keys"`) || strings.Contains(line, "//") {
			continue
		}
		at := strings.Index(src, line)
		if at >= queueStart && at <= queueEnd {
			continue // inside queueRedraw
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
