package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newRecoveryTestStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	return &Storage{
		configDir:  dir,
		configPath: filepath.Join(dir, "sessions.json"),
	}
}

func TestAutomaticBackupExcludesSecret(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	settings := DefaultSettings()
	settings.AnthropicAPIKey = "must-not-enter-backup"
	if err := storage.SaveAll([]*Instance{}, []*Group{}, settings); err != nil {
		t.Fatal(err)
	}
	backups, err := storage.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(backups))
	}
	raw, err := os.ReadFile(filepath.Join(storage.backupDirLocked(), backups[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-enter-backup") {
		t.Fatal("secret was copied into automatic backup")
	}
}

func TestBackupComparisonRejectsOversizedCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(path, []byte("same plus untrusted tail"), 0o600); err != nil {
		t.Fatal(err)
	}
	if backupFileMatches(path, []byte("same"), 4) {
		t.Fatal("oversized backup candidate was treated as an identical snapshot")
	}
	if !backupFileMatches(path, []byte("same plus untrusted tail"), 64) {
		t.Fatal("bounded identical backup was not recognized")
	}
}

func TestInvalidNamedJSONCannotSuppressUsableBackup(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	data := &StorageData{
		SchemaVersion: recoverySchemaVersion,
		Instances:     []*Instance{},
		Groups:        []*Group{},
		Settings:      &Settings{},
		Trash:         []*TrashEntry{},
	}
	_, raw, err := sanitizedStorageData(data)
	if err != nil {
		t.Fatal(err)
	}
	dir := storage.backupDirLocked()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zzzz.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.createBackupLocked(data, true); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if valid := backupJSONEntries(entries); len(valid) != 1 {
		t.Fatalf("backup success published %d usable recovery points, want one", len(valid))
	}
	if _, err := os.Stat(filepath.Join(dir, "zzzz.json")); err != nil {
		t.Fatalf("unknown user file was removed during backup cleanup: %v", err)
	}
}

func TestListBackupsPublishesOnlyRestorableIDs(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	dir := storage.backupDirLocked()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := "20260822T120000.000000000Z-deadbeef.json"
	for _, name := range []string{valid, "zzzz.json", "20260822T120000.000000000Z-DEADBEEF.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	backups, err := storage.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].ID != valid {
		t.Fatalf("published backups = %+v, want only %q", backups, valid)
	}
	if _, err := os.Stat(filepath.Join(dir, "zzzz.json")); err != nil {
		t.Fatalf("listing removed an unknown user file: %v", err)
	}
}

