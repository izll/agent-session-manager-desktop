package session

import (
	"testing"
	"time"
)

// The window the tests prune against, as a duration. Most cases exercise the
// default rather than a custom value, since that's what an unconfigured
// install uses.
const trashRetention = defaultTrashRetentionDays * 24 * time.Hour

func entry(id string, age time.Duration, now time.Time) *TrashEntry {
	return &TrashEntry{ID: id, Kind: "session", DeletedAt: now.Add(-age)}
}

func ids(entries []*TrashEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func TestPruneTrashDropsExpired(t *testing.T) {
	now := time.Now().UTC()
	in := []*TrashEntry{
		entry("fresh", time.Hour, now),
		entry("old", trashRetention+time.Hour, now),
		entry("yesterday", 24*time.Hour, now),
	}

	kept, changed := pruneTrash(in, now, defaultTrashRetentionDays)
	if !changed {
		t.Fatal("dropping an expired entry should report a change")
	}
	if got := ids(kept); len(got) != 2 || got[0] != "fresh" || got[1] != "yesterday" {
		t.Fatalf("kept %v, want the two recent entries in order", got)
	}
}

// The boundary matters: an entry exactly at the retention age is expired, one a
// moment inside it is not.
func TestPruneTrashBoundary(t *testing.T) {
	now := time.Now().UTC()

	atLimit := []*TrashEntry{entry("at", trashRetention, now)}
	if kept, _ := pruneTrash(atLimit, now, defaultTrashRetentionDays); len(kept) != 0 {
		t.Errorf("an entry exactly at the retention age should expire, kept %d", len(kept))
	}

	justInside := []*TrashEntry{entry("inside", trashRetention-time.Minute, now)}
	if kept, _ := pruneTrash(justInside, now, defaultTrashRetentionDays); len(kept) != 1 {
		t.Errorf("an entry a minute short of the limit should survive, kept %d", len(kept))
	}
}

// Entries written before DeletedAt existed decode as the zero time. Treating
// that as "very old" would silently bin something the user might still want.
func TestPruneTrashKeepsEntriesWithNoTimestamp(t *testing.T) {
	now := time.Now().UTC()
	in := []*TrashEntry{{ID: "legacy", Kind: "session"}}

	kept, changed := pruneTrash(in, now, defaultTrashRetentionDays)
	if len(kept) != 1 || changed {
		t.Fatalf("an entry with no deletion time must be kept, got %d entries changed=%v", len(kept), changed)
	}
}

func TestPruneTrashEnforcesTheCap(t *testing.T) {
	now := time.Now().UTC()
	in := make([]*TrashEntry, 0, trashRetentionCount+5)
	// Oldest first, all well inside the retention window, so only the cap can
	// remove any of them.
	for i := 0; i < trashRetentionCount+5; i++ {
		age := time.Duration(trashRetentionCount+5-i) * time.Minute
		in = append(in, entry(string(rune('a'+i%26))+time.Duration(i).String(), age, now))
	}

	kept, changed := pruneTrash(in, now, defaultTrashRetentionDays)
	if !changed {
		t.Fatal("trimming to the cap should report a change")
	}
	if len(kept) != trashRetentionCount {
		t.Fatalf("kept %d entries, want the cap of %d", len(kept), trashRetentionCount)
	}
	// The five oldest are the ones that should have gone.
	if kept[0].ID != in[5].ID {
		t.Errorf("kept the wrong entries: first is %q, want %q", kept[0].ID, in[5].ID)
	}
}

func TestPruneTrashLeavesAHealthyTrashAlone(t *testing.T) {
	now := time.Now().UTC()
	in := []*TrashEntry{
		entry("a", time.Hour, now),
		entry("b", 2*time.Hour, now),
	}

	kept, changed := pruneTrash(in, now, defaultTrashRetentionDays)
	if changed {
		t.Error("nothing to prune should report no change")
	}
	if got := ids(kept); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("order should be untouched, got %v", got)
	}
}

