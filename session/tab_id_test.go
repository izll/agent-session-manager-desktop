package session

import (
	"os"
	"strings"
	"testing"
	"time"
)

func tabIDs(windows []FollowedWindow) []string {
	ids := make([]string, len(windows))
	for at, window := range windows {
		ids[at] = window.ID
	}
	return ids
}

// Tabs stored before tabs had an ID are given one on load — the same one on
// every load, by every reader, until an ordinary save writes it down.
//
// A random ID here would be a different ID for the sidebar poll than for the
// merge write running beside it, and a task assigned in between would point at
// a tab that no longer answers to it.
func TestTabsStoredWithoutAnIDGetTheSameOneOnEveryLoad(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	legacy := &Instance{
		ID: "legacy", Name: "legacy", Path: "/tmp/legacy", Status: StatusStopped,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentCodex, Name: "codex"},
			{Index: 2, Agent: AgentTerminal, Name: "shell"},
			// A duplicate descriptor, which old stores can hold.
			{Index: 2, Agent: AgentTerminal, Name: "shell again"},
		},
	}
	if err := storage.SaveAll([]*Instance{legacy}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(storage.configPath)
	if strings.Contains(string(raw), `"id": "`+legacyTabID("legacy", 1, 0)) {
		t.Fatal("setup: the store already has tab IDs")
	}

	first, err := storage.GetInstance("legacy")
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.GetInstance("legacy")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for at, window := range first.FollowedWindows {
		if window.ID == "" {
			t.Fatalf("tab %q was loaded without an ID", window.Name)
		}
		if seen[window.ID] {
			t.Fatalf("two tabs share the ID %s: %v", window.ID, tabIDs(first.FollowedWindows))
		}
		seen[window.ID] = true
		if second.FollowedWindows[at].ID != window.ID {
			t.Fatalf("tab %q got %s on one load and %s on the next",
				window.Name, window.ID, second.FollowedWindows[at].ID)
		}
	}

	// Saved once, the ID is on disk, and a renumbering after that keeps it.
	if err := storage.UpdateInstance(first); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(storage.configPath)
	if !strings.Contains(string(raw), first.FollowedWindows[0].ID) {
		t.Fatal("the derived ID was not written by the next save")
	}
	first.FollowedWindows[0].Index = 9
	if err := storage.UpdateInstance(first); err != nil {
		t.Fatal(err)
	}
	after, err := storage.GetInstance("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if after.FollowedWindows[0].ID != first.FollowedWindows[0].ID {
		t.Errorf("renumbering a saved tab changed its ID: %s became %s",
			first.FollowedWindows[0].ID, after.FollowedWindows[0].ID)
	}
}

// A tab that was in the trash before tabs had IDs is keyed on its trash entry,
// not on the index it had: the session may by now hold another legacy tab at
// that index, and restoring would bring back two tabs under one ID.
func TestALegacyTrashedTabDoesNotShareAnIDWithTheTabNowAtItsIndex(t *testing.T) {
	data := &StorageData{
		Instances: []*Instance{{ID: "s", FollowedWindows: []FollowedWindow{{Index: 3}}}},
		Trash: []*TrashEntry{{
			ID: "trash-1", Kind: "tab", ParentSessionID: "s", Tab: &FollowedWindow{Index: 3},
		}},
	}
	backfillTabIDs(data)
	if live, trashed := data.Instances[0].FollowedWindows[0].ID, data.Trash[0].Tab.ID; live == "" || live == trashed {
		t.Errorf("live tab %q and trashed tab %q", live, trashed)
	}
}

// A stored ID is never replaced by a derived one.
func TestBackfillLeavesStoredIDsAlone(t *testing.T) {
	data := &StorageData{Instances: []*Instance{{ID: "s", FollowedWindows: []FollowedWindow{{ID: "kept", Index: 3}}}}}
	backfillTabIDs(data)
	if got := data.Instances[0].FollowedWindows[0].ID; got != "kept" {
		t.Errorf("stored ID became %q", got)
	}
}

