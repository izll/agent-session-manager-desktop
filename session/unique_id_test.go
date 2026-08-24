package session

import (
	"strings"
	"testing"
	"time"
)

// The clock is not a source of unique values. On Windows time.Now().UnixNano()
// has a resolution coarse enough that consecutive calls read the same instant —
// measured at 199997 duplicates in 200000 readings — so an ID minted from it
// alone collides with the one just handed out. That corrupted the config: a
// second group got the ID of the first and validation rejected the file.
func TestNewUniqueIDDoesNotRepeatWithinOneClockTick(t *testing.T) {
	taken := make(map[string]bool)
	const count = 1000
	for i := 0; i < count; i++ {
		id := NewUniqueID("grp", taken)
		if taken[id] {
			t.Fatalf("NewUniqueID returned %q, which was already taken (iteration %d)", id, i)
		}
		if !strings.HasPrefix(id, "grp_") {
			t.Fatalf("NewUniqueID lost its prefix: %q", id)
		}
		taken[id] = true
	}
	if len(taken) != count {
		t.Fatalf("expected %d distinct IDs, got %d", count, len(taken))
	}
}

// The real Windows condition, reproduced on any platform: a clock that never
// advances. Without this the bug is invisible on Linux, whose clock is
// fine-grained enough that consecutive readings always differ — the regression
// would pass on the broken code and only fail in Windows CI.
func TestNewUniqueIDSurvivesAClockThatNeverAdvances(t *testing.T) {
	frozen := time.Now().UnixNano()
	restore := nowUnixNano
	nowUnixNano = func() int64 { return frozen }
	t.Cleanup(func() { nowUnixNano = restore })

	taken := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewUniqueID("tpl", taken)
		if taken[id] {
			t.Fatalf("with a frozen clock, mint %d repeated the ID %q", i, id)
		}
		taken[id] = true
	}
}

// AddGroup is the path that actually broke: it minted without consulting the
// groups it had just loaded.
func TestAddGroupTwiceGivesDistinctIDs(t *testing.T) {
	isolateHome(t)
	storage, err := NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	first, err := storage.AddGroup("alpha")
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.AddGroup("beta")
	if err != nil {
		t.Fatalf("adding a second group back-to-back failed: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("both groups got the ID %q", first.ID)
	}
}
