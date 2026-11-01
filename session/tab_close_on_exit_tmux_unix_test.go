//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"strings"
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

// Against a real shell in a real tab, since what happens to the rest of a line after
// Ctrl+C is the shell's own decision. Measured: a plain "cmd; exit" stays open.
func TestABackgroundCommandsTabFollowsHowItEnded(t *testing.T) {
	for name, test := range map[string]struct {
		command string
		ctrlC   bool
		closes  bool
	}{
		"finished":           {command: "true", closes: true},
		"stopped by Ctrl+C":  {command: "sleep 60", ctrlC: true, closes: true},
		"failed":             {command: "exit 3", closes: false},
		"not found":          {command: "no-such-command-here", closes: false},
		"trailing ampersand": {command: "sleep 0 &", closes: true},
		"trailing comment":   {command: "true # a note", closes: true},
		"pipe and quotes":    {command: `printf '%s' "it's" | grep -q it`, closes: true},
	} {
		t.Run(name, func(t *testing.T) {
			tmux := isolatedTmux(t)
			inst := closingTab(t, tmux, "c"+strings.ReplaceAll(name, " ", ""), "bash")
			leaves, err := inst.RunUntilDoneInFreshShell(0, test.command, 2*time.Second)
			if err != nil || !leaves {
				t.Fatalf("leaves = %v, %v", leaves, err)
			}
			if test.ctrlC {
				time.Sleep(300 * time.Millisecond)
				waitUntilExited(t, inst, false, time.Second)
				if _, err := tmux("send-keys", "-t", inst.TmuxSessionName()+":0", "C-c"); err != nil {
					t.Fatal(err)
				}
			}
			if test.closes {
				waitUntilExited(t, inst, true, 3*time.Second)
				return
			}
			time.Sleep(500 * time.Millisecond)
			waitUntilExited(t, inst, false, time.Second)
		})
	}
}

// The command runs where it is meant to: what it writes is the proof.
func TestAWrappedCommandStillRunsInTheTabsDirectory(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := closingTab(t, tmux, "cwd", "bash")
	marker := filepath.Join(t.TempDir(), "ran")
	if _, err := inst.RunUntilDoneInFreshShell(0, "echo \"it's\" > "+marker, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	waitUntilExited(t, inst, true, 3*time.Second)
	data, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(data)) != "it's" {
		t.Fatalf("data = %q, %v", data, err)
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
