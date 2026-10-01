package session

import "testing"

// What a dragged tab is told before it is dropped, from the records alone: the
// sessions it can join, and whether it can become a session of its own.

func TestTheOnlyTabOfASessionMovesWhereTheSessionMerges(t *testing.T) {
	solo := &Instance{ID: "solo", Status: StatusStopped}
	local := &Instance{ID: "local"}
	onServer := &Instance{ID: "remote", ServerID: "srv1"}

	if got := solo.MoveRefusal(0, local); got != "" {
		t.Errorf("the only tab into a local session: %q, want none", got)
	}
	if got, want := solo.MoveRefusal(0, onServer), solo.MergeRefusal(onServer); got != want || got == "" {
		t.Errorf("the only tab into a session on a server: %q, want the merge's refusal %q", got, want)
	}
	if got := solo.MoveRefusal(0, solo); got != errTabMoveSameSession {
		t.Errorf("into itself: %q", got)
	}

	// With another tab beside it, the own window stays the session.
	withTab := &Instance{ID: "with-tab", Status: StatusStopped,
		FollowedWindows: []FollowedWindow{{ID: "tab-a", Index: 1}}}
	if got := withTab.MoveRefusal(0, local); got != errTabMoveMainTab {
		t.Errorf("the own window beside a tab: %q, want %q", got, errTabMoveMainTab)
	}
}

func TestSplitRefusal(t *testing.T) {
	running := &Instance{ID: "src", Status: StatusRunning, FollowedWindows: []FollowedWindow{
		{ID: "live", Index: 1},
		{ID: "stopped", Index: 2, Stopped: true},
		{ID: "remote", Index: 3, ServerID: "srv1"},
	}}
	liveLocal := ""
	if !RunningTabMoveSupported() {
		liveLocal = errTabMoveRunningLocal
	}
	cases := []struct {
		name  string
		inst  *Instance
		index int
		want  string
	}{
		{"a running tab here", running, 1, liveLocal},
		{"a stopped tab", running, 2, ""},
		{"a tab on a server", running, 3, ""},
		{"the session's own window", running, 0, errTabMoveMainTab},
		{"the only tab of a session", &Instance{ID: "solo", Status: StatusStopped}, 0, errTabSplitOnlyTab},
	}
	for _, c := range cases {
		if got := c.inst.SplitRefusal(c.index); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsOnlyTab(t *testing.T) {
	solo := &Instance{ID: "solo", Status: StatusStopped}
	if !solo.IsOnlyTab(0) {
		t.Error("a stopped session's own window, alone, is not its only tab")
	}
	if solo.IsOnlyTab(4) {
		t.Error("an index that names no window is an only tab")
	}
	withTab := &Instance{ID: "with-tab", Status: StatusStopped, FollowedWindows: []FollowedWindow{{ID: "a", Index: 1}}}
	if withTab.IsOnlyTab(0) || withTab.IsOnlyTab(1) {
		t.Error("a session with two tabs has an only tab")
	}
}
