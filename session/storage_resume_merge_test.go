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
	if got.ResumeSessionID != "manual-main" {
		t.Fatalf("manual main ID overwritten: %q", got.ResumeSessionID)
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
