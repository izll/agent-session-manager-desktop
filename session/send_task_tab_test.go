package session

import (
	"strings"
	"testing"
)

func sentTo(commands [][]string) []string {
	var targets []string
	for _, args := range commands {
		if len(args) >= 4 && args[0] == "send-keys" && args[1] == "-l" {
			targets = append(targets, args[3])
		}
	}
	return targets
}

// "Send to agent" goes to the tab the task is assigned to — on the machine
// that tab runs on — and not to whichever window happens to be active.
func TestSendingATaskGoesToItsAssignedTab(t *testing.T) {
	local := &recordingExecutor{}
	remote := &recordingExecutor{}
	inst := &Instance{ID: "send-task", Name: "send-task", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{ID: "shell", Index: 2, Agent: AgentTerminal},
			{ID: "codex", Index: 100, Agent: AgentCodex, ServerID: "srv"},
			{ID: "parked", Index: 3, Agent: AgentClaude, Stopped: true},
		}}
	SetExecutor(inst.ID, local)
	SetTabExecutor(inst.ID, "srv", remote)
	t.Cleanup(func() {
		ClearExecutor(inst.ID)
		ClearTabExecutor(inst.ID, "srv")
	})
	session := inst.TmuxSessionName()

	if err := inst.SendTaskToAgent("local work", "shell", -1); err != nil {
		t.Fatal(err)
	}
	if got := sentTo(local.seen()); len(got) != 1 || got[0] != session+":2" {
		t.Fatalf("a task for the local tab was typed into %v", got)
	}

	if err := inst.SendTaskToAgent("server work", "codex", -1); err != nil {
		t.Fatal(err)
	}
	if got := sentTo(remote.seen()); len(got) != 1 || got[0] != session+":100" {
		t.Errorf("a task for the server tab reached the server as %v", got)
	}
	if got := sentTo(local.seen()); len(got) != 1 {
		t.Errorf("a task for the server tab was typed here too: %v", got)
	}

	// A tab that is gone is no assignment: the active window, as before.
	if err := inst.SendTaskToAgent("orphan", "closed-long-ago", -1); err != nil {
		t.Fatal(err)
	}
	if got := sentTo(local.seen()); len(got) != 2 || got[1] != session {
		t.Errorf("a task whose tab is gone went to %v, not the active window", got)
	}

	// A stopped tab is refused rather than silently swapped for another.
	if err := inst.SendTaskToAgent("parked", "parked", -1); err == nil ||
		!strings.Contains(err.Error(), "error.assignedTabStopped") {
		t.Errorf("sending to a stopped tab: %v", err)
	}
	if got := sentTo(local.seen()); len(got) != 2 {
		t.Errorf("text was typed for a stopped tab: %v", got)
	}
}

// A task assigned to no tab goes to the tab on screen. The app shows each tab
// through a view of its own, so the multiplexer's active window is not that
// tab: a task sent while looking at a Codex tab went to the main window's
// Claude.
func TestAnUnassignedTaskGoesToTheTabOnScreen(t *testing.T) {
	local := &recordingExecutor{}
	inst := &Instance{ID: "send-viewed", Name: "send-viewed", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{ID: "codex", Index: 7, Agent: AgentCodex},
			{ID: "shell", Index: 2, Agent: AgentTerminal},
		}}
	SetExecutor(inst.ID, local)
	t.Cleanup(func() { ClearExecutor(inst.ID) })
	session := inst.TmuxSessionName()

	if err := inst.SendTaskToAgent("for codex", "", 7); err != nil {
		t.Fatal(err)
	}
	// A closed tab is no assignment either.
	if err := inst.SendTaskToAgent("orphan", "closed-long-ago", 7); err != nil {
		t.Fatal(err)
	}
	// An assignment still wins over what is on screen.
	if err := inst.SendTaskToAgent("for the shell", "shell", 7); err != nil {
		t.Fatal(err)
	}
	want := []string{session + ":7", session + ":7", session + ":2"}
	if got := sentTo(local.seen()); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("typed into %v, want %v", got, want)
	}
}
