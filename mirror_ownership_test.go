package main

import (
	"os"
	"strings"
	"testing"
)

// attachTarget starts out holding a session NAME, then gets overwritten with a
// session ID ($700) so the attach resolves exactly. Anything after that point
// comparing it against linkedName — still a name — can never match.
//
// That silently disabled three things at once: the window resize (the pane kept
// the size it was created with while the client asked for another, so every
// line wrapped in the wrong place until the user pressed Refresh), the redraw,
// and the mirror cleanup on detach (leaving orphaned mirror sessions behind).
//
// Nothing failed loudly, which is why this is pinned by a test: the ownership
// question has to be answered BEFORE the id swap and carried in a variable.
func TestMirrorOwnershipRecordedBeforeIDSwap(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	const flag = "attachedToOwnMirror := attachTarget == linkedName"
	flagAt := strings.Index(src, flag)
	if flagAt < 0 {
		t.Fatal("ownership flag not found; it must be captured before the id swap")
	}

	const swap = "attachTarget = id"
	swapAt := strings.Index(src, swap)
	if swapAt < 0 {
		t.Fatal("id swap not found; if it moved, update this test")
	}

	if flagAt > swapAt {
		t.Error("the ownership flag is captured AFTER attachTarget is replaced by a " +
			"session id — by then it compares an id against a name and is always false")
	}

	// After the swap, no comparison may go back to using the name directly.
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "attachTarget == linkedName") {
			continue
		}
		if strings.Contains(line, "attachedToOwnMirror :=") {
			continue // the capture itself
		}
		// Everything before the swap is still comparing name to name.
		if strings.Index(src, line) > swapAt {
			t.Errorf("terminal_ws.go:%d compares attachTarget to linkedName after the "+
				"id swap; use attachedToOwnMirror:\n  %s", i+1, strings.TrimSpace(line))
		}
	}
}

// The resize is what the user notices: without it the multiplexer window keeps
// its creation size and the content wraps at the wrong column.
func TestResizeIsGuardedByOwnershipFlag(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}
	src := string(b)

	at := strings.Index(src, `"resize-window"`)
	if at < 0 {
		t.Fatal("resize-window call not found")
	}
	// Look back a little for the condition that gates it.
	start := at - 400
	if start < 0 {
		start = 0
	}
	if !strings.Contains(src[start:at], "attachedToOwnMirror") {
		t.Error("resize-window is not gated on attachedToOwnMirror — either it can " +
			"resize the shared base session, or (comparing an id to a name) it never runs")
	}
}
