package session

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// writeCursorChat lays out one chat the way Cursor does: a directory named
// after the chat id, under a directory named after the project's path hash.
func writeCursorChat(t *testing.T, home, projectPath, chatID, meta, prompts string) {
	t.Helper()
	sum := md5.Sum([]byte(filepath.Clean(projectPath)))
	dir := filepath.Join(home, ".cursor", "chats", hex.EncodeToString(sum[:]), chatID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if meta != "" {
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if prompts != "" {
		if err := os.WriteFile(filepath.Join(dir, "prompt_history.json"), []byte(prompts), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The directory name is the MD5 of the project path — measured against a real
// store, where all three project directories matched exactly. Getting this
// wrong finds nothing at all, silently.
func TestCursorSessionsAreFoundByPathHash(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "11111111-1111-4111-8111-111111111111",
		`{"title":"Something","createdAtMs":1700000000000,"updatedAtMs":1700000100000,"hasConversation":true}`,
		`["first prompt","second prompt"]`)

	sessions, err := ListCursorSessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1: %#v", len(sessions), sessions)
	}
	got := sessions[0]
	if got.SessionID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("session id = %q", got.SessionID)
	}
	// The typed prompt, not Cursor's own summary: it is what the user recognises.
	if got.FirstPrompt != "first prompt" {
		t.Errorf("label = %q, want the first typed prompt", got.FirstPrompt)
	}
	if got.LastPrompt != "second prompt" {
		t.Errorf("last prompt = %q", got.LastPrompt)
	}
	if got.MessageCount != 2 {
		t.Errorf("message count = %d, want 2", got.MessageCount)
	}
}

// A trailing separator changes the digest and finds nothing. This is the one
// mistake that produces an empty list rather than an error.
func TestCursorPathHashIgnoresTrailingSeparator(t *testing.T) {
	plain := cursorProjectHash("/tmp/some/project")
	trailing := cursorProjectHash("/tmp/some/project/")
	if plain != trailing {
		t.Errorf("a trailing separator changed the hash: %s vs %s", plain, trailing)
	}
}

// Another project's chats must not appear: the picker is scoped to one.
func TestCursorSessionsAreScopedToTheProject(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	other := filepath.Join(home, "other")
	for _, dir := range []string{project, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeCursorChat(t, home, project, "aaaaaaaa-1111-4111-8111-111111111111",
		`{"hasConversation":true}`, `["mine"]`)
	writeCursorChat(t, home, other, "bbbbbbbb-2222-4222-8222-222222222222",
		`{"hasConversation":true}`, `["theirs"]`)

	sessions, _ := ListCursorSessions(project)
	if len(sessions) != 1 || sessions[0].FirstPrompt != "mine" {
		t.Fatalf("scoping failed: %#v", sessions)
	}
}

// Cursor leaves a directory behind for a chat that was opened and never used.
// Four of the six in a real store were like that, and offering them to resume
// gives the user an empty conversation.
func TestCursorSkipsChatsThatWereNeverUsed(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "cccccccc-3333-4333-8333-333333333333",
		`{"hasConversation":false}`, `[]`)
	writeCursorChat(t, home, project, "dddddddd-4444-4444-8444-444444444444",
		`{"hasConversation":true}`, `["real work"]`)

	sessions, _ := ListCursorSessions(project)
	if len(sessions) != 1 || sessions[0].FirstPrompt != "real work" {
		t.Fatalf("empty chats were offered: %#v", sessions)
	}
}

// A chat resumed from elsewhere can have a title and no local prompt history.
func TestCursorFallsBackToTheTitle(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "eeeeeeee-5555-4555-8555-555555555555",
		`{"title":"Titled elsewhere","hasConversation":true}`, "")

	sessions, _ := ListCursorSessions(project)
	if len(sessions) != 1 || sessions[0].FirstPrompt != "Titled elsewhere" {
		t.Fatalf("title fallback failed: %#v", sessions)
	}
}

// Newest first, or the picker buries what the user was last doing.
func TestCursorSessionsAreSortedNewestFirst(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		`{"updatedAtMs":1700000000000,"hasConversation":true}`, `["older"]`)
	writeCursorChat(t, home, project, "22222222-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		`{"updatedAtMs":1800000000000,"hasConversation":true}`, `["newer"]`)

	sessions, _ := ListCursorSessions(project)
	if len(sessions) != 2 || sessions[0].FirstPrompt != "newer" {
		t.Fatalf("ordering failed: %#v", sessions)
	}
}

// A directory name reaches a command line as --resume <id>. Anything that is
// not a plain identifier is refused rather than trusted.
func TestCursorRefusesAnUnsafeChatDirectory(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "--not-an-id", `{"hasConversation":true}`, `["x"]`)

	if sessions, _ := ListCursorSessions(project); len(sessions) != 0 {
		t.Fatalf("an unsafe id was offered: %#v", sessions)
	}
}

func TestCursorResumeIDValidation(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCursorChat(t, home, project, "ffffffff-6666-4666-8666-666666666666",
		`{"hasConversation":true}`, `["x"]`)

	if !ResumeIDExists(AgentCursor, "ffffffff-6666-4666-8666-666666666666") {
		t.Error("an existing chat was reported missing")
	}
	if ResumeIDExists(AgentCursor, "99999999-9999-4999-8999-999999999999") {
		t.Error("a chat that does not exist was reported present")
	}
	// With the directory known the answer is scoped to it.
	if !ResumeIDExistsForDir(AgentCursor, "ffffffff-6666-4666-8666-666666666666", project) {
		t.Error("the project-scoped check missed its own chat")
	}
	if ResumeIDExistsForDir(AgentCursor, "ffffffff-6666-4666-8666-666666666666",
		filepath.Join(home, "elsewhere")) {
		t.Error("a chat from another project passed the scoped check")
	}
}
