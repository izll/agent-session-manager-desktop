package session

import "strings"

// The quick-jump list: the handful of places you move between all day.
//
// Separate from the ⭐ favourite mark, which says "this one matters" and shows
// it in the sidebar. This says "I keep going here", which is a different
// question with a different answer — the list is ordered by hand, holds tabs as
// well as whole sessions, and its order is the whole point, because the first
// nine entries answer to the number keys.
//
// Entries are kept when their target disappears. A stopped session is not a
// deleted one, and renumbering the list every time something stops would break
// the muscle memory the numbers exist to build: if "3" is the build tab, it has
// to stay the build tab.

// QuickJumpEntry is one place worth returning to.
type QuickJumpEntry struct {
	// SessionID is always set.
	SessionID string `json:"sessionId"`
	// WindowIdx names a tab within that session. Negative means the session
	// itself — jumping to it selects whichever tab was last open there, which
	// is what "go to that session" means everywhere else in the app.
	WindowIdx int `json:"windowIdx"`
	// Note is the user's own words about this entry, shown after its name:
	// "claude — the migration one". A tab's own name is often "claude" or
	// "shell" in a dozen places at once, and what distinguishes them is why
	// they are on this list, which only the user knows.
	//
	// Added to the name rather than replacing it, so an entry stays
	// recognisable when a session is renamed.
	Note string `json:"note,omitempty"`
}

// TargetsSession reports whether this entry means the session as a whole
// rather than one particular tab.
func (e QuickJumpEntry) TargetsSession() bool { return e.WindowIdx < 0 }

// SameTarget reports whether two entries point at the same place, so the list
// does not collect duplicates.
func (e QuickJumpEntry) SameTarget(other QuickJumpEntry) bool {
	if e.SessionID != other.SessionID {
		return false
	}
	// Any negative index means "the session", so -1 and -2 are the same target.
	if e.TargetsSession() || other.TargetsSession() {
		return e.TargetsSession() && other.TargetsSession()
	}
	return e.WindowIdx == other.WindowIdx
}

// NormaliseQuickJump cleans a list arriving from storage or the frontend.
//
// Applied on the way in rather than trusted: the list is edited by hand and
// round-trips through JSON, so an entry with no session, a duplicate, or a
// stray index is a case to handle rather than a bug to hunt later.
func NormaliseQuickJump(entries []QuickJumpEntry) []QuickJumpEntry {
	cleaned := make([]QuickJumpEntry, 0, len(entries))

	for _, entry := range entries {
		entry.SessionID = strings.TrimSpace(entry.SessionID)
		entry.Note = strings.TrimSpace(entry.Note)
		if entry.SessionID == "" {
			continue
		}
		// One canonical spelling for "the session", so comparisons elsewhere
		// do not have to know that -2 and -1 mean the same thing.
		if entry.WindowIdx < 0 {
			entry.WindowIdx = -1
		}

		duplicate := false
		for _, kept := range cleaned {
			if kept.SameTarget(entry) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			cleaned = append(cleaned, entry)
		}
	}
	return cleaned
}

// MoveQuickJumpEntry moves the entry at `from` to `to`, returning a new slice.
//
// Reordering is how the numbers get assigned, so this is the operation the
// feature is built around rather than a convenience. Out-of-range indices leave
// the list untouched instead of panicking: they arrive from a drag that ended
// somewhere unexpected.
func MoveQuickJumpEntry(entries []QuickJumpEntry, from, to int) []QuickJumpEntry {
	if from < 0 || from >= len(entries) || to < 0 || to >= len(entries) || from == to {
		return entries
	}

	moved := make([]QuickJumpEntry, 0, len(entries))
	moved = append(moved, entries[:from]...)
	moved = append(moved, entries[from+1:]...)

	tail := append([]QuickJumpEntry{entries[from]}, moved[to:]...)
	return append(moved[:to:to], tail...)
}
