package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Starting a Gemini tab on a session it cannot resume printed
//
//	Invalid session identifier "..." — No previous sessions found for this project
//
// twice and left a dead tab. Nothing checked the id first: Gemini fell through
// to the "unknown agent, assume it exists" branch.

func writeGeminiTranscript(t *testing.T, sessionID, projectDir string) {
	t.Helper()
	dir := os.Getenv("GEMINI_CONFIG_DIR")
	hash := sha256.Sum256([]byte(projectDir))
	project := hex.EncodeToString(hash[:])
	chats := filepath.Join(dir, "tmp", project, "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	// The filename carries only the first eight characters of the id, which is
	// why a filename match cannot be trusted on its own.
	name := "session-2026-09-03T07-13-" + sessionID[:8] + ".json"
	body := `{"sessionId":"` + sessionID + `","projectHash":"` + project + `",` +
		`"messages":[{"type":"user","content":"hello"}]}`
	if err := os.WriteFile(filepath.Join(chats, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiSessionInTheSameDirectoryResumes(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_DIR", t.TempDir())
	project := "/home/someone/project"
	id := "c78329d0-1ff9-41fd-bb6e-448099661341"
	writeGeminiTranscript(t, id, project)

	if !ResumeIDExistsForDir(AgentGemini, id, project) {
		t.Fatal("a session recorded for this very directory was rejected")
	}
}

// The failure that was reported: the transcript exists, but Gemini refuses it
// from anywhere else, so treating it as present starts a tab that dies.
func TestGeminiSessionFromAnotherDirectoryIsRejected(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_DIR", t.TempDir())
	id := "c78329d0-1ff9-41fd-bb6e-448099661341"
	writeGeminiTranscript(t, id, "/home/someone/project")

	if ResumeIDExistsForDir(AgentGemini, id, "/home/someone/elsewhere") {
		t.Fatal("a session belonging to another directory was accepted — Gemini " +
			"will refuse it and the tab will die on start")
	}
}

func TestUnknownGeminiSessionIsRejected(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_DIR", t.TempDir())
	if ResumeIDExistsForDir(AgentGemini, "00000000-1111-2222-3333-444444444444", "/tmp") {
		t.Fatal("an id with no transcript at all was accepted")
	}
}

// The filename holds eight characters of the id. Two sessions can share those
// and be different conversations.
func TestMatchingFilenamePrefixIsNotEnough(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_DIR", t.TempDir())
	project := "/home/someone/project"
	writeGeminiTranscript(t, "c78329d0-1ff9-41fd-bb6e-448099661341", project)

	// Same first eight characters, different session.
	if ResumeIDExistsForDir(AgentGemini, "c78329d0-9999-9999-9999-999999999999", project) {
		t.Fatal("a different session was accepted on a matching filename prefix")
	}
}

// The other agents keep their existing behaviour through the new entry point.
func TestOtherAgentsAreUnaffected(t *testing.T) {
	if ResumeIDExistsForDir(AgentClaude, "", "/tmp") {
		t.Error("an empty id should never be resumable")
	}
	if ResumeIDExistsForDir(AgentGemini, "../escape", "/tmp") {
		t.Error("an unsafe id must be refused before it reaches a command line")
	}
}

// The failure as it actually reached the user: a session that never got going.
//
// A login that fails leaves a transcript of nothing but info and error notices,
// and Gemini hides those from its own listing — so the file sits on disk while
// the CLI insists there are "no previous sessions for this project". Resuming
// it printed that on every start and left a dead tab.
func TestGeminiSessionOfOnlyNoticesIsRejected(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_DIR", dir)
	project := "/home/someone/project"
	id := "c78329d0-1ff9-41fd-bb6e-448099661341"

	hash := sha256.Sum256([]byte(project))
	projectHash := hex.EncodeToString(hash[:])
	chats := filepath.Join(dir, "tmp", projectHash, "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"sessionId":"` + id + `","projectHash":"` + projectHash + `","messages":[` +
		`{"type":"info","content":"Loaded cached credentials."},` +
		`{"type":"error","content":"Failed to login."}]}`
	name := "session-2026-09-03T07-13-" + id[:8] + ".json"
	if err := os.WriteFile(filepath.Join(chats, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if ResumeIDExistsForDir(AgentGemini, id, project) {
		t.Fatal("a transcript of only notices was accepted; Gemini hides those, " +
			"so the tab starts and immediately dies")
	}
}

// An assistant reply alone is a real conversation too — the rule is "anything
// that is not a notice", not "a user message".
func TestGeminiSessionWithOnlyAnAssistantReplyResumes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_DIR", dir)
	project := "/home/someone/project"
	id := "aabbccdd-1111-2222-3333-444444444444"

	hash := sha256.Sum256([]byte(project))
	projectHash := hex.EncodeToString(hash[:])
	chats := filepath.Join(dir, "tmp", projectHash, "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"sessionId":"` + id + `","projectHash":"` + projectHash + `","messages":[` +
		`{"type":"info","content":"Loaded cached credentials."},` +
		`{"type":"gemini","content":"Here you go."}]}`
	name := "session-2026-09-03T07-13-" + id[:8] + ".json"
	if err := os.WriteFile(filepath.Join(chats, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if !ResumeIDExistsForDir(AgentGemini, id, project) {
		t.Fatal("a session with a real reply in it was rejected")
	}
}
