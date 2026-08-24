package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirAtMostRejectsOversizedDirectoryWithoutPartialPublication(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := readDirAtMost(dir, 2); err == nil || entries != nil {
		t.Fatalf("readDirAtMost = (%v, %v), want nil oversized error", entries, err)
	}
	entries, err := readDirAtMost(dir, 3)
	if err != nil || len(entries) != 3 {
		t.Fatalf("bounded directory = (%d, %v), want (3, nil)", len(entries), err)
	}
}

func TestListAgentSessionsFailsClosedOnOversizedProjectsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	projectsDir := filepath.Join(home, ".claude", "projects")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= maxDiscoveryDirectoryEntries; index++ {
		if err := os.Mkdir(filepath.Join(projectsDir, fmt.Sprintf("project-%05d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if sessions, err := ListAgentSessions(home); err == nil || sessions != nil {
		t.Fatalf("ListAgentSessions = (%v, %v), want nil oversized error", sessions, err)
	}
}

func TestDetectGeminiSessionIDFailsClosedOnOversizedChatsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	projectPath := filepath.Join(home, "repo")
	hash := sha256.Sum256([]byte(projectPath))
	chatsDir := filepath.Join(home, ".gemini", "tmp", hex.EncodeToString(hash[:]), "chats")
	if err := os.MkdirAll(chatsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	validID := "123e4567-e89b-12d3-a456-426614174000"
	if err := os.WriteFile(filepath.Join(chatsDir, "session-valid.json"), []byte(`{"sessionId":"`+validID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxDiscoveryDirectoryEntries; index++ {
		name := fmt.Sprintf("foreign-%05d", index)
		if err := os.WriteFile(filepath.Join(chatsDir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := DetectGeminiSessionID(projectPath); got != "" {
		t.Fatalf("oversized chats directory yielded session ID %q", got)
	}
}
