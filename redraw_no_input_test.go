package main

import (
	"os"
	"strings"
	"testing"
)

// Automatic repaints must never send input to the pane.
//
// send-keys is input: it goes to whatever is reading the pane, and not every
// reader treats Ctrl-L as "clear the screen". An agent prompt takes it as text
// — Claude Code turned it into a stray "/clear" in the composer, which fired on
// every window resize and every tab switch because a repaint had been wired to
// run automatically.
//
// RefreshWindow may send it: that one is behind the Refresh button, where the
// user asked for it. RedrawWindow is the automatic path and must not.
func TestRedrawWindowSendsNoInput(t *testing.T) {
	body := funcBody(t, "app.go", "func (a *App) RedrawWindow(")
	if strings.Contains(body, "send-keys") {
		t.Error("RedrawWindow sends keystrokes into the pane; it runs without the user " +
			"asking, so anything it sends lands in whatever the agent is composing")
	}
	// It still has to do the part that is safe, or it is not a repaint at all.
	if !strings.Contains(body, "RefreshSessionClients") {
		t.Error("RedrawWindow no longer refreshes the session's clients, so it repaints nothing")
	}
}

// The resize path in the terminal server runs on every window resize, with no
// user involvement — same rule.
func TestResizePathSendsNoInput(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	at := strings.Index(src, `"resize-window"`)
	if at < 0 {
		t.Fatal("resize-window call not found; if it moved, update this test")
	}
	// The window around the resize: from the call to the end of that branch.
	window := src[at:]
	if end := strings.Index(window, "} else if"); end > 0 {
		window = window[:end]
	}
	// A Ctrl-L here is intended — it is the only thing that makes a
	// bottom-aligned TUI lay itself out again. What must not happen is two in
	// quick succession, which is what produced the stray "/clear"; so if this
	// path sends one, it has to be behind the rate limiter.
	if strings.Contains(window, `"send-keys"`) && !strings.Contains(window, "allowRedrawKey") {
		t.Error("the resize path sends a redraw keystroke without allowRedrawKey — " +
			`two arriving back to back put a stray "/clear" into Claude Code's composer`)
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

// The guard keys on size, not on time and not on history.
//
// Not time: measured gaps between real resizes ran 206ms to 6.5s, overlapping
// completely with the gaps inside a burst. Not history: suppressing a size the
// program had drawn at before broke restoring a window, which returns to
// exactly such a size and needs the redraw as much as any other. What is left
// is the redundant case — the same size arriving again with nothing in between.
func TestAllowRedrawKeySuppressesOnlyRepeats(t *testing.T) {
	const sid = "redraw-guard-test"

	if !allowRedrawKey(sid, 0, 221, 60) {
		t.Fatal("the first size was refused; nothing would ever redraw")
	}
	if allowRedrawKey(sid, 0, 221, 60) {
		t.Error("the same size was allowed twice in a row — this is the redundant " +
			`pair that produced the stray "/clear"`)
	}
	if !allowRedrawKey(sid, 0, 340, 84) {
		t.Error("maximising to a new size was refused")
	}
	// Restoring goes back to a size drawn at earlier. It is a real resize and
	// must redraw: suppressing it is what left the codex pane offset.
	if !allowRedrawKey(sid, 0, 221, 60) {
		t.Error("restoring to a previous size was refused; the pane comes back " +
			"offset because the program never re-lays-out")
	}
	if !allowRedrawKey(sid, 1, 221, 60) {
		t.Error("a different window was refused; the guard is meant to be per window")
	}
	// After a detach the program behind the pane may be a different one.
	forgetRedrawSizes(sid, 0)
	if !allowRedrawKey(sid, 0, 221, 60) {
		t.Error("the size survived a detach; a fresh pane must redraw")
	}
}