// Every way of making a new tab gives it an ID of its own.
func TestNewTabsGetDistinctIDs(t *testing.T) {
	// On a server, so the fake executor answers: a local command bypasses it.
	server := &recordingExecutor{output: []byte("4\n")}
	inst := &Instance{ID: "new-tabs", Name: "new-tabs", Path: "/srv/app", Status: StatusRunning, ServerID: "srv"}
	SetExecutor(inst.ID, server)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	if _, err := inst.NewWindowWithNameOn("srv", "shell", inst.Path); err != nil {
		t.Fatal(err)
	}
	inst.ToggleWindowFollow(7)
	imported := (&PortableSession{Name: "x", Path: "/tmp/x", Tabs: []PortableTab{{Name: "a"}, {Name: "b"}}}).FromPortable("")

	ids := append(tabIDs(inst.FollowedWindows), tabIDs(imported.FollowedWindows)...)
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || id == MainTabID || seen[id] {
			t.Fatalf("new tabs must each get their own ID, got %v", ids)
		}
		seen[id] = true
	}
}

// A restart recreates every local tab, and the multiplexer numbers them
// afresh. The tab is the same tab, so it keeps its ID.
func TestARestartKeepsTheTabsID(t *testing.T) {
	local := &recordingExecutor{output: []byte("5\n")}
	inst := &Instance{
		ID: "restarted", Name: "restarted", Path: t.TempDir(), Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{ID: "tab-a", Index: 8, Agent: AgentTerminal, Name: "shell"}},
	}
	SetExecutor(inst.ID, local)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	inst.restoreFollowedWindows(allWindows)

	if len(inst.FollowedWindows) != 1 || inst.FollowedWindows[0].Index != 5 {
		t.Fatalf("setup: the tab was not renumbered: %+v", inst.FollowedWindows)
	}
	if inst.FollowedWindows[0].ID != "tab-a" {
		t.Errorf("the restarted tab lost its ID: %+v", inst.FollowedWindows[0])
	}
}

// A stray server tab is moved into its server's band; it keeps its ID.
func TestRenumberingAStrayServerTabKeepsItsID(t *testing.T) {
	server := newScriptedExecutor()
	inst := &Instance{ID: "stray-id", Name: "stray-id", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{ID: "tab-s", Index: 3, Agent: AgentClaude, ServerID: "srv"}}}
	SetTabExecutor(inst.ID, "srv", server)
	t.Cleanup(func() { ClearTabExecutor(inst.ID, "srv") })

	inst.renumberStrayRemoteTabs()

	if inst.FollowedWindows[0].Index == 3 {
		t.Fatal("setup: the tab was not renumbered")
	}
	if inst.FollowedWindows[0].ID != "tab-s" {
		t.Errorf("the renumbered tab lost its ID: %+v", inst.FollowedWindows[0])
	}
}

// A tab restored from the trash comes back under a new index but is the tab
// that was deleted, so a task assigned to it finds it again.
func TestATabRestoredFromTheTrashKeepsItsID(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	instance := &Instance{
		ID: "session-1", Name: "API", Path: "/tmp/api", Status: StatusStopped, Agent: AgentClaude,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{{ID: "codex-tab", Index: 7, Agent: AgentCodex, Name: "Review"}},
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashTab(instance.ID, 7); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("unexpected trash: %v %#v", err, trash)
	}
	if _, err := storage.RestoreTrashItem(trash[0].ID); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.GetInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	tab := restored.FollowedWindows[0]
	if tab.Index == 7 {
		t.Fatal("setup: the tab came back at its old index")
	}
	if tab.ID != "codex-tab" {
		t.Errorf("the restored tab lost its ID: %+v", tab)
	}
}

func TestWindowForTabID(t *testing.T) {
	inst := &Instance{ID: "w", MainWindowStopped: true, FollowedWindows: []FollowedWindow{
		{ID: "a", Index: 3},
		{ID: "b", Index: 5, Stopped: true},
	}}
	if idx, stopped, ok := inst.WindowForTabID(MainTabID); !ok || idx != inst.GetMainWindowIndex() || !stopped {
		t.Errorf("main: %d %v %v", idx, stopped, ok)
	}
	if idx, stopped, ok := inst.WindowForTabID("a"); !ok || idx != 3 || stopped {
		t.Errorf("a: %d %v %v", idx, stopped, ok)
	}
	if _, stopped, ok := inst.WindowForTabID("b"); !ok || !stopped {
		t.Errorf("b: %v %v", stopped, ok)
	}
	for _, missing := range []string{"", "gone"} {
		if _, _, ok := inst.WindowForTabID(missing); ok {
			t.Errorf("%q resolved to a tab", missing)
		}
	}
}
