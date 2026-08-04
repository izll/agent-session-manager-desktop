package session

import "testing"

func TestMergeResumeSessionIDsPreservesConcurrentMetadata(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	current := &Instance{
		ID:              "session-1",
		Name:            "renamed while polling",
		Notes:           "new notes",
		ResumeSessionID: "manual-main",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Name: "renamed tab", Agent: AgentGemini},
			{Index: 3, Name: "new concurrent tab", Agent: AgentClaude},
		},
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	staleDetection := &Instance{
		ID:              "session-1",
		Name:            "stale name",
		ResumeSessionID: "detected-main",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Name: "stale tab", Agent: AgentGemini, ResumeSessionID: "detected-tab"},
		},
	}
	if err := storage.MergeResumeSessionIDsForProject("", staleDetection); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	got := instances[0]
	if got.Name != current.Name || got.Notes != current.Notes {
		t.Fatalf("concurrent metadata was overwritten: name=%q notes=%q", got.Name, got.Notes)
	}
	// Updated, not preserved: a detected ID that disagrees with the stored one
	// is the live conversation, and the stored one is where the tab used to be.
	if got.ResumeSessionID != "detected-main" {
		t.Fatalf("main ID = %q, want it updated to detected-main", got.ResumeSessionID)
	}
	if len(got.FollowedWindows) != 2 || got.FollowedWindows[1].Name != "new concurrent tab" {
		t.Fatalf("concurrently created tab was lost: %#v", got.FollowedWindows)
	}
	if got.FollowedWindows[0].ResumeSessionID != "detected-tab" {
		t.Fatalf("detected tab ID = %q, want detected-tab", got.FollowedWindows[0].ResumeSessionID)
	}
}

func TestCaptureCodexResumeIDsForProjectUsesLatestStoredTabs(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	current := &Instance{
		ID:    "session-1",
		Path:  "/work/main",
		Agent: AgentCodex,
		FollowedWindows: []FollowedWindow{
			{Index: 7, Name: "new tab at reused index", Agent: AgentCodex, WorkDir: "/work/new"},
		},
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	detector := func(_ string, windowIdx int, expectedCWD string) string {
		switch {
		case windowIdx == 0 && expectedCWD == "/work/main":
			return "current-main"
		case windowIdx == 7 && expectedCWD == "/work/new":
			return "current-tab"
		default:
			t.Fatalf("detector received stale identity: window=%d cwd=%q", windowIdx, expectedCWD)
			return ""
		}
	}
	changed, err := storage.captureCodexResumeIDsForProject(
		"",
		current.ID,
		detector,
		func(*Instance) (int, bool) { return 0, true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("capture reported no change")
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := instances[0].ResumeSessionID; got != "current-main" {
		t.Fatalf("main ID = %q, want current-main", got)
	}
	if got := instances[0].FollowedWindows[0].ResumeSessionID; got != "current-tab" {
		t.Fatalf("tab ID = %q, want current-tab", got)
	}
}

// The whole point of the merge is that polling detects from a snapshot taken
// before the write, so it must never speak for anything it did not detect.
func TestMergeLeavesUndetectedIDsAlone(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	current := &Instance{
		ID:              "session-1",
		ResumeSessionID: "main-id",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentClaude, ResumeSessionID: "tab-id"},
			{Index: 3, Agent: AgentClaude, ResumeSessionID: "other-tab-id"},
		},
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	// Detection found nothing for the main window and nothing for tab 3 — the
	// panes may have exited, or the agent may simply not report. Empty means
	// "no answer", never "no conversation".
	detection := &Instance{
		ID: "session-1",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentClaude, ResumeSessionID: "tab-id"},
			{Index: 3, Agent: AgentClaude},
		},
	}
	if err := storage.MergeResumeSessionIDsForProject("", detection); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := instances[0]
	if got.ResumeSessionID != "main-id" {
		t.Errorf("main ResumeSessionID = %q; a detection that reported nothing "+
			"must not discard a working id", got.ResumeSessionID)
	}
	if got.FollowedWindows[1].ResumeSessionID != "other-tab-id" {
		t.Errorf("tab 3 ResumeSessionID = %q; nothing was detected for it",
			got.FollowedWindows[1].ResumeSessionID)
	}
}

// Switching conversations inside a tab has to reach storage. It is the last
// gate: the poll detected the change correctly, then this refused to record it
// because a value was already there, so restarting the tab reopened the old
// conversation — the failure this test exists to catch.
func TestMergeRecordsASwitchedConversation(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	current := &Instance{
		ID: "session-1",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentClaude, ResumeSessionID: "the-one-we-opened-with"},
		},
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	detection := &Instance{
		ID: "session-1",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentClaude, ResumeSessionID: "the-one-in-use-now"},
		},
	}
	if err := storage.MergeResumeSessionIDsForProject("", detection); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := instances[0].FollowedWindows[0].ResumeSessionID; got != "the-one-in-use-now" {
		t.Errorf("tab ResumeSessionID = %q, want the-one-in-use-now — a resume "+
			"inside the tab was detected but never stored", got)
	}
}
