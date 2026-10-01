package session

import (
	"reflect"
	"testing"
)

// Both sessions, a new one and the quick-jump list are written as one change.
func TestCommitTabMoveSavesEverySessionInOneWrite(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	a := &Instance{ID: "a", Name: "a", FollowedWindows: []FollowedWindow{{ID: "t", Index: 1}}}
	b := &Instance{ID: "b", Name: "b"}
	c := &Instance{ID: "c", Name: "c"}
	settings := DefaultSettings()
	settings.QuickJump = []QuickJumpEntry{{SessionID: "a", WindowIdx: 1}}
	if err := storage.SaveAll([]*Instance{a, b, c}, nil, settings); err != nil {
		t.Fatal(err)
	}

	movedA := &Instance{ID: "a", Name: "a"}
	movedB := &Instance{ID: "b", Name: "b", FollowedWindows: []FollowedWindow{{ID: "t", Index: 4}}}
	added := &Instance{ID: "n", Name: "b"}
	err := storage.CommitTabMove(TabMoveCommit{
		Updated:    []*Instance{movedA, movedB},
		Added:      added,
		AddedAfter: "a",
		Removed:    "c",
		QuickJump: func(entries []QuickJumpEntry) []QuickJumpEntry {
			return RetargetQuickJump(entries, "a", "b", []TabMove{{FromIndex: 1, FromTabID: "t", TabID: "t", Index: 4}}, false)
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	instances, _, err := storage.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, inst := range instances {
		ids = append(ids, inst.ID)
	}
	if want := []string{"a", "n", "b"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("sessions = %v, want %v (new one after its source, merged one gone)", ids, want)
	}
	if len(instances[0].FollowedWindows) != 0 || len(instances[2].FollowedWindows) != 1 {
		t.Errorf("the tab is not where it was moved: %+v / %+v", instances[0], instances[2])
	}
	if instances[1].Name != "b 2" || added.Name != "b 2" {
		t.Errorf("the new session is called %q (caller sees %q), want a name of its own", instances[1].Name, added.Name)
	}
	_, _, loaded, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if want := []QuickJumpEntry{{SessionID: "b", WindowIdx: 4}}; !reflect.DeepEqual(loaded.QuickJump, want) {
		t.Errorf("quick jump = %+v, want %+v", loaded.QuickJump, want)
	}
}

func TestCommitTabMoveRefusesASessionThatIsGone(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "a"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.CommitTabMove(TabMoveCommit{Updated: []*Instance{{ID: "zz"}}}); err == nil {
		t.Fatal("a move into a session deleted meanwhile was saved")
	}
}

func TestRetargetQuickJump(t *testing.T) {
	moves := []TabMove{
		{FromIndex: 2, FromTabID: "x", TabID: "x", Index: 5},
		{FromIndex: 0, FromTabID: MainTabID, TabID: "new", Index: 6},
	}
	entries := []QuickJumpEntry{
		{SessionID: "src", WindowIdx: 2, Label: "build"},
		{SessionID: "src", WindowIdx: -1, Label: "whole"},
		{SessionID: "src", WindowIdx: 0},
		{SessionID: "other", WindowIdx: 2},
	}

	moved := RetargetQuickJump(entries, "src", "dst", moves[:1], false)
	if want := []QuickJumpEntry{
		{SessionID: "dst", WindowIdx: 5, Label: "build"},
		{SessionID: "src", WindowIdx: -1, Label: "whole"},
		{SessionID: "src", WindowIdx: 0},
		{SessionID: "other", WindowIdx: 2},
	}; !reflect.DeepEqual(moved, want) {
		t.Errorf("one tab moved:\n got %+v\nwant %+v", moved, want)
	}

	merged := RetargetQuickJump(entries, "src", "dst", moves, true)
	if want := []QuickJumpEntry{
		{SessionID: "dst", WindowIdx: 5, Label: "build"},
		{SessionID: "dst", WindowIdx: 6, Label: "whole"},
		{SessionID: "other", WindowIdx: 2},
	}; !reflect.DeepEqual(merged, want) {
		t.Errorf("session merged (the own-window entry folds into the session's):\n got %+v\nwant %+v", merged, want)
	}
}

func TestCarryTabsMovesTheRunningMarks(t *testing.T) {
	snapshot := RunningSnapshot{Sessions: map[string]RunningRecord{
		"src": {Tabs: []string{MainTabID, "x", "y"}, Multiplexer: "m"},
	}}
	moves := []TabMove{{FromTabID: "x", TabID: "x"}}
	if !snapshot.CarryTabs("src", "dst", moves, false) {
		t.Fatal("no change reported")
	}
	if got := snapshot.Sessions["src"].Tabs; !reflect.DeepEqual(got, []string{MainTabID, "y"}) {
		t.Errorf("source = %v", got)
	}
	if got := snapshot.Sessions["dst"]; !reflect.DeepEqual(got.Tabs, []string{"x"}) || got.Multiplexer != "m" {
		t.Errorf("target = %+v", got)
	}

	split := RunningSnapshot{Sessions: map[string]RunningRecord{"src": {Tabs: []string{"y"}}}}
	split.CarryTabs("src", "new", []TabMove{{FromTabID: "y", TabID: MainTabID}}, false)
	if _, kept := split.Sessions["src"]; kept {
		t.Error("a session with nothing running left is still recorded")
	}
	if got := split.Sessions["new"].Tabs; !reflect.DeepEqual(got, []string{MainTabID}) {
		t.Errorf("the split-off session = %v, want its own window running", got)
	}

	merge := RunningSnapshot{Sessions: map[string]RunningRecord{
		"src": {Tabs: []string{MainTabID}}, "dst": {Tabs: []string{MainTabID}},
	}}
	merge.CarryTabs("src", "dst", []TabMove{{FromTabID: MainTabID, TabID: "was-main"}}, true)
	if _, kept := merge.Sessions["src"]; kept {
		t.Error("the merged session is still recorded")
	}
	if got := merge.Sessions["dst"].Tabs; !reflect.DeepEqual(got, []string{MainTabID, "was-main"}) {
		t.Errorf("target = %v", got)
	}
}
