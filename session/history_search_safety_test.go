package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHistoryIndexLazyLoadIsConcurrent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	index := NewHistoryIndex()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = index.Search("needle")
			_ = index.IsLoaded()
		}()
	}
	wg.Wait()
	if !index.IsLoaded() {
		t.Fatal("concurrent lazy load did not publish a complete index")
	}
}

func TestGeminiHistoryRejectsUnknownProjectAndOversizedPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	knownPath := filepath.Join(home, "known")
	knownHash := sha256.Sum256([]byte(knownPath))
	unknownHash := sha256.Sum256([]byte(filepath.Join(home, "unknown")))

	writeSession := func(hash, name, content string) string {
		t.Helper()
		dir := filepath.Join(home, ".gemini", "tmp", hash, "chats")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		raw, err := json.Marshal(geminiSession{
			SessionID: name,
			Messages:  []geminiMessage{{Type: "user", Content: content}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	writeSession(hex.EncodeToString(unknownHash[:]), "session-unknown.json", "private")
	knownFile := writeSession(hex.EncodeToString(knownHash[:]), "session-known.json", "visible")
	oversizedFile := writeSession(hex.EncodeToString(knownHash[:]), "session-oversized.json", strings.Repeat("x", historyPreviewLimit+1))

	index := NewHistoryIndex()
	index.SetInstances([]*Instance{{Path: knownPath}})
	index.mu.Lock()
	index.loadBytes = 0
	entries := index.parseGeminiHistory()
	index.mu.Unlock()
	if len(entries) != 1 || entries[0].SessionFile != knownFile {
		t.Fatalf("Gemini project allowlist returned %#v", entries)
	}
	oversized := HistoryEntry{Agent: AgentGemini, SessionFile: oversizedFile}
	if _, err := oversized.LoadConversation(); err == nil {
		t.Fatal("oversized Gemini conversation preview was accepted")
	}
}

func TestHistorySearchResultAndIndexBudgets(t *testing.T) {
	index := NewHistoryIndex()
	index.mu.Lock()
	index.loaded = true
	for n := 0; n < historySearchResultLimit+10; n++ {
		if !index.appendHistoryEntry(&index.entries, HistoryEntry{
			ID:      generateHistoryID(),
			Content: "needle full private content",
		}) {
			t.Fatal("small test entry unexpectedly exceeded index budget")
		}
	}
	index.mu.Unlock()
	results := index.Search("needle")
	if len(results) != historySearchResultLimit {
		t.Fatalf("search returned %d results, want cap %d", len(results), historySearchResultLimit)
	}
}
