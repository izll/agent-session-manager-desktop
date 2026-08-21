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
			const backupID = "corrupt-restore.json"
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
