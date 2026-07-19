package session

import (
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
