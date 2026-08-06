package session

import "testing"

// The order is the feature: the first nine entries answer to the number keys,
// so moving an entry is how a number gets assigned.
func TestMovingAnEntryReordersTheList(t *testing.T) {
	list := []QuickJumpEntry{
		{SessionID: "a", WindowIdx: -1},
		{SessionID: "b", WindowIdx: -1},
		{SessionID: "c", WindowIdx: -1},
	}

	cases := []struct {
		name     string
		from, to int
		want     string
	}{
		{"down one", 0, 1, "bac"},
		{"to the end", 0, 2, "bca"},
		{"up to the front", 2, 0, "cab"},
		{"nowhere", 1, 1, "abc"},
		{"out of range leaves it alone", 0, 9, "abc"},
		{"negative leaves it alone", -1, 1, "abc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			moved := MoveQuickJumpEntry(append([]QuickJumpEntry(nil), list...), tc.from, tc.to)

			got := ""
			for _, entry := range moved {
				got += entry.SessionID
			}
			if got != tc.want {
				t.Errorf("move(%d→%d) = %q, want %q", tc.from, tc.to, got, tc.want)
			}
			if len(moved) != len(list) {
				t.Errorf("list length changed from %d to %d", len(list), len(moved))
			}
		})
	}
}

// A session and one of its tabs are different destinations, and two tabs of the
// same session are too — but "the session" has only one meaning however it is
// spelled.
func TestSameTargetDistinguishesTabsFromSessions(t *testing.T) {
	session := QuickJumpEntry{SessionID: "a", WindowIdx: -1}
	otherSpelling := QuickJumpEntry{SessionID: "a", WindowIdx: -5}
	tab := QuickJumpEntry{SessionID: "a", WindowIdx: 2}
	otherTab := QuickJumpEntry{SessionID: "a", WindowIdx: 3}
	elsewhere := QuickJumpEntry{SessionID: "b", WindowIdx: 2}

	if !session.SameTarget(otherSpelling) {
		t.Error("two spellings of the same session were treated as different places")
	}
	if session.SameTarget(tab) {
		t.Error("a session and one of its tabs were treated as the same place")
	}
	if tab.SameTarget(otherTab) {
		t.Error("two different tabs were treated as the same place")
	}
	if tab.SameTarget(elsewhere) {
		t.Error("tabs in different sessions were treated as the same place")
	}
}

// The list is hand-edited and round-trips through JSON, so it is cleaned on the
// way in rather than trusted.
func TestNormaliseCleansTheList(t *testing.T) {
	cleaned := NormaliseQuickJump([]QuickJumpEntry{
		{SessionID: "  a  ", WindowIdx: -1, Note: "  Build  "},
		{SessionID: "", WindowIdx: 3},   // no session: nothing to jump to
		{SessionID: "a", WindowIdx: -7}, // the same session again
		{SessionID: "a", WindowIdx: 2},  // a tab of it: a different place
		{SessionID: "b", WindowIdx: 2},
	})

	if len(cleaned) != 3 {
		t.Fatalf("kept %d entries, want 3: %+v", len(cleaned), cleaned)
	}
	if cleaned[0].SessionID != "a" || cleaned[0].Note != "Build" {
		t.Errorf("surrounding whitespace survived: %+v", cleaned[0])
	}
	if cleaned[0].WindowIdx != -1 {
		t.Errorf("WindowIdx = %d, want -1 as the one spelling for a session",
			cleaned[0].WindowIdx)
	}
	if !cleaned[1].SameTarget(QuickJumpEntry{SessionID: "a", WindowIdx: 2}) {
		t.Errorf("the tab entry was lost: %+v", cleaned)
	}
}

// An entry whose session has stopped is kept, deliberately: a stopped session
// is not a deleted one, and dropping it would renumber everything below and
// break the muscle memory the numbers exist to build.
func TestNormaliseKeepsEntriesItCannotVerify(t *testing.T) {
	cleaned := NormaliseQuickJump([]QuickJumpEntry{
		{SessionID: "long-gone", WindowIdx: -1},
		{SessionID: "still-here", WindowIdx: 4},
	})

	if len(cleaned) != 2 {
		t.Errorf("kept %d entries, want both: normalising must not decide what exists", len(cleaned))
	}
}

// The note is the user's own words about an entry, shown after its name. It
// adds to the name rather than replacing it: a tab is called "claude" or
// "shell" in a dozen places at once, and what tells them apart is why they are
// on this list — but a note that replaced the name would leave an entry
// unrecognisable if the note were vague.
func TestNoteIsKeptAlongsideTheTarget(t *testing.T) {
	cleaned := NormaliseQuickJump([]QuickJumpEntry{
		{SessionID: "a", WindowIdx: 2, Note: "the migration one"},
	})

	if len(cleaned) != 1 {
		t.Fatalf("kept %d entries, want 1", len(cleaned))
	}
	if cleaned[0].Note != "the migration one" {
		t.Errorf("Note = %q, want it preserved", cleaned[0].Note)
	}
	// The note must not become part of what identifies the entry, or editing it
	// would make the entry a different place and add a duplicate.
	if !cleaned[0].SameTarget(QuickJumpEntry{SessionID: "a", WindowIdx: 2}) {
		t.Error("the note changed which place the entry points at")
	}
}
