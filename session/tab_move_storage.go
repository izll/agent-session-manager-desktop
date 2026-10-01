package session

import (
	"fmt"
	"slices"
	"strconv"
)

// TabMoveCommit is the stored half of a move of tabs between sessions, saved
// as one change.
//
// One write rather than an UpdateInstance per session: between two writes the
// file holds the tab in both sessions or in neither, and a crash there — or the
// 1 Hz poll saving what it loaded in between — would keep that.
type TabMoveCommit struct {
	// Updated replace the stored sessions with the same IDs.
	Updated []*Instance
	// Added is a session made out of a tab, placed right after AddedAfter
	// (appended when that is not found). Its name is made unique among the
	// project's sessions, in place.
	Added      *Instance
	AddedAfter string
	// Removed is a session a merge has emptied.
	Removed string
	// QuickJump rewrites the quick-jump list, when the move changes where an
	// entry points.
	QuickJump func([]QuickJumpEntry) []QuickJumpEntry
}

// CommitTabMove saves a move of tabs between sessions.
func (s *Storage) CommitTabMove(change TabMoveCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}

	updated := make(map[string]*Instance, len(change.Updated))
	for _, inst := range change.Updated {
		updated[inst.ID] = inst
	}
	instances := make([]*Instance, 0, len(data.Instances)+1)
	for _, inst := range data.Instances {
		if inst == nil || inst.ID == change.Removed {
			continue
		}
		if replacement, ok := updated[inst.ID]; ok {
			inst = replacement
			delete(updated, inst.ID)
		}
		instances = append(instances, inst)
	}
	for id := range updated {
		return fmt.Errorf("instance not found: %s", id)
	}

	if change.Added != nil {
		change.Added.Name = uniqueSessionName(change.Added.Name, instances)
		at := len(instances)
		for position, inst := range instances {
			if inst.ID == change.AddedAfter {
				at = position + 1
				break
			}
		}
		instances = append(instances[:at], append([]*Instance{change.Added}, instances[at:]...)...)
	}

	if change.QuickJump != nil {
		if data.Settings == nil {
			data.Settings = &Settings{}
		}
		data.Settings.QuickJump = change.QuickJump(data.Settings.QuickJump)
	}

	data.Instances = instances
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.writeStorageDataLocked(data, true)
}

// uniqueSessionName is name, or name with a number after it when another
// session already has it — two sessions of one name cannot be told apart in
// the sidebar, which is why AddInstance refuses them.
func uniqueSessionName(name string, instances []*Instance) string {
	taken := make(map[string]bool, len(instances))
	for _, inst := range instances {
		taken[inst.Name] = true
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		candidate := name + " " + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// UniqueSessionID is a new session ID for a session of this name and agent,
// distinct from every ID in taken. The ID is also the multiplexer session's
// name, so it must not collide with a session that exists.
func UniqueSessionID(name string, agent AgentType, taken map[string]bool) string {
	return generateUniqueID(name, agent, taken)
}

// RetargetQuickJump points the entries that named a moved tab at where it is
// now.
//
// moves are the tabs that left session from for session to. With merged set,
// the whole session went: an entry for the session itself, or for a tab of it
// no move names (its own window, which a stopped session lists as 0), goes to
// the tab its own window became.
func RetargetQuickJump(entries []QuickJumpEntry, from, to string, moves []TabMove, merged bool) []QuickJumpEntry {
	ownWindow := -1
	for _, move := range moves {
		if move.FromTabID == MainTabID {
			ownWindow = move.Index
		}
	}
	out := make([]QuickJumpEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.SessionID == from {
			target, ok := retarget(entry, moves, merged, ownWindow)
			if ok {
				entry.SessionID = to
				entry.WindowIdx = target
			}
		}
		out = append(out, entry)
	}
	return NormaliseQuickJump(out)
}

func retarget(entry QuickJumpEntry, moves []TabMove, merged bool, ownWindow int) (int, bool) {
	if !entry.TargetsSession() {
		for _, move := range moves {
			if move.FromTabID != MainTabID && move.FromIndex == entry.WindowIdx {
				return move.Index, true
			}
		}
	}
	if merged {
		return ownWindow, true
	}
	return 0, false
}

// CarryTabs moves the running marks of tabs that left session from for session
// to, under the IDs they have there. With merged set, from is gone and its
// record goes with it.
//
// The poll would correct the record within a few seconds anyway. Done here so
// that a machine going down in those seconds brings the tabs back in the
// session they are in now, rather than not at all.
func (snapshot *RunningSnapshot) CarryTabs(from, to string, moves []TabMove, merged bool) bool {
	source, ok := snapshot.Sessions[from]
	if !ok {
		return false
	}
	// Edited below; the stored record must stay as it was until replaced.
	source.Tabs = slices.Clone(source.Tabs)
	target, hasTarget := snapshot.Sessions[to]
	if !hasTarget {
		target = RunningRecord{Multiplexer: source.Multiplexer, SeenAt: source.SeenAt}
	}
	changed := false
	for _, move := range moves {
		if !source.Has(move.FromTabID) {
			continue
		}
		source.Tabs = slices.DeleteFunc(source.Tabs, func(id string) bool { return id == move.FromTabID })
		if !target.Has(move.TabID) {
			target.Tabs = append(target.Tabs, move.TabID)
		}
		changed = true
	}
	if merged {
		delete(snapshot.Sessions, from)
		changed = true
	} else if changed {
		snapshot.RecordRunning(from, source)
	}
	if len(target.Tabs) > 0 && changed {
		snapshot.Sessions[to] = target
	}
	return changed
}
