//go:build !windows

package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A pane with history to scroll back through, in a session of the test's own tmux.
func copyModeInstance(t *testing.T, tmux func(args ...string) ([]byte, error)) *Instance {
	t.Helper()
	inst := &Instance{ID: "copymode", Name: "copymode", Path: t.TempDir(), Status: StatusRunning}
	if _, err := tmux("new-session", "-d", "-x", "80", "-y", "10", "-s", inst.TmuxSessionName(), "seq 1 200; sleep 60"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := tmux("display-message", "-p", "-t", inst.TmuxSessionName()+":0", "#{history_size}")
		if strings.TrimSpace(string(out)) != "0" && len(out) > 0 {
			return inst
		}
		if time.Now().After(deadline) {
			t.Fatal("no history")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func scrollOf(t *testing.T, inst *Instance, window int) PaneScroll {
	t.Helper()
	state, err := inst.PaneScrollContext(context.Background(), window)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// Copy mode freezes what the pane shows; the app has to be able to tell.
func TestPaneScrollTellsCopyMode(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := copyModeInstance(t, tmux)
	target := inst.TmuxSessionName() + ":0"
	if got := scrollOf(t, inst, 0); got != (PaneScroll{}) {
		t.Fatalf("live pane: %+v", got)
	}
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if got := scrollOf(t, inst, 0); got != (PaneScroll{InMode: true}) {
		t.Fatalf("at the bottom: %+v", got)
	}
	if _, err := tmux("send-keys", "-X", "-t", target, "-N", "5", "scroll-up"); err != nil {
		t.Fatal(err)
	}
	if got := scrollOf(t, inst, 0); got != (PaneScroll{InMode: true, Scrolled: true}) {
		t.Fatalf("scrolled up: %+v", got)
	}
}

// A window that is not there must not be answered for by another one in copy mode.
func TestPaneScrollOfAMissingWindowIsNone(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := copyModeInstance(t, tmux)
	if _, err := tmux("copy-mode", "-t", inst.TmuxSessionName()+":0"); err != nil {
		t.Fatal(err)
	}
	if got := scrollOf(t, inst, 7); got != (PaneScroll{}) {
		t.Fatalf("missing window: %+v", got)
	}
}

// Shown again at the bottom of copy mode there is no place to keep: the pane goes
// back to live output. Scrolled up, the place is the user's, and stays.
func TestSettlingCopyModeOnlyLeavesItAtTheBottom(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := copyModeInstance(t, tmux)
	target := inst.TmuxSessionName() + ":0"
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if got, err := inst.SettleCopyModeContext(context.Background(), 0); err != nil || got != (PaneScroll{}) {
		t.Fatalf("at the bottom: %+v, %v", got, err)
	}
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("send-keys", "-X", "-t", target, "-N", "5", "scroll-up"); err != nil {
		t.Fatal(err)
	}
	if got, err := inst.SettleCopyModeContext(context.Background(), 0); err != nil || got != (PaneScroll{InMode: true, Scrolled: true}) {
		t.Fatalf("scrolled up: %+v, %v", got, err)
	}
	// Settling a live pane does nothing, and is no error.
	if _, err := inst.LeaveCopyModeContext(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got, err := inst.SettleCopyModeContext(context.Background(), 0); err != nil || got != (PaneScroll{}) {
		t.Fatalf("live: %+v, %v", got, err)
	}
}

// Leaving is asked for by the user, scrolled up or not.
func TestLeavingCopyModeEndsIt(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := copyModeInstance(t, tmux)
	target := inst.TmuxSessionName() + ":0"
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("send-keys", "-X", "-t", target, "-N", "5", "scroll-up"); err != nil {
		t.Fatal(err)
	}
	if got, err := inst.LeaveCopyModeContext(context.Background(), 0); err != nil || got != (PaneScroll{}) {
		t.Fatalf("after leaving: %+v, %v", got, err)
	}
}
