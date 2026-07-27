package session

import "testing"

// newTestStorage points NewStorage at a throwaway HOME so the test never
// touches the developer's real session store.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	s, err := NewStorage()
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	return s
}

func groupNames(t *testing.T, s *Storage) []string {
	t.Helper()

	groups, err := s.GetGroups()
	if err != nil {
		t.Fatalf("get groups: %v", err)
	}

	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	return names
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMoveGroup(t *testing.T) {
	s := newTestStorage(t)

	ids := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d"} {
		g, err := s.AddGroup(name)
		if err != nil {
			t.Fatalf("add group %s: %v", name, err)
		}
		ids[name] = g.ID
	}

	if got := groupNames(t, s); !equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("groups should start in creation order, got %v", got)
	}

	// Move down: "a" to index 2 lands after "c", because removing "a" first
	// shifts the rest left. This is the behaviour drag-and-drop relies on.
	if err := s.MoveGroup(ids["a"], 2); err != nil {
		t.Fatalf("move a down: %v", err)
	}
	if got := groupNames(t, s); !equal(got, []string{"b", "c", "a", "d"}) {
		t.Fatalf("after moving a to 2: %v", got)
	}

	// Move up.
	if err := s.MoveGroup(ids["d"], 0); err != nil {
		t.Fatalf("move d to front: %v", err)
	}
	if got := groupNames(t, s); !equal(got, []string{"d", "b", "c", "a"}) {
		t.Fatalf("after moving d to 0: %v", got)
	}

	// Out-of-range indices clamp rather than error, so the menu can pass
	// index-1 / index+1 at the ends of the list without special-casing.
	if err := s.MoveGroup(ids["d"], -5); err != nil {
		t.Fatalf("clamp low: %v", err)
	}
	if got := groupNames(t, s); !equal(got, []string{"d", "b", "c", "a"}) {
		t.Fatalf("clamping below zero should keep d first: %v", got)
	}

	if err := s.MoveGroup(ids["d"], 99); err != nil {
		t.Fatalf("clamp high: %v", err)
	}
	if got := groupNames(t, s); !equal(got, []string{"b", "c", "a", "d"}) {
		t.Fatalf("clamping past the end should put d last: %v", got)
	}

	if err := s.MoveGroup("grp_missing", 0); err == nil {
		t.Fatal("moving an unknown group should fail")
	}
}

// The order has to survive a reload, since it lives in the JSON array rather
// than in a numeric field.
func TestMoveGroupPersists(t *testing.T) {
	s := newTestStorage(t)

	var second string
	for _, name := range []string{"first", "second"} {
		g, err := s.AddGroup(name)
		if err != nil {
			t.Fatalf("add group %s: %v", name, err)
		}
		if name == "second" {
			second = g.ID
		}
	}

	if err := s.MoveGroup(second, 0); err != nil {
		t.Fatalf("move: %v", err)
	}

	reloaded, err := NewStorage()
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	if got := groupNames(t, reloaded); !equal(got, []string{"second", "first"}) {
		t.Fatalf("order should survive a reload, got %v", got)
	}
}
