package dictation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyMigrationRetriesPartialCopyAndPreservesExistingFiles(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "ai-dictate")
	newDir := filepath.Join(home, ".config", "agent-session-manager-desktop", "dictation")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(`{"google_api_key":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "speech_context.json"), []byte(`{"en":["term"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	writes := 0
	injected := errors.New("injected migration write failure")
	err := migrateLegacyDictationConfigWithWriter(home, newDir, func(path string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return injected
		}
		return atomicWriteConfigFile(path, data, mode)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("first migration error = %v, want injected failure", err)
	}

	// Simulate a user modifying the file that made it across before the crash.
	firstName := "settings.json"
	if _, err := os.Stat(filepath.Join(newDir, firstName)); os.IsNotExist(err) {
		firstName = "speech_context.json"
	}
	firstPath := filepath.Join(newDir, firstName)
	if err := os.WriteFile(firstPath, []byte("user-new-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyDictationConfig(home, newDir); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
	got, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-new-value" {
		t.Fatalf("retry overwrote existing destination: %q", got)
	}
	for _, name := range []string{"settings.json", "speech_context.json"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); err != nil {
			t.Fatalf("missing migrated %s after retry: %v", name, err)
		}
	}
}

func TestLegacyMigrationDoesNotFollowSymlinks(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "ai-dictate")
	newDir := filepath.Join(home, ".config", "agent-session-manager-desktop", "dictation")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside-secret")
	if err := os.WriteFile(outside, []byte("do-not-copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(legacy, "settings.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := migrateLegacyDictationConfig(home, newDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(newDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy symlink was copied: %v", err)
	}
}

func TestDictationConfigReadRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxDictationConfigBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readDictationConfigFile(path); err == nil {
		t.Fatal("oversized dictation config was accepted")
	}
}

func TestUsageUpdateDoesNotDeadlockDuringMigrationCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	done := make(chan error, 1)
	go func() {
		app := &AppService{}
		done <- app.AddUsage(1, 0.5)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddUsage: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AddUsage deadlocked re-entering dictation config path migration")
	}
}
