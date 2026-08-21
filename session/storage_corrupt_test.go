package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalStorageRejectsNilAndDuplicateIdentityInsteadOfPanicking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	storage := &Storage{configDir: dir, configPath: path}

	cases := map[string]string{
		"null instance":         `{"schema_version":1,"instances":[null]}`,
		"duplicate instance":    `{"schema_version":1,"instances":[{"id":"same","name":"one"},{"id":"same","name":"two"}]}`,
		"null group":            `{"schema_version":1,"groups":[null]}`,
		"invalid trash payload": `{"schema_version":1,"trash":[{"id":"trash","kind":"session"}]}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := storage.LoadAllWithSettings(); err == nil || !strings.Contains(err.Error(), "validate config") {
				t.Fatalf("LoadAllWithSettings error = %v, want structural validation", err)
			}
		})
	}
}

func TestProjectCatalogRejectsNilAndDuplicateIdentity(t *testing.T) {
	dir := t.TempDir()
	storage := &Storage{configDir: dir}
	path := filepath.Join(dir, "projects.json")
	for name, document := range map[string]string{
		"null":      `{"projects":[null]}`,
		"duplicate": `{"projects":[{"id":"same","name":"one"},{"id":"same","name":"two"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := storage.LoadProjects(); err == nil || !strings.Contains(err.Error(), "validate projects") {
				t.Fatalf("LoadProjects error = %v, want structural validation", err)
			}
		})
	}
}

func TestCanonicalStorageAndProjectCatalogHaveReadLimits(t *testing.T) {
	dir := t.TempDir()
	storagePath := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(storagePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(storagePath, maxCanonicalStorageBytes+1); err != nil {
		t.Fatal(err)
	}
	storage := &Storage{configDir: dir, configPath: storagePath}
	if _, _, _, err := storage.LoadAllWithSettings(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized canonical storage error = %v, want size refusal", err)
	}

	projectsPath := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(projectsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(projectsPath, maxProjectCatalogBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadProjects(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized project catalog error = %v, want size refusal", err)
	}
}
