package session

import "testing"

func TestDiffDirAppliesToTabsInTheSessionDirectory(t *testing.T) {
	inst := &Instance{Path: "/work", DiffDir: "/work/app", FollowedWindows: []FollowedWindow{
		{Index: 1, Name: "same dir"},
		{Index: 2, Name: "session dir named", WorkDir: "/work"},
		{Index: 3, Name: "worktree", WorkDir: "/work/.worktrees/x"},
		{Index: 100, Name: "on a server", ServerID: "srv1"},
	}}
	want := map[int]string{0: "/work/app", 1: "/work/app", 2: "/work/app", 3: "", 100: "", 7: "/work/app"}
	for window, dir := range want {
		if got := inst.DiffDirFor(window); got != dir {
			t.Errorf("window %d: %q, want %q", window, got, dir)
		}
	}
	inst.DiffDir = ""
	if got := inst.DiffDirFor(0); got != "" {
		t.Errorf("no diff folder: %q", got)
	}
}
