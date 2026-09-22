package session

import (
	"strings"
	"testing"
)

// The dialog's "only this tab" offer ends up comparing the index it was given
// against the stored tabs. Getting that comparison wrong parks the wrong
// window: either the chosen tab is stopped, or the agent the user did not ask
// for keeps running.
func TestIsMainWindowIndex(t *testing.T) {
	inst := &Instance{
		ID: "s1",
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal},
			{Index: 2, Agent: AgentTerminal},
		},
	}

	if !inst.isMainWindowIndex(0) {
		t.Error("window 0 is not one of the tabs, so it is the session's own window")
	}
	for _, idx := range []int{1, 2} {
		if inst.isMainWindowIndex(idx) {
			t.Errorf("window %d is a stored tab, not the main window", idx)
		}
	}
	// An index nobody owns is not a reason to park a tab.
	if !inst.isMainWindowIndex(7) {
		t.Error("an unknown index must not be taken for a tab")
	}
}

// The answer must not depend on the multiplexer: this is asked while a stopped
// session is being started, when getMainWindowIndex still reports nothing.
func TestIsMainWindowIndexWorksOnAStoppedSession(t *testing.T) {
	inst := &Instance{
		ID:              "s1",
		Status:          StatusStopped,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}},
	}

	if _, ok := inst.getMainWindowIndex(); ok {
		t.Fatal("a stopped session should have no resolvable main window index")
	}
	if inst.isMainWindowIndex(1) {
		t.Error("the stored tab was taken for the main window while stopped")
	}
}

// Everything the user did not choose comes back parked. Without this the
// offer starts every tab, which is the option next to it in the dialog.
//
// restoreFollowedWindows needs a live multiplexer to do the rest of its work,
// so what is checked here is the decision it makes before any of that: which
// tabs it marks stopped. Reimplementing the rule in the test would check
// nothing, so the source of the rule is read instead.
func TestRestoreFollowedWindowsParksEveryTabButTheChosenOne(t *testing.T) {
	body := functionBody(t, readSource(t, "instance.go"),
		"func (i *Instance) restoreFollowedWindows(")

	if !strings.Contains(body, "onlyWindowIdx != allWindows && originalIdx != onlyWindowIdx") {
		t.Error("the tabs the user did not pick are no longer parked, so " +
			"\"only this tab\" starts all of them")
	}
	if !strings.Contains(body, "fw.Stopped = true") {
		t.Error("nothing marks the unpicked tabs stopped")
	}
}

// allWindows has to stay distinct from every real index, including 0 — the
// session's own window. A zero sentinel would park the main agent on every
// ordinary start.
func TestAllWindowsIsNotARealIndex(t *testing.T) {
	if allWindows >= 0 {
		t.Errorf("allWindows = %d overlaps a real window index", allWindows)
	}
}

// The view has to land on the tab the user picked. tmux renumbers the windows
// as they are recreated, so the chosen tab does not keep its index — and the
// main window it would otherwise select is the one about to be parked, which
// reads as "it started everything except the one I asked for".
func TestRestoreFollowedWindowsSelectsTheChosenTab(t *testing.T) {
	body := functionBody(t, readSource(t, "instance.go"),
		"func (i *Instance) restoreFollowedWindows(")

	if !strings.Contains(body, "originalIdx == onlyWindowIdx") {
		t.Error("nothing records where the chosen tab landed, so the view cannot follow it")
	}
	if !strings.Contains(body, "chosenNewIdx = newIdx") {
		t.Error("the chosen tab's new index is not captured; tmux renumbers, so the old one is wrong")
	}
	if !strings.Contains(body, "if chosenFound {") {
		t.Error("the final select-window ignores the chosen tab and lands on the main window")
	}

	// The marking must read the index the tab had on the way in, not one that
	// a previous iteration could have changed.
	if !strings.Contains(body, "originalIdx := fw.Index") {
		t.Error("the tab's incoming index is no longer pinned before it is used")
	}
}

// An ordinary start still ends on the session's own agent: that is the window
// the user is there to look at.
func TestOrdinaryStartStillSelectsTheMainWindow(t *testing.T) {
	body := functionBody(t, readSource(t, "instance.go"),
		"func (i *Instance) restoreFollowedWindows(")

	if !strings.Contains(body, "if mainWindowIdx, ok := i.getMainWindowIndex(); ok {") {
		t.Error("the main-window selection is gone, so a normal start lands on the last tab created")
	}
}
