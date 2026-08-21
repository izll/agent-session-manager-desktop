package session

import (
	"errors"
	"path/filepath"
	"testing"
)

func newDefaultImportStorage(t *testing.T) (*Storage, string, *Settings) {
	t.Helper()
	dir := t.TempDir()
	storage := &Storage{configDir: dir, configPath: filepath.Join(dir, "sessions.json")}
	settings := DefaultSettings()
	settings.Language = "hu"
	settings.AnthropicAPIKey = "preserve-secret"
	instance := &Instance{
		ID: "default-session", Name: "Default", Path: "/tmp/default",
		Status: StatusStopped, Agent: AgentClaude, GroupID: "default-group",
	}
	group := &Group{ID: "default-group", Name: "Work"}
	if err := storage.SaveAll([]*Instance{instance}, []*Group{group}, settings); err != nil {
		t.Fatal(err)
	}
	targetID := "proj_target"
	if err := storage.setActiveProjectLocked(targetID); err != nil {
		t.Fatal(err)
	}
	if err := storage.saveAllLocked(nil, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.setActiveProjectLocked(""); err != nil {
		t.Fatal(err)
	}
	return storage, targetID, settings
}

func TestImportDefaultSessionsRollsBackTargetWhenDefaultClearFails(t *testing.T) {
	storage, targetID, expectedSettings := newDefaultImportStorage(t)
	calls := 0
	injected := errors.New("injected default clear failure")
	_, err := storage.importDefaultSessions(targetID, func(instances []*Instance, groups []*Group, settings *Settings) error {
		calls++
		if calls == 2 {
			return injected
		}
		return storage.saveAllLocked(instances, groups, settings)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want injected clear failure", err)
	}
	if storage.GetActiveProjectID() != "" {
		t.Fatalf("active project was not restored: %q", storage.GetActiveProjectID())
	}

	if err := storage.setActiveProjectLocked(targetID); err != nil {
		t.Fatal(err)
	}
	targetInstances, targetGroups, _, err := storage.loadAllWithSettingsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(targetInstances) != 0 || len(targetGroups) != 0 {
		t.Fatalf("target was not rolled back: instances=%+v groups=%+v", targetInstances, targetGroups)
	}
	if err := storage.setActiveProjectLocked(""); err != nil {
		t.Fatal(err)
	}
	defaultInstances, _, defaultSettings, err := storage.loadAllWithSettingsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultInstances) != 1 {
		t.Fatalf("default sessions changed after failed move: %+v", defaultInstances)
	}
	if defaultSettings.Language != expectedSettings.Language || defaultSettings.AnthropicAPIKey != expectedSettings.AnthropicAPIKey {
		t.Fatalf("default settings were lost: %+v", defaultSettings)
	}
}

func TestImportDefaultSessionsRetryIsIdempotentAndPreservesSettings(t *testing.T) {
	storage, targetID, expectedSettings := newDefaultImportStorage(t)
	calls := 0
	_, err := storage.importDefaultSessions(targetID, func(instances []*Instance, groups []*Group, settings *Settings) error {
		calls++
		if calls == 2 || calls == 3 {
			return errors.New("injected clear and rollback failure")
		}
		return storage.saveAllLocked(instances, groups, settings)
	})
	if err == nil {
		t.Fatal("expected partial first attempt")
	}

	count, err := storage.ImportDefaultSessions(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if err := storage.setActiveProjectLocked(targetID); err != nil {
		t.Fatal(err)
	}
	targetInstances, targetGroups, _, err := storage.loadAllWithSettingsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(targetInstances) != 1 || len(targetGroups) != 1 {
		t.Fatalf("retry duplicated target data: instances=%+v groups=%+v", targetInstances, targetGroups)
	}
	if err := storage.setActiveProjectLocked(""); err != nil {
		t.Fatal(err)
	}
	defaultInstances, defaultGroups, defaultSettings, err := storage.loadAllWithSettingsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultInstances) != 0 || len(defaultGroups) != 0 {
		t.Fatalf("default sessions were not cleared: %+v %+v", defaultInstances, defaultGroups)
	}
	if defaultSettings.Language != expectedSettings.Language || defaultSettings.AnthropicAPIKey != expectedSettings.AnthropicAPIKey {
		t.Fatalf("default settings were reset: %+v", defaultSettings)
	}
}
