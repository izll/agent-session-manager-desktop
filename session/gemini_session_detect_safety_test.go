package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGeminiSessionIDRejectsOversizedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{"sessionId":"123e4567-e89b-12d3-a456-426614174000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := extractGeminiSessionIDAtMost(path, 8); got != "" {
		t.Fatalf("oversized Gemini metadata yielded session ID %q", got)
	}
	if got := extractGeminiSessionIDAtMost(path, 1024); got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("bounded Gemini metadata yielded %q", got)
	}
}
