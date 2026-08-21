package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskManagerRejectsOversizedStore(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".taskmaster")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxCanonicalStorageBytes+1); err != nil {
		t.Fatal(err)
	}
	manager := NewTaskManager(project)
	if err := manager.Load(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized task store error = %v, want size refusal", err)
	}
}
