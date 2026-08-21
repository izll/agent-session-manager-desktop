package main

import (
	"bytes"
	"sync"
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

// Reslicing to keep the tail leaves the original array alive: the window moves,
// the memory does not, and append grows by doubling — so an overflowed tab
// could sit on twice the limit while reporting the limit in length. Per tab
// that is invisible; across the 55 tabs a real workspace here runs, it is
// gigabytes.
func TestOverflowDoesNotStrandTheOldBuffer(t *testing.T) {
	tc := &termConn{}

	for written := 0; written < maxHeldWhileHidden*2; written += 1 << 20 {
		tc.holdWhileHidden(bytes.Repeat([]byte("x"), 1<<20))
	}

	tc.heldMu.Lock()
	length, capacity := len(tc.held), cap(tc.held)
	tc.heldMu.Unlock()

	if capacity > length {
		t.Errorf("cap=%d for len=%d: the discarded prefix is still allocated",
			capacity, length)
	}
}

func TestOverflowReusesTheBoundedRingBuffer(t *testing.T) {
	tc := &termConn{}
	tc.holdWhileHidden(bytes.Repeat([]byte("a"), maxHeldWhileHidden))
	tc.holdWhileHidden([]byte("first-overflow"))

	tc.heldMu.Lock()
	backing := &tc.held[0]
	tc.heldMu.Unlock()
	marker := []byte("LATEST-RING-MARKER")
	for i := 0; i < 32; i++ {
		tc.holdWhileHidden(bytes.Repeat([]byte("x"), 32<<10))
	}
	tc.holdWhileHidden(marker)

	tc.heldMu.Lock()
	if &tc.held[0] != backing {
		t.Error("overflow allocated a new full-size tail instead of reusing the bounded ring")
	}
	if len(tc.held) != maxHeldWhileHidden || cap(tc.held) != maxHeldWhileHidden {
		t.Errorf("ring len/cap = %d/%d, want %d/%d", len(tc.held), cap(tc.held), maxHeldWhileHidden, maxHeldWhileHidden)
	}
	tc.heldMu.Unlock()

	held, overflowed := tc.takeHeldWhileHidden()
	if !overflowed || !bytes.HasSuffix(held, marker) {
		t.Fatalf("ring replay overflow=%v suffix=%q", overflowed, held[len(held)-len(marker):])
	}
}

// Held output is only worth keeping for a tab that will come back. Once the
// connection is closing nobody will ever ask for those bytes, and holding them
// until the conn is collected keeps up to the full limit alive per dead tab.
func TestDiscardFreesTheBuffer(t *testing.T) {
	tc := &termConn{}
	tc.holdWhileHidden(bytes.Repeat([]byte("x"), 1<<20))

	tc.discardHeldWhileHidden()

	tc.heldMu.Lock()
	held, over := tc.held, tc.heldOver
	tc.heldMu.Unlock()

	if held != nil {
		t.Errorf("%d bytes still held after discard", len(held))
	}
	if over {
		t.Error("the overflow flag survived a discard")
	}
}

// Visibility changes and PTY output are handled by different goroutines. A
// chunk that observes "hidden" just before reveal must be part of that replay;
// if reveal wins first it must be delivered live. Either order is valid, loss
// or a chunk left in held after reveal is not.
func TestRevealCannotStrandConcurrentOutput(t *testing.T) {
	for i := 0; i < 500; i++ {
		tc := &termConn{}
		tc.setHidden()

		start := make(chan struct{})
		var wg sync.WaitGroup
		var deliveredMu sync.Mutex
		var delivered []byte
		deliver := func(data []byte) error {
			deliveredMu.Lock()
			delivered = append(delivered, data...)
			deliveredMu.Unlock()
			return nil
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = tc.deliverOrHold([]byte("chunk"), deliver)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, _ = tc.reveal(deliver)
		}()
		close(start)
		wg.Wait()

		deliveredMu.Lock()
		got := string(delivered)
		deliveredMu.Unlock()
		if got != "chunk" {
			t.Fatalf("iteration %d delivered %q, want the chunk exactly once", i, got)
		}
		if held, _ := tc.takeHeldWhileHidden(); len(held) != 0 {
			t.Fatalf("iteration %d stranded %q after reveal", i, held)
		}
	}
}

func TestHiddenFlushIsHeldInsteadOfDropped(t *testing.T) {
	tc := &termConn{}
	tc.setHidden()
	called := false
	held, err := tc.deliverOrHold([]byte("pending ticker output"), func([]byte) error {
		called = true
		return nil
	})
	if err != nil || !held || called {
		t.Fatalf("deliverOrHold: held=%v called=%v err=%v", held, called, err)
	}

	var replayed []byte
	_, _, err = tc.reveal(func(data []byte) error {
		replayed = append(replayed, data...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(replayed) != "pending ticker output" {
		t.Fatalf("replayed %q; ticker output was lost", replayed)
	}
}
