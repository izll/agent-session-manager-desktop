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
func (a *App) SetQuickJump(entries []session.QuickJumpEntry) error {
	return a.storeQuickJump(func([]session.QuickJumpEntry) []session.QuickJumpEntry {
		return session.NormaliseQuickJump(entries)
	})
}

// AddQuickJump appends a place to the end of the list.
//
// The end rather than the front: the numbers are what make this useful, and
// inserting at the front would shift every one of them each time something new
// is added.
func (a *App) AddQuickJump(sessionID string, windowIdx int) error {
	if sessionID == "" {
		return fmt.Errorf("no session to add")
	}
	entry := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx}

	return a.storeQuickJump(func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		for _, existing := range current {
			if existing.SameTarget(entry) {
				return current
			}
		}
		return append(current, entry)
	})
}

// RemoveQuickJump drops one place from the list.
func (a *App) RemoveQuickJump(sessionID string, windowIdx int) error {
	entry := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx}

	return a.storeQuickJump(func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		kept := make([]session.QuickJumpEntry, 0, len(current))
		for _, existing := range current {
			if !existing.SameTarget(entry) {
				kept = append(kept, existing)
			}
		}
		return kept
	})
}

// SetQuickJumpNote records the user's own words about an entry.
//
// Kept separate from the entry itself: the note is edited in place, one entry
// at a time, and rewriting the whole list to change a few characters would
// race with anything else editing it at that moment.
func (a *App) SetQuickJumpNote(sessionID string, windowIdx int, note string) error {
	target := session.QuickJumpEntry{SessionID: sessionID, WindowIdx: windowIdx}

	return a.storeQuickJump(func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		for i := range current {
			if current[i].SameTarget(target) {
				current[i].Note = note
				break
			}
		}
		return current
	})
}

// MoveQuickJump changes an entry's position, which is what assigns the numbers.
func (a *App) MoveQuickJump(from, to int) error {
	return a.storeQuickJump(func(current []session.QuickJumpEntry) []session.QuickJumpEntry {
		return session.MoveQuickJumpEntry(current, from, to)
	})
}

// storeQuickJump applies a change under the storage lock.
//
// Read-modify-write rather than a plain save, so two edits in quick succession
// — which is how this list is used — cannot lose one another.
func (a *App) storeQuickJump(change func([]session.QuickJumpEntry) []session.QuickJumpEntry) error {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	if !a.projectLocked {
		return fmt.Errorf("project is read-only in this application instance")
	}

	return a.storage.UpdateSettings(func(current *session.Settings) {
		current.QuickJump = session.NormaliseQuickJump(
			change(session.NormaliseQuickJump(current.QuickJump)))
	})
}
