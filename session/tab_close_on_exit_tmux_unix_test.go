//go:build !windows

package session

import (
	"testing"
	"time"
)

func closingTab(t *testing.T, tmux func(args ...string) ([]byte, error), id, shell string) *Instance {
	t.Helper()
	inst := &Instance{ID: id, Name: id, Path: t.TempDir(), FollowedWindows: []FollowedWindow{{ID: "tab", Index: 0}}}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-x", "100", "-y", "20", "-s", name, shell, "--norc", "--noprofile"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-w", "-t", name+":0", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	return inst
}

func waitUntilExited(t *testing.T, inst *Instance, want bool, wait time.Duration) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for inst.TabProcessExited(0) != want {
		if time.Now().After(deadline) {
			t.Fatalf("exited = %v after %v, want %v", !want, wait, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestATabIsOnlyListedWhenMarkedAndExited(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := closingTab(t, tmux, "marked", "bash")
	inst.FollowedWindows = append(inst.FollowedWindows, FollowedWindow{ID: "gone", Index: 9, CloseOnExit: TabCloseCommand})
	if _, err := tmux("send-keys", "-t", inst.TmuxSessionName()+":0", "exit", "Enter"); err != nil {
		t.Fatal(err)
	}
	waitUntilExited(t, inst, true, 3*time.Second)
	// Exited but not marked; marked but its window is not there, where tmux would
	// answer for another one.
	if got := inst.ExitedTabsToClose(); len(got) != 0 {
		t.Fatalf("listed %+v", got)
	}
	inst.MarkTabCloseOnExit(0, TabCloseLogin, 4)
	got := inst.ExitedTabsToClose()
	if len(got) != 1 || got[0].ID != "tab" || got[0].Reason != TabCloseLogin || got[0].ReturnTo != 4 {
		t.Fatalf("listed %+v", got)
	}
}
