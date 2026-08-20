package main

import (
	"context"
	"testing"
	"time"
)

func TestAttentionWatcherHasSingleCancellableLifecycle(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.startAttentionWatcher(ctx)
	firstCancel := a.attentionCancel
	if firstCancel == nil {
		t.Fatal("attention watcher did not register its cancel function")
	}
	a.startAttentionWatcher(ctx)
	if a.attentionCancel == nil {
		t.Fatal("second start cleared the active watcher")
	}

	stopped := make(chan struct{})
	go func() {
		a.stopAttentionWatcher()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("attention watcher did not stop promptly after cancellation")
	}
	if a.attentionCancel != nil {
		t.Fatal("stopped attention watcher still looks active")
	}

	// Repeated shutdown is a no-op, not a second close of the same channel.
	a.stopAttentionWatcher()
}

func TestAttentionTransitionsSilentlyRebaselineOnProjectSwitch(t *testing.T) {
	var state attentionTransitionState
	if got := state.observe("project-a", map[string]string{"a": "waiting"}); len(got) != 0 {
		t.Fatalf("initial baseline notified: %v", got)
	}
	if got := state.observe("project-a", map[string]string{"a": "busy"}); len(got) != 0 {
		t.Fatalf("non-waiting transition notified: %v", got)
	}
	if got := state.observe("project-a", map[string]string{"a": "waiting"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("waiting transition = %v, want [a]", got)
	}

	// Project B is already waiting when selected. That is baseline state, not a
	// new agent transition, so switching projects must not send a burst.
	if got := state.observe("project-b", map[string]string{"b": "waiting"}); len(got) != 0 {
		t.Fatalf("project switch notified existing waiting sessions: %v", got)
	}
}
