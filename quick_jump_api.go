package main

import (
	"fmt"

	"asmgr-desktop/session"
)

// The quick-jump list's own API, rather than a field on the settings save.
//
// Adding, removing and reordering happen while the jump window is open, one
// keystroke at a time. Routing that through SaveSettings would rewrite every
// setting on each keypress — a much wider write than the change deserves, and
// one that races with anything else editing settings at that moment.

// GetQuickJump returns the list in its stored order.
func (a *App) GetQuickJump() ([]session.QuickJumpEntry, error) {
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return nil, err
	}
	if settings == nil {
		// An empty list, never nil: nil crosses the Wails bridge as JSON null,
		// and .length on that throws in the middle of a render.
		return []session.QuickJumpEntry{}, nil
	}
	entries := session.NormaliseQuickJump(settings.QuickJump)
	if entries == nil {
		entries = []session.QuickJumpEntry{}
	}
	return entries, nil
}

// SetQuickJump replaces the list, normalising it first.
func (a *App) SetQuickJump(entries []session.QuickJumpEntry, expectedProjectID string) error {
	return a.storeQuickJump(expectedProjectID, func([]session.QuickJumpEntry) []session.QuickJumpEntry {
		return session.NormaliseQuickJump(entries)
	})
}

// AddQuickJump appends a place to the end of the list.
//
// The end rather than the front: the numbers are what make this useful, and
// inserting at the front would shift every one of them each time something new
// is added.
func (a *App) AddQuickJump(sessionID string, windowIdx int, label, expectedProjectID string) error {
	if sessionID == "" {
		return fmt.Errorf("no session to add")
	}
	entry := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx, Label: label}

	return a.storeQuickJump(expectedProjectID, func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		for _, existing := range current {
			if existing.SameTarget(entry) {
				return current
			}
		}
		return append(current, entry)
	})
}

// RemoveQuickJump drops one place from the list.
func (a *App) RemoveQuickJump(sessionID string, windowIdx int, expectedProjectID string) error {
	entry := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx}

	return a.storeQuickJump(expectedProjectID, func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		kept := make([]session.QuickJumpEntry, 0, len(current))
		for _, existing := range current {
			if !existing.SameTarget(entry) {
				kept = append(kept, existing)
			}
		}
		return kept
	})
}

// SetQuickJumpLabel renames an entry.
//
// Kept separate from writing the whole list: a rename is one entry at a time,
// and rewriting everything to change a few characters would race with anything
// else editing the list at that moment.
func (a *App) SetQuickJumpLabel(sessionID string, windowIdx int, label, expectedProjectID string) error {
	target := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx}

	return a.storeQuickJump(expectedProjectID, func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		for i := range current {
			if current[i].SameTarget(target) {
				current[i].Label = label
				break
			}
		}
		return current
	})
}

// MoveQuickJump changes an entry's position, which is what assigns the numbers.
func (a *App) MoveQuickJump(from, to int, expectedProjectID string) error {
	return a.storeQuickJump(expectedProjectID, func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		return session.MoveQuickJumpEntry(current, from, to)
	})
}

// storeQuickJump applies a change under the storage lock.
//
// Read-modify-write rather than a plain save, so two edits in quick succession
// — which is how this list is used — cannot lose one another.
func (a *App) storeQuickJump(expectedProjectID string, change func([]session.QuickJumpEntry) []session.QuickJumpEntry) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()

	return a.storage.UpdateSettings(func(current *session.Settings) {
		current.QuickJump = session.NormaliseQuickJump(
			change(session.NormaliseQuickJump(current.QuickJump)))
	})
}
