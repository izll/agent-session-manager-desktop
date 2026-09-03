package session

import "testing"

// Renaming a tab in a stopped session used to fail with "instance not running",
// which is true of tmux and beside the point: the name lives in the store, and
// a stopped session's tabs are listed from there. The tab bar went on showing
// the old name with that error as the only explanation.

func stoppedInstance() *Instance {
	inst := &Instance{Agent: AgentClaude, Name: "proj", Status: StatusStopped}
	inst.FollowedWindows = []FollowedWindow{
		{Index: 1, Agent: AgentTerminal, Name: "Terminal"},
		{Index: 2, Agent: AgentClaude, Name: "claude tab"},
	}
	return inst
}

func TestRenamingATabInAStoppedSessionIsStored(t *testing.T) {
	inst := stoppedInstance()
	old, err := inst.RenameWindow(2, "nesting")
	if err != nil {
		t.Fatalf("renaming a stopped session's tab failed: %v", err)
	}
	if old != "claude tab" {
		t.Errorf("old name = %q, want %q", old, "claude tab")
	}
	for _, fw := range inst.FollowedWindows {
		if fw.Index == 2 {
			if fw.Name != "nesting" {
				t.Fatalf("the new name was not stored: %q", fw.Name)
			}
			return
		}
	}
	t.Fatal("the tab vanished")
}

// The main tab too. Which window is the main one is a question for tmux, so a
// stopped session cannot be asked — index 0 is what the tab bar shows for it.
func TestRenamingTheMainTabInAStoppedSessionIsStored(t *testing.T) {
	inst := stoppedInstance()
	if _, err := inst.RenameWindow(0, "mentes teszt"); err != nil {
		t.Fatalf("renaming a stopped session's main tab failed: %v", err)
	}
	if inst.MainWindowName != "mentes teszt" {
		t.Fatalf("MainWindowName = %q, want the new name", inst.MainWindowName)
	}
}

// A tab that is not tracked at all must still be refused, stopped or not —
// otherwise a rename of something that does not exist reports success.
func TestRenamingAnUnknownTabInAStoppedSessionFails(t *testing.T) {
	inst := stoppedInstance()
	if _, err := inst.RenameWindow(7, "nowhere"); err == nil {
		t.Fatal("renaming a window that is not a tracked tab should fail")
	}
}