func TestPruneTrashHandlesAnEmptyTrash(t *testing.T) {
	kept, changed := pruneTrash(nil, time.Now().UTC(), defaultTrashRetentionDays)
	if len(kept) != 0 || changed {
		t.Errorf("an empty trash should stay empty and unchanged, got %d changed=%v", len(kept), changed)
	}
}

// A custom retention is honoured in place of the default: an entry the default
// would keep expires under a shorter window.
func TestPruneTrashHonoursACustomRetention(t *testing.T) {
	now := time.Now().UTC()
	in := []*TrashEntry{
		entry("recent", 3*24*time.Hour, now),
		entry("beyond7", 8*24*time.Hour, now),
	}

	kept, changed := pruneTrash(in, now, 7)
	if !changed {
		t.Fatal("an entry past a 7 day window should be dropped")
	}
	if got := ids(kept); len(got) != 1 || got[0] != "recent" {
		t.Fatalf("kept %v, want only the entry inside the 7 day window", got)
	}
}

// Zero or fewer days switches expiry off entirely — the "keep everything"
// setting, which reaches pruneTrash as 0. Only the count cap still applies.
func TestPruneTrashWithRetentionOffKeepsEverything(t *testing.T) {
	now := time.Now().UTC()
	in := []*TrashEntry{
		entry("fresh", time.Hour, now),
		entry("ancient", 5*trashRetention, now),
	}

	kept, changed := pruneTrash(in, now, 0)
	if changed {
		t.Error("with expiry off nothing should be removed")
	}
	if got := ids(kept); len(got) != 2 || got[0] != "fresh" || got[1] != "ancient" {
		t.Fatalf("kept %v, want both entries in order", got)
	}
}

// The cap is a ceiling on the file size, not part of expiry, so it still binds
// when the user has asked to keep everything.
func TestPruneTrashAppliesTheCapWithRetentionOff(t *testing.T) {
	now := time.Now().UTC()
	in := make([]*TrashEntry, 0, trashRetentionCount+3)
	for i := 0; i < trashRetentionCount+3; i++ {
		// All far past any retention window, to prove the cap did the work.
		age := trashRetention + time.Duration(trashRetentionCount+3-i)*time.Minute
		in = append(in, entry(time.Duration(i).String(), age, now))
	}

	kept, changed := pruneTrash(in, now, 0)
	if !changed || len(kept) != trashRetentionCount {
		t.Fatalf("kept %d entries changed=%v, want the cap of %d", len(kept), changed, trashRetentionCount)
	}
}

// Unset — the zero value, and so every config written before this setting
// existed — must resolve to the default rather than expiring the trash at once.
func TestTrashRetentionDaysDefaultsWhenUnset(t *testing.T) {
	if got := trashRetentionDays(&Settings{}); got != defaultTrashRetentionDays {
		t.Errorf("unset retention resolved to %d, want the default %d", got, defaultTrashRetentionDays)
	}
	// Missing settings entirely should land on the same default, not panic.
	if got := trashRetentionDays(nil); got != defaultTrashRetentionDays {
		t.Errorf("nil settings resolved to %d, want the default %d", got, defaultTrashRetentionDays)
	}
}

// "Keep everything" is stored as a negative, since zero already means unset.
// It resolves to 0, which pruneTrash reads as "no expiry".
func TestTrashRetentionDaysNegativeMeansKeepEverything(t *testing.T) {
	if got := trashRetentionDays(&Settings{TrashRetentionDays: -1}); got != 0 {
		t.Errorf("a negative retention resolved to %d, want 0 for keep-everything", got)
	}
}

func TestTrashRetentionDaysHonoursAConfiguredValue(t *testing.T) {
	for _, days := range []int{7, 30, 90} {
		if got := trashRetentionDays(&Settings{TrashRetentionDays: days}); got != days {
			t.Errorf("retention of %d resolved to %d, want it unchanged", days, got)
		}
	}
}
