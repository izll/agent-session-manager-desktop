//go:build !windows

package main

import (
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// From the app's side: a tab shown at the bottom of copy mode is live again.
func TestShowingATabEndsCopyModeAtTheBottom(t *testing.T) {
	app, tmux, inst := backgroundTestApp(t)
	target := inst.TmuxSessionName() + ":0"
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.GetTerminalScroll("cmds", 0); got != (session.PaneScroll{InMode: true}) {
		t.Fatalf("before: %+v", got)
	}
	if got, err := app.SettleTerminalScroll("cmds", 0); err != nil || got != (session.PaneScroll{}) {
		t.Fatalf("settled: %+v, %v", got, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		out, _ := tmux("display-message", "-p", "-t", target, "#{pane_in_mode}")
		if strings.TrimSpace(string(out)) == "0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still in copy mode")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := tmux("copy-mode", "-t", target); err != nil {
		t.Fatal(err)
	}
	if got, err := app.LeaveTerminalScroll("cmds", 0); err != nil || got != (session.PaneScroll{}) {
		t.Fatalf("left: %+v, %v", got, err)
	}
}