func TestTrashAndRestoreSessionPreservesMetadata(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	instance := &Instance{
		ID:        "session-1",
		Name:      "API",
		Path:      "/tmp/api",
		Status:    StatusStopped,
		Agent:     AgentCodex,
		Favorite:  true,
		Notes:     "keep this",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashInstance(instance.ID); err != nil {
		t.Fatal(err)
	}
	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("active instances = %d, want 0", len(instances))
	}
	trash, err := storage.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 || trash[0].Kind != "session" {
		t.Fatalf("unexpected trash: %#v", trash)
	}
	result, err := storage.RestoreTrashItem(trash[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != instance.ID || result.WindowIdx != 0 {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	restored, err := storage.GetInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Agent != AgentCodex || !restored.Favorite || restored.Notes != "keep this" || restored.Status != StatusStopped {
		t.Fatalf("metadata was not preserved: %+v", restored)
	}
}

func TestTrashAndRestoreTabUsesSafeStoredIndex(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	instance := &Instance{
		ID:        "session-1",
		Name:      "API",
		Path:      "/tmp/api",
		Status:    StatusStopped,
		Agent:     AgentClaude,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		TabOrder:  []int{7, 0},
		FollowedWindows: []FollowedWindow{{
			Index: 7, Agent: AgentCodex, Name: "Review", Notes: "important",
			WorkDir: "/tmp/review", HideStatusLine: true, TextColor: "#fff",
		}},
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashTab(instance.ID, 7); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 || trash[0].Kind != "tab" {
		t.Fatalf("unexpected trash: %#v", trash)
	}
	result, err := storage.RestoreTrashItem(trash[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := storage.GetInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.FollowedWindows) != 1 {
		t.Fatalf("restored tabs = %d, want 1", len(restored.FollowedWindows))
	}
	tab := restored.FollowedWindows[0]
	if result.WindowIdx != tab.Index || tab.Index == 7 || !tab.Stopped {
		t.Fatalf("unsafe restore index/state: result=%+v tab=%+v", result, tab)
	}
	if tab.Agent != AgentCodex || tab.Name != "Review" || tab.Notes != "important" ||
		tab.WorkDir != "/tmp/review" || !tab.HideStatusLine || tab.TextColor != "#fff" {
		t.Fatalf("tab metadata was not preserved: %+v", tab)
	}
	if len(restored.TabOrder) != 2 || restored.TabOrder[0] != tab.Index || restored.TabOrder[1] != 0 {
		t.Fatalf("tab order was not restored: %v", restored.TabOrder)
	}
}

func TestRestoreTabReportsRollbackWindowFailure(t *testing.T) {
	restoreErr := errors.New("failed to publish restored tab")
	cleanupErr := errors.New("tmux window is still alive")
	err := cleanupFailedRestoreWindow(restoreErr, func() error { return cleanupErr })
	if !errors.Is(err, restoreErr) {
		t.Fatalf("restore failure was lost: %v", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("window cleanup failure was lost: %v", err)
	}

	if err := cleanupFailedRestoreWindow(restoreErr, func() error { return nil }); !errors.Is(err, restoreErr) {
		t.Fatalf("successful cleanup changed the original failure: %v", err)
	}
}

func TestRestoreBackupPreservesCurrentSecret(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	settings := DefaultSettings()
	settings.AnthropicAPIKey = "current-secret"
	first := &Instance{ID: "one", Name: "First", Status: StatusStopped}
	if err := storage.SaveAll([]*Instance{first}, []*Group{}, settings); err != nil {
		t.Fatal(err)
	}
	backups, err := storage.ListBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("initial backups: %v, %v", backups, err)
	}
	firstBackupID := backups[0].ID

	second := &Instance{ID: "two", Name: "Second", Status: StatusStopped}
	if err := storage.SaveAll([]*Instance{second}, []*Group{}, settings); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreBackup(firstBackupID); err != nil {
		t.Fatal(err)
	}
	instances, _, restoredSettings, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ID != "one" {
		t.Fatalf("wrong restored instances: %+v", instances)
	}
	if restoredSettings.AnthropicAPIKey != "current-secret" {
		t.Fatal("restore did not preserve the current secret")
	}
}

func TestStoppedRestoreWindowCommandPrintsDetachedWindowIndex(t *testing.T) {
	cmd := newTmuxWindowCommand("asm_test", "/tmp/work", "Review", true, nil)
	args := strings.Join(cmd.Args[1:], " ")
	for _, required := range []string{
		"new-window", "-d", "-P", "-F #{window_index}",
		"-t asm_test", "-c /tmp/work", "-n Review",
	} {
		if !strings.Contains(args, required) {
			t.Fatalf("command %q does not contain %q", args, required)
		}
	}
	index, err := parseTmuxWindowIndex([]byte("7\n"))
	if err != nil || index != 7 {
		t.Fatalf("parseTmuxWindowIndex = %d, %v; want 7, nil", index, err)
	}
}

func TestUpdateSettingsPreservesBackendOnlyFields(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	settings := DefaultSettings()
	settings.AnthropicAPIKey = "keep-me"
	if err := storage.SaveAll(nil, nil, settings); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpdateSettings(func(current *Settings) {
		current.SplitView = true
		current.MarkedSessionID = "session-a"
		current.MarkedWindowIdx = 4
	}); err != nil {
		t.Fatal(err)
	}
	_, _, restored, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if restored.AnthropicAPIKey != "keep-me" {
		t.Fatal("backend-only secret was overwritten")
	}
	if !restored.SplitView || restored.MarkedSessionID != "session-a" || restored.MarkedWindowIdx != 4 {
		t.Fatalf("frontend settings were not updated: %+v", restored)
	}
}

func TestLoadRejectsNewerStorageSchema(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	raw := []byte(`{"schema_version":999,"instances":[]}`)
	if err := os.WriteFile(storage.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := storage.LoadAllWithSettings(); err == nil {
		t.Fatal("newer storage schema was accepted")
	}
}

// A tab tmux has but the store does not must still close.
//
// The tab bar lists what tmux actually holds, so a window that outlives the
// record of it appears as an ordinary tab — one that refuses to close, forever,
// with no way to tell from the outside why. It was seen on a real session
// carrying two such terminals.
//
// A stopped session is the honest exception: there is no tmux window to kill
// and no record to remove, so "tab not found" is the truth.
func TestTrashTabOnStoppedSessionStillReportsMissingTab(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	instance := &Instance{
		ID:        "session-untracked",
		Name:      "API",
		Path:      "/tmp/api",
		Status:    StatusStopped,
		Agent:     AgentClaude,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{{
			Index: 1, Agent: AgentTerminal, Name: "Terminal",
		}},
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	// Window 4 is in neither the store nor tmux — nothing to close.
	if err := storage.TrashTab(instance.ID, 4); err == nil {
		t.Fatal("closing a tab that exists nowhere should report it missing")
	}

	// And the tab that IS stored still trashes normally.
	if err := storage.TrashTab(instance.ID, 1); err != nil {
		t.Fatalf("trashing a stored tab failed: %v", err)
	}
	trash, err := storage.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 {
		t.Fatalf("trash entries = %d, want 1", len(trash))
	}
}

func TestPersistTrashTabDoesNotDeleteWindowWhenSaveFails(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	storage.configPath = filepath.Join(t.TempDir(), "missing", "sessions.json")
	data := &StorageData{SchemaVersion: recoverySchemaVersion}
	deleted := false

	err := storage.persistTrashThenApply(data, data, func() error {
		deleted = true
		return nil
	})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if deleted {
		t.Fatal("live window was deleted before trash metadata became durable")
	}
}

func TestPersistTrashTabRollsBackMetadataWhenWindowDeleteFails(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	original := &StorageData{
		SchemaVersion: recoverySchemaVersion,
		Revision:      1,
		Instances:     []*Instance{{ID: "session-1", Name: "before"}},
	}
	updated := &StorageData{
		SchemaVersion: recoverySchemaVersion,
		Revision:      2,
		Trash:         []*TrashEntry{{ID: "trash-1", Kind: "tab"}},
	}
	deleteErr := fmt.Errorf("injected tmux failure")

	err := storage.persistTrashThenApply(updated, original, func() error { return deleteErr })
	if !errors.Is(err, deleteErr) {
		t.Fatalf("error = %v, want injected delete failure", err)
	}
	data, err := storage.loadStorageDataLocked()
	if err != nil {
		t.Fatal(err)
	}
	if data.Revision != original.Revision || len(data.Instances) != 1 || data.Instances[0].Name != "before" || len(data.Trash) != 0 {
		t.Fatalf("metadata was not rolled back: %#v", data)
	}
}

func TestRestoreBackupRejectsStructurallyCorruptSnapshotsBeforeWrite(t *testing.T) {
	cases := map[string]string{
		"null instance": `{"schema_version":1,"instances":[null]}`,
		"duplicate instance ID": `{"schema_version":1,"instances":[` +
			`{"id":"same","name":"one"},{"id":"same","name":"two"}]}`,
		"missing group": `{"schema_version":1,"groups":[],"instances":[` +
			`{"id":"one","name":"one","group_id":"missing"}]}`,
		"null trash payload": `{"schema_version":1,"instances":[],"trash":[` +
			`{"id":"trash","kind":"session","session":null}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			storage := newRecoveryTestStorage(t)
			original := &Instance{ID: "original", Name: "Original", Status: StatusStopped}
			if err := storage.SaveAll([]*Instance{original}, nil, DefaultSettings()); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(storage.configPath)
			if err != nil {
				t.Fatal(err)
			}
			backupDir := storage.backupDirLocked()
			if err := os.MkdirAll(backupDir, 0o700); err != nil {
				t.Fatal(err)
			}
			const backupID = "20260822T120000.000000000Z-bad0c0de.json"
			if err := os.WriteFile(filepath.Join(backupDir, backupID), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			entriesBefore, err := os.ReadDir(backupDir)
			if err != nil {
				t.Fatal(err)
			}

			if err := storage.RestoreBackup(backupID); err == nil {
				t.Fatal("structurally corrupt backup was accepted")
			}
			after, err := os.ReadFile(storage.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("corrupt restore changed canonical storage")
			}
			entriesAfter, err := os.ReadDir(backupDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entriesAfter) != len(entriesBefore) {
				t.Fatal("corrupt restore created a safety backup before validation")
			}
		})
	}
}

func TestBackupIDRejectsWindowsDevicesAndNonGeneratedNames(t *testing.T) {
	valid := "20260822T120000.000000000Z-deadbeef.json"
	if !validBackupID(valid) {
		t.Fatalf("generated backup ID %q was rejected", valid)
	}
	for _, id := range []string{
		"COM1.json",
		"CON.json",
		"20260822T120000.000000000Z-not-hex!.json",
		"20260822T120000.000000000Z-DEADBEEF.json",
		"../" + valid,
	} {
		if validBackupID(id) {
			t.Errorf("unsafe/non-generated backup ID %q was accepted", id)
		}
	}
}

// A tab restored from the trash must keep the range its machine owns.
//
// Window indexes are how everything addresses a tab, and they are kept unique
// across the machines a session spans by giving each server a band starting at
// remoteWindowIndexBase (see instance.go). Restoring ignored that: it asked
// for the next index after the highest one held, which for a session whose
// tabs are local is a low, local number.
//
// The restored record then said two contradictory things — a local index and a
// server of its own — and the session's own multiplexer had no such window.
// Clicking the tab looked like nothing happening at all: the attach asked the
// server, correctly, whether it held that window, the server said no, and the
// socket was refused before it ever opened.
func TestRestoringARemoteTabKeepsItInItsServersIndexRange(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	const serverID = "srv-web"
	instance := &Instance{
		ID:        "session-1",
		Name:      "API",
		Path:      "/tmp/api",
		Status:    StatusStopped,
		Agent:     AgentClaude,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{
			{Index: 7, Agent: AgentClaude, Name: "local"},
			{Index: 100, Agent: AgentTerminal, Name: "Terminal", ServerID: serverID},
		},
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashTab(instance.ID, 100); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(trash) != 1 {
		t.Fatalf("unexpected trash: %#v", trash)
	}
	if _, err := storage.RestoreTrashItem(trash[0].ID); err != nil {
		t.Fatal(err)
	}

	restored, err := storage.GetInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	var tab *FollowedWindow
	for at := range restored.FollowedWindows {
		if restored.FollowedWindows[at].ServerID == serverID {
			tab = &restored.FollowedWindows[at]
		}
	}
	if tab == nil {
		t.Fatal("the restored tab lost the server it runs on")
	}
	if tab.Index < remoteWindowIndexBase {
		t.Errorf("the tab runs on %s but was restored at index %d, which is a "+
			"local index: the session's own multiplexer has no such window, so "+
			"clicking the tab refuses the attach and nothing happens",
			serverID, tab.Index)
	}
}

// The other direction: a local tab restored into a session that also has a tab
// on a server must stay in the local range.
//
// Counting the remote tab's index as if it were local put the restored tab at
// remoteWindowIndexBase — the very index the server's tab already held, so two
// records claimed one window.
func TestRestoringALocalTabBesideARemoteOneStaysLocal(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	instance := &Instance{
		ID:        "session-1",
		Name:      "API",
		Path:      "/tmp/api",
		Status:    StatusStopped,
		Agent:     AgentClaude,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{
			{Index: 3, Agent: AgentClaude, Name: "local"},
			{Index: 7, Agent: AgentTerminal, Name: "to restore"},
			{Index: 100, Agent: AgentTerminal, Name: "remote", ServerID: "srv-web"},
		},
	}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashTab(instance.ID, 7); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("unexpected trash: %#v, %v", trash, err)
	}
	result, err := storage.RestoreTrashItem(trash[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.WindowIdx >= remoteWindowIndexBase {
		t.Errorf("a local tab was restored at index %d, inside the servers' range "+
			"— and onto the index the remote tab already holds", result.WindowIdx)
	}
	restored, err := storage.GetInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, tab := range restored.FollowedWindows {
		if seen[tab.Index] {
			t.Errorf("two tabs share index %d", tab.Index)
		}
		seen[tab.Index] = true
	}
}

// A session with a hundred-odd local tabs is a real workload. The servers'
// range used to start at 100, where the local multiplexer — which hands out the
// lowest free number — would arrive after a hundred tabs and give out an index
// a server's tab already held.
func TestManyLocalTabsDoNotReachTheServersRange(t *testing.T) {
	// The order that actually collides: the server's tab is made while the
	// session is small, and the local tabs grow past it afterwards.
	early := &Instance{ID: "young", FollowedWindows: []FollowedWindow{
		{Index: 1, Agent: AgentTerminal}, {Index: 2, Agent: AgentClaude},
	}}
	if idx := early.nextRemoteWindowIndex("srv-a"); idx < 1000 {
		t.Errorf("a server's tab in a small session got index %d; the local "+
			"multiplexer reaches that number once the session has %d tabs open",
			idx, idx)
	}

	instance := &Instance{ID: "busy"}
	for idx := 1; idx <= 500; idx++ {
		instance.FollowedWindows = append(instance.FollowedWindows,
			FollowedWindow{Index: idx, Agent: AgentTerminal})
	}
	first := instance.nextRemoteWindowIndex("srv-a")
	if first <= 500 {
		t.Fatalf("a server's first tab got index %d, among the local tabs", first)
	}

	instance.FollowedWindows = append(instance.FollowedWindows,
		FollowedWindow{Index: first, ServerID: "srv-a"})
	second := instance.nextRemoteWindowIndex("srv-b")
	if second-first < 500 {
		t.Errorf("the second server's band starts at %d, only %d after the "+
			"first's — a busy server would run into it", second, second-first)
	}
}

// A server tab created at the session's own directory is stored with no
// directory of its own. Restored into a running session, that came back as
// "no directory" and the restore refused with tabNeedsWorkDirOnServer, leaving
// the tab in the trash. It means the session's path.
func TestRestoringAServerTabWithNoDirOfItsOwnUsesTheSessionPath(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	local := newScriptedExecutor()
	local.answers["list-windows"] = "0\t1\n"
	server := newScriptedExecutor()
	server.answers["new-window"] = "10000\n"
	server.answers["list-windows"] = "10000\n"

	parent := &Instance{
		ID: "running", Name: "running", Path: "/home/u/proj", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 10000, Agent: AgentTerminal, Name: "on server", ServerID: "srv"},
		},
	}
	if err := storage.SaveAll([]*Instance{parent}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	SetExecutor(parent.ID, local)
	SetTabExecutor(parent.ID, "srv", server)
	t.Cleanup(func() { ClearExecutor(parent.ID); ClearTabExecutor(parent.ID, "srv") })

	if err := storage.TrashTab(parent.ID, 10000); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash: %v %v", trash, err)
	}
	if _, err := storage.RestoreTrashItem(trash[0].ID); err != nil {
		t.Fatalf("the restore failed: %v", err)
	}

	created := server.commandsNamed("new-window")
	if len(created) == 0 {
		t.Fatal("nothing was created on the server")
	}
	if !strings.Contains(strings.Join(created[0], " "), "-c /home/u/proj") {
		t.Errorf("the window was not created at the session's path: %v", created[0])
	}
	restored, err := storage.GetInstance(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.FollowedWindows) != 1 || restored.FollowedWindows[0].WorkDir != "" {
		t.Errorf("the tab should still have no directory of its own: %+v", restored.FollowedWindows)
	}
}

// A stray server tab sits in the local range until a restart renumbers it.
// Restoring a local tab into the stopped session skipped server tabs when
// counting, and could land on the stray tab's number.
func TestARestoredLocalTabAvoidsAStrayServerTabsIndex(t *testing.T) {
	inst := &Instance{ID: "s", FollowedWindows: []FollowedWindow{
		{Index: 1, Agent: AgentTerminal},
		{Index: 2, Agent: AgentTerminal, ServerID: "srv"}, // stray: local-range index
	}}
	if got := nextStoredWindowIndex(inst, ""); got == 2 || got == 1 {
		t.Errorf("the restored local tab took index %d, already held", got)
	}
}
