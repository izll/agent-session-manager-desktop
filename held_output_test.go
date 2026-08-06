package main

import (
	"bytes"
	"testing"
)

// A tab you are not looking at keeps its output so it can be replayed byte for
// byte when you come back. Past the limit that is not possible, and what
// happens then decides what you see on return.
//
// Measured, not predicted: a Claude tab hidden for a few minutes overflowed the
// original 4 MiB, and coming back gave a repainted screen with no history
// behind it — the user had to scroll up to find what the agent had done. An
// agent redraws its whole screen many times a second and every redraw carries
// the escape sequences to reposition and repaint it, so the budget goes far
// faster than "a screen is about ten kilobytes" suggests.
func TestOverflowKeepsTheNewestOutput(t *testing.T) {
	tc := &termConn{}

	// Fill to just under the limit, then push past it in small writes — the
	// shape real output has. The marker arrives last and must survive; the
	// filler is what should be pushed off the top.
	tc.holdWhileHidden(bytes.Repeat([]byte("o"), maxHeldWhileHidden-10))
	tc.holdWhileHidden(bytes.Repeat([]byte("x"), 4096))
	tc.holdWhileHidden([]byte("THE-LATEST-OUTPUT"))

	held, overflowed := tc.takeHeldWhileHidden()

	if !overflowed {
		t.Error("overflow was not reported, so no repaint would follow the partial replay")
	}
	if len(held) > maxHeldWhileHidden {
		t.Errorf("held %d bytes, over the %d limit", len(held), maxHeldWhileHidden)
	}
	if !bytes.HasSuffix(held, []byte("THE-LATEST-OUTPUT")) {
		t.Error("the most recent output was dropped; it is the part worth keeping")
	}
	// Dropping everything is what left the user with no scrollback.
	if len(held) == 0 {
		t.Error("everything was discarded on overflow")
	}
}

// Under the limit nothing is lost and no repaint is needed: the bytes are the
// ones the multiplexer already produced, in order, so replaying them puts the
// screen exactly where the pane is.
func TestOutputUnderTheLimitIsKeptWhole(t *testing.T) {
	tc := &termConn{}

	tc.holdWhileHidden([]byte("first "))
	tc.holdWhileHidden([]byte("second"))

	held, overflowed := tc.takeHeldWhileHidden()

	if overflowed {
		t.Error("overflow reported for output well under the limit")
	}
	if string(held) != "first second" {
		t.Errorf("held %q, want the output whole and in order", held)
	}
}

// Taking the held output clears it, so returning to a tab twice does not
// replay the same bytes again.
func TestTakingHeldOutputClearsIt(t *testing.T) {
	tc := &termConn{}
	tc.holdWhileHidden([]byte("once"))

	if held, _ := tc.takeHeldWhileHidden(); string(held) != "once" {
		t.Fatalf("first take returned %q", held)
	}
	held, overflowed := tc.takeHeldWhileHidden()
	if len(held) != 0 || overflowed {
		t.Errorf("second take returned %q (overflowed=%v), want nothing", held, overflowed)
	}
}
