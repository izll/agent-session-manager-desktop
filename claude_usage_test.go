package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeCredentialsReadIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readClaudeUsageFileAtMost(path, 4); err == nil {
		t.Fatal("oversized credentials file was accepted")
	}
	if raw, err := readClaudeUsageFileAtMost(path, 5); err != nil || string(raw) != "12345" {
		t.Fatalf("bounded credentials read = %q, %v", raw, err)
	}
}
