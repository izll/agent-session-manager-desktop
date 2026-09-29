package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// ── The record ──────────────────────────────────────────────────────────────

func TestRunningSnapshotRoundTripsPerProject(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	record := RunningRecord{Tabs: []string{MainTabID, "tab-a"}, Multiplexer: "123 456", SeenAt: time.Unix(1000, 0).UTC()}

	for _, project := range []string{"", "p1"} {
		if err := storage.UpdateRunningSnapshot(project, func(s *RunningSnapshot) bool {
			return s.RecordRunning("s1", record)
		}); err != nil {
			t.Fatalf("project %q: %v", project, err)
		}
	}
	if _, err := os.Stat(filepath.Join(storage.configDir, runningSnapshotFile)); err != nil {
		t.Errorf("the default project's record is not beside its sessions.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage.configDir, "projects", "p1", runningSnapshotFile)); err != nil {
		t.Errorf("a project's record is not in its own directory: %v", err)
	}

	got, err := storage.LoadRunningSnapshot("p1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sessions["s1"], record) {
		t.Errorf("read back %+v, want %+v", got.Sessions["s1"], record)
	}
}

// The first launch has nothing recorded; that must read as "nothing to offer",
// not as an error that keeps the app from starting.
func TestRunningSnapshotMissingOrCorruptIsEmpty(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	got, err := storage.LoadRunningSnapshot("")
	if err != nil || len(got.Sessions) != 0 {
		t.Fatalf("missing file: %+v, %v", got, err)
	}

	path := filepath.Join(storage.configDir, runningSnapshotFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = storage.LoadRunningSnapshot("")
	if err != nil || len(got.Sessions) != 0 {
		t.Fatalf("corrupt file: %+v, %v", got, err)
	}
}

func TestRunningSnapshotRejectsAnEscapingProjectID(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	if err := storage.UpdateRunningSnapshot("../x", func(*RunningSnapshot) bool { return true }); err == nil {
		t.Fatal("a project ID with a path in it was written to")
	}
}

// Nothing left running is nothing to bring back: the file goes, so a later
// launch has no stale entry to consider.
func TestRunningSnapshotEmptiedRemovesTheFile(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	_ = storage.UpdateRunningSnapshot("", func(s *RunningSnapshot) bool {
		return s.RecordRunning("s1", RunningRecord{Tabs: []string{MainTabID}})
	})
	if err := storage.UpdateRunningSnapshot("", func(s *RunningSnapshot) bool { return s.Forget("s1") }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(storage.configDir, runningSnapshotFile)); !os.IsNotExist(err) {
		t.Fatalf("the emptied record is still on disk: %v", err)
	}
}

func TestRecordRunningReportsOnlyRealChanges(t *testing.T) {
	s := RunningSnapshot{Sessions: map[string]RunningRecord{}}
	first := RunningRecord{Tabs: []string{MainTabID}, Multiplexer: "1 2", SeenAt: time.Unix(1, 0)}
	if !s.RecordRunning("s1", first) {
		t.Fatal("a new session was not a change")
	}
	later := first
	later.SeenAt = time.Unix(99, 0)
	if s.RecordRunning("s1", later) {
		t.Error("only the time moved, and that rewrote the file on every poll")
	}
	moreTabs := first
	moreTabs.Tabs = []string{MainTabID, "t"}
	if !s.RecordRunning("s1", moreTabs) {
		t.Error("a tab starting was not recorded")
	}
	if !s.RecordRunning("s1", RunningRecord{}) || len(s.Sessions) != 0 {
		t.Error("a session with nothing running left is still recorded")
	}
}

func TestForgetTabLeavesTheRestOfTheSession(t *testing.T) {
	s := RunningSnapshot{Sessions: map[string]RunningRecord{
		"s1": {Tabs: []string{MainTabID, "a", "b"}},
	}}
	if !s.ForgetTab("s1", "a") {
		t.Fatal("stopping a recorded tab changed nothing")
	}
	if got := s.Sessions["s1"].Tabs; !reflect.DeepEqual(got, []string{MainTabID, "b"}) {
		t.Errorf("tabs after the stop = %v", got)
	}
	if s.ForgetTab("s1", "zzz") || s.ForgetTab("nope", "a") {
		t.Error("forgetting something unrecorded reported a change")
	}
	s.ForgetTab("s1", MainTabID)
	s.ForgetTab("s1", "b")
	if _, ok := s.Sessions["s1"]; ok {
		t.Error("a session with every tab stopped is still offered")
	}
}

// ── Reboot or relaunch ─────────────────────────────────────────────────────

func TestJudgeInterruption(t *testing.T) {
	for _, tc := range []struct {
		name      string
		alive     bool
		reachable bool
		recorded  string
		current   string
		want      Interruption
	}{
		// Closing and reopening the app leaves tmux running.
		{"app relaunched, session alive", true, true, "10 100", "10 100", StillRunning},
		// A reboot: no server at all when the app comes up.
		{"reboot, no server yet", false, true, "10 100", "", Interrupted},
		// A reboot, and the user's terminal already started a new server.
		{"reboot, a new server", false, true, "10 100", "77 900", Interrupted},
		// The same server is up: this one session was ended on its own.
		{"session killed alone", false, true, "10 100", "10 100", EndedOnItsOwn},
		// A multiplexer that cannot identify itself gives no way to tell, and
		// offering is the harmless side.
		{"no identity recorded", false, true, "", "", Interrupted},
		// A server not connected yet is not evidence of anything.
		{"server unreachable", false, false, "10 100", "", Undecided},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := JudgeInterruption(tc.alive, tc.reachable, tc.recorded, tc.current); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseMultiplexerIdentity(t *testing.T) {
	for in, want := range map[string]string{
		"179368 1790667066\n": "179368 1790667066",
		"4242 \n":              "4242", // tmux before 3.2: no start_time
		"":                     "",
		"#{pid} #{start_time}": "", // echoed back unexpanded
		"no server running":    "",
	} {
		if got := parseMultiplexerIdentity([]byte(in)); got != want {
			t.Errorf("parse(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── Which tabs are running ─────────────────────────────────────────────────

func TestLiveTabsFromListing(t *testing.T) {
	followed := []FollowedWindow{
		{ID: "running", Index: 1},
		{ID: "exited", Index: 2},                     // its agent quit: a dead pane
		{ID: "parked", Index: 3, Stopped: true},      // stopped by the user
		{ID: "gone", Index: 4},                       // not in the listing at all
		{ID: "server", Index: 10000, ServerID: "s9"}, // on a server
		{ID: "server-stopped", Index: 10001, ServerID: "s9", Stopped: true},
	}
	listing := "0\t1\t0\n1\t\t0\n2\t\t1\n3\t\t1\n"

	got, ok := liveTabsFromListing([]byte(listing), followed, "")
	if !ok {
		t.Fatal("a readable listing was reported unreadable")
	}
	want := []string{MainTabID, "running", "server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("live tabs = %v, want %v", got, want)
	}
}

func TestLiveTabsFromListingParkedMainWindow(t *testing.T) {
	followed := []FollowedWindow{{ID: "t1", Index: 1}}
	// No @asmgr_main marker: an older session, told apart by elimination.
	got, _ := liveTabsFromListing([]byte("0\t\t1\n1\t\t0\n"), followed, "")
	if !reflect.DeepEqual(got, []string{"t1"}) {
		t.Errorf("live tabs = %v, want only t1 (the main window is parked)", got)
	}
}

func TestLiveTabsFromListingUnreadable(t *testing.T) {
	if _, ok := liveTabsFromListing([]byte("garbage"), nil, ""); ok {
		t.Error("an unreadable listing would record the session as having nothing running")
	}
}
