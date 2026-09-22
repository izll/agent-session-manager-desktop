package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Gemini used to file transcripts under the sha256 of the project path and now
// uses the project's name, and it reads only the scheme it currently writes.
//
// A transcript in the old layout is on disk, matches by id, and carries a
// projectHash that agrees — and is still refused, because Gemini is looking in
// the other directory. Measured on a real store: twelve directories in the old
// scheme beside two in the new.
func TestGeminiIgnoresATranscriptItWouldNotFind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEMINI_CONFIG_DIR", filepath.Join(home, ".gemini"))

	const id = "6c6c9fe2-5c66-45d8-a06f-eea3b8c63560"
	projectDir := filepath.Join(home, "work", "myproject")
	oldScope := geminiProjectHash(projectDir)

	// Gemini records the path hash either way; what changed is the directory
	// it files the transcript in.
	write := func(scope string) {
		dir := filepath.Join(home, ".gemini", "tmp", scope, "chats")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(
			`{"sessionId":%q,"projectHash":%q,"messages":[{"type":"user"},{"type":"gemini"}]}`,
			id, oldScope)
		if err := os.WriteFile(
			filepath.Join(dir, "session-2026-01-01T00-00-"+id[:8]+".json"),
			[]byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Only the old scheme: Gemini has never written a named directory for this
	// project, so it is still reading the hashed one and will find it.
	write(oldScope)
	if !ResumeIDExistsForDir(AgentGemini, id, projectDir) {
		t.Error("a transcript in the only layout there is was rejected")
	}

	// Now the named directory exists, which is where Gemini looks. The old
	// transcript is unreachable to it, however well it matches.
	namedChats := filepath.Join(home, ".gemini", "tmp", "myproject", "chats")
	if err := os.MkdirAll(namedChats, 0o755); err != nil {
		t.Fatal(err)
	}
	if ResumeIDExistsForDir(AgentGemini, id, projectDir) {
		t.Error("a transcript Gemini no longer reads was reported as resumable; " +
			"the tab starts and dies with \"Invalid session identifier\"")
	}

	// And one written in the current scheme is found.
	write("myproject")
	if !ResumeIDExistsForDir(AgentGemini, id, projectDir) {
		t.Error("a transcript in the current layout was rejected")
	}
}
