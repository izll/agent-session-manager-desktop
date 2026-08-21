package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandLibraryRejectsAmbiguousAndOversizedStores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.json")
	storage := &Storage{configDir: dir}
	for name, document := range map[string]string{
		"duplicate command": `{"commands":[{"id":"same","name":"one","command":"true"},{"id":"same","name":"two","command":"true"}]}`,
		"duplicate group":   `{"groups":[{"id":"same","name":"one"},{"id":"same","name":"two"}]}`,
		"missing group":     `{"commands":[{"id":"cmd","name":"one","command":"true","group_id":"missing"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := storage.LoadCommands(); err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("LoadCommands error = %v, want structural refusal", err)
			}
		})
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, commandLibraryMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadCommands(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized command library error = %v, want size refusal", err)
	}
}

func TestTemplateLibraryRejectsAmbiguousAndOversizedStores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")
	storage := &Storage{configDir: dir}
	if err := os.WriteFile(path, []byte(`{"templates":[{"id":"same","name":"one"},{"id":"same","name":"two"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadTemplates(); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("duplicate template error = %v, want structural refusal", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, templateLibraryMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadTemplates(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized template library error = %v, want size refusal", err)
	}
}
