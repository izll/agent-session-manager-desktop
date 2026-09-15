package session

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// linkCursorStore points a fake /proc file descriptor at a chat's store, the
// way a live cursor-agent holds one open.
func linkCursorStore(t *testing.T, procRoot, chatsRoot, projectPath, chatID, fd string, pid int) {
	t.Helper()
	sum := md5.Sum([]byte(filepath.Clean(projectPath)))
	chatDir := filepath.Join(chatsRoot, hex.EncodeToString(sum[:]), chatID)
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(chatDir, "store.db")
	if err := os.WriteFile(store, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procRoot, fmt.Sprint(pid), "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(fdDir, fd)); err != nil {
		t.Fatal(err)
	}
}

// The id is the directory the store sits in. Measured on a running pane: the
// agent's own process held store.db, store.db-wal and store.db-shm open, and
// nothing else from the chat store.
func TestCursorChatIDComesFromTheOpenStore(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")

	writeProcessChildren(t, procRoot, 100, "")
	linkCursorStore(t, procRoot, chatsRoot, project, "11111111-1111-4111-8111-111111111111", "7", 100)

	got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project)
	if got != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("chat id = %q, want the directory holding the open store", got)
	}
}

// The wal and shm files name the same chat; all three must not read as three.
func TestCursorSidecarFilesAreTheSameChat(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")
	chatID := "22222222-2222-4222-8222-222222222222"

	writeProcessChildren(t, procRoot, 100, "")
	linkCursorStore(t, procRoot, chatsRoot, project, chatID, "7", 100)
	sum := md5.Sum([]byte(filepath.Clean(project)))
	chatDir := filepath.Join(chatsRoot, hex.EncodeToString(sum[:]), chatID)
	for i, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(chatDir, "store.db"+suffix)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(procRoot, "100", "fd", fmt.Sprint(8+i))); err != nil {
			t.Fatal(err)
		}
	}

	if got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project); got != chatID {
		t.Errorf("chat id = %q, want %q — the sidecars are the same chat", got, chatID)
	}
}

// Two tabs in one project is the case the "most recently modified directory"
// shortcut could not answer: both would point at whichever wrote last.
func TestCursorTabsAreToldApartByTheirOwnProcess(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")

	writeProcessChildren(t, procRoot, 100, "")
	writeProcessChildren(t, procRoot, 200, "")
	linkCursorStore(t, procRoot, chatsRoot, project, "aaaaaaaa-1111-4111-8111-111111111111", "7", 100)
	linkCursorStore(t, procRoot, chatsRoot, project, "bbbbbbbb-2222-4222-8222-222222222222", "7", 200)

	first := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project)
	second := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 200, project)
	if first != "aaaaaaaa-1111-4111-8111-111111111111" || second != "bbbbbbbb-2222-4222-8222-222222222222" {
		t.Errorf("tabs were not told apart: %q and %q", first, second)
	}
}

// The chat of another project must not be picked up: the directory name is the
// project's path hash, so this is decided without reading anything.
func TestCursorChatFromAnotherProjectIsRefused(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")
	other := filepath.Join(tmp, "other")

	writeProcessChildren(t, procRoot, 100, "")
	linkCursorStore(t, procRoot, chatsRoot, other, "cccccccc-3333-4333-8333-333333333333", "7", 100)

	if got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project); got != "" {
		t.Errorf("chat id = %q, want empty: it belongs to another project", got)
	}
}

// A child process holding the store still counts — a tab whose command is a
// wrapper has the agent one level down.
func TestCursorChatIDIsFoundOnAChildProcess(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")

	writeProcessChildren(t, procRoot, 100, "101")
	writeProcessChildren(t, procRoot, 101, "")
	linkCursorStore(t, procRoot, chatsRoot, project, "dddddddd-4444-4444-8444-444444444444", "7", 101)

	if got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project); got != "dddddddd-4444-4444-8444-444444444444" {
		t.Errorf("a chat held by a child process was missed: %q", got)
	}
}

// Nothing open, nothing claimed. Guessing here would resume the wrong chat.
func TestCursorWithNoOpenStoreFindsNothing(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	writeProcessChildren(t, procRoot, 100, "")
	if got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot,
		filepath.Join(tmp, "chats"), 100, filepath.Join(tmp, "repo")); got != "" {
		t.Errorf("chat id = %q, want empty", got)
	}
}

// Two different chats open at once is ambiguous, and a wrong id is worse than
// none: resuming would land in a conversation the user never asked for.
func TestCursorTwoOpenChatsAreAmbiguous(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	chatsRoot := filepath.Join(tmp, "chats")
	project := filepath.Join(tmp, "repo")

	writeProcessChildren(t, procRoot, 100, "")
	linkCursorStore(t, procRoot, chatsRoot, project, "eeeeeeee-5555-4555-8555-555555555555", "7", 100)
	linkCursorStore(t, procRoot, chatsRoot, project, "ffffffff-6666-4666-8666-666666666666", "8", 100)

	if got := detectCursorChatIDFromProcessTreeContext(context.Background(), procRoot, chatsRoot, 100, project); got != "" {
		t.Errorf("chat id = %q, want empty when two chats are open", got)
	}
}

// A tab records its own id, and a tab that moved to another chat is corrected
// rather than left pointing at the one the user walked away from.
func TestCaptureCursorResumeIDsUpdatesTabs(t *testing.T) {
	inst := &Instance{
		ID:     "s1",
		Agent:  AgentCursor,
		Status: StatusRunning,
		Path:   "/repo",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentCursor, ResumeSessionID: "stale"},
			{Index: 3, Agent: AgentTerminal},
			{Index: 4, Agent: AgentCursor, Stopped: true, ResumeSessionID: "kept"},
		},
	}
	detect := func(_ string, windowIdx int, _ string) string {
		switch windowIdx {
		case 1:
			return "main-chat"
		case 2:
			return "tab-chat"
		}
		return ""
	}
	if !inst.captureCursorResumeIDsAtMainWindow(detect, 1, true) {
		t.Fatal("nothing was recorded")
	}
	if inst.ResumeSessionID != "main-chat" {
		t.Errorf("session id = %q", inst.ResumeSessionID)
	}
	if inst.FollowedWindows[0].ResumeSessionID != "tab-chat" {
		t.Errorf("a stale tab id was not corrected: %q", inst.FollowedWindows[0].ResumeSessionID)
	}
	if inst.FollowedWindows[2].ResumeSessionID != "kept" {
		t.Error("a stopped tab was touched")
	}
}

func TestNeedsCursorResumeCapture(t *testing.T) {
	stopped := &Instance{Agent: AgentCursor, Status: StatusStopped}
	if stopped.NeedsCursorResumeCapture() {
		t.Error("a stopped session was polled")
	}
	running := &Instance{Agent: AgentClaude, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 2, Agent: AgentCursor}}}
	if !running.NeedsCursorResumeCapture() {
		t.Error("a Cursor tab under another agent's session was skipped")
	}
}
