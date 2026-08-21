package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMutateTaskMasterFileRejectsOversizedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxTaskMasterFileBytes+1); err != nil {
		t.Fatal(err)
	}
	err := MutateTaskMasterFile(path, func(map[string]interface{}) error {
		t.Fatal("mutator ran for oversized store")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized TaskMaster store error = %v, want size refusal", err)
	}
}

func TestMutateTaskMasterFileRejectsTrailingJSONWithoutRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	original := []byte(`{"master":{"tasks":[]}} {"foreign":"keep"}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := MutateTaskMasterFile(path, func(root map[string]interface{}) error {
		root["changed"] = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing JSON error = %v, want parse refusal", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("refused mutation rewrote corrupt provider file: %q", got)
	}
}
