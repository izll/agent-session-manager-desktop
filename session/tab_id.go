package session

import (
	"strconv"

	"github.com/google/uuid"
)

// MainTabID names a session's own window — the agent the session was created
// with, which is not one of its FollowedWindows and so has no stored ID.
//
// A fixed word rather than a generated ID: there is exactly one main window
// per session for the whole life of the session, and a word that can never be
// a UUID cannot be mistaken for one of the followed tabs.
const MainTabID = "main"

// newTabID gives a newly created tab its identity.
//
// Window indexes cannot serve: a restart renumbers local tabs, a stray server
// tab is moved into its server's band, and a tab restored from the trash comes
// back under whatever index is free. Anything that has to keep pointing at "the
// Codex tab" — a task assigned to it — needs a name that survives all of that.
func newTabID() string {
	return uuid.NewString()
}

// legacyTabIDNamespace scopes the IDs derived for tabs stored before tabs had
// one. Any fixed UUID would do; it only has to never change.
var legacyTabIDNamespace = uuid.MustParse("6f1c7a52-3d0e-4b8a-9a55-0c2b1d7e4f19")

// backfillTabIDs gives every stored tab that predates tab IDs one.
//
// The ID is DERIVED, not generated: the same stored tab always yields the same
// ID, from its owner and the index it holds on disk. That is what makes it safe
// to do on every load without writing anything back. Every reader — the 1 Hz
// sidebar poll, the merge writes that save what they loaded (RecordTerminalDirs,
// MergeResumeSessionIDs), a second window of the app — computes the same answer,
// so there is no race over whose random UUID wins, and nothing has to be
// written under the read path. The first ordinary save then stores it, and from
// then on the stored value is used and the tab can be renumbered freely.
//
// Until that save happens, the index it is derived from has not moved either:
// every code path that renumbers a tab works on instances that came through
// this load, carries the derived ID along, and saves both together.
func backfillTabIDs(data *StorageData) {
	for _, instance := range data.Instances {
		if instance != nil {
			assignMissingTabIDs(instance.FollowedWindows, instance.ID)
		}
	}
	for _, entry := range data.Trash {
		if entry == nil {
			continue
		}
		if entry.Session != nil {
			assignMissingTabIDs(entry.Session.FollowedWindows, entry.Session.ID)
		}
		// Keyed on the trash entry, not the parent session. The parent may by
		// now hold another legacy tab at the index this one had when it was
		// deleted, and restoring this one must not bring back a second tab
		// under the same ID.
		if entry.Tab != nil && entry.Tab.ID == "" {
			entry.Tab.ID = legacyTabID(entry.ID, entry.Tab.Index, 0)
		}
	}
}

// assignMissingTabIDs fills in derived IDs for one session's tabs.
//
// Old stores can hold two descriptors for one index (see
// selectFollowedWindowForRestart); the occurrence count keeps their IDs apart
// while staying the same from one load to the next.
func assignMissingTabIDs(windows []FollowedWindow, owner string) {
	seen := make(map[int]int)
	for idx := range windows {
		window := &windows[idx]
		occurrence := seen[window.Index]
		seen[window.Index] = occurrence + 1
		if window.ID == "" {
			window.ID = legacyTabID(owner, window.Index, occurrence)
		}
	}
}

func legacyTabID(owner string, index, occurrence int) string {
	key := owner + "\x00" + strconv.Itoa(index) + "\x00" + strconv.Itoa(occurrence)
	return uuid.NewSHA1(legacyTabIDNamespace, []byte(key)).String()
}

// WindowForTabID finds the window a tab ID currently names.
//
// ok is false for an ID this session no longer has a tab for — the tab was
// closed, or the ID was never this session's. Callers treat that as "not
// assigned" rather than as an error: a task outlives the tab it was given to.
func (i *Instance) WindowForTabID(tabID string) (windowIdx int, stopped bool, ok bool) {
	if tabID == "" {
		return 0, false, false
	}
	if tabID == MainTabID {
		return i.GetMainWindowIndex(), i.MainWindowStopped, true
	}
	for _, window := range i.FollowedWindows {
		if window.ID == tabID {
			return window.Index, window.Stopped, true
		}
	}
	return 0, false, false
}
