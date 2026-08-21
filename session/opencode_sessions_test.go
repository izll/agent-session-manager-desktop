package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListOpenCodeSessionsIsProjectScopedAndSkipsInvalidIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionDir := filepath.Join(home, ".local", "share", "opencode", "storage", "session", "project-id")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "repo")
	other := project + "2"
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name, id, directory string) {
		t.Helper()
		data := fmt.Sprintf(`{"id":%q,"directory":%q,"title":"title","time":{"created":1,"updated":2}}`, id, directory)
		if err := os.WriteFile(filepath.Join(sessionDir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("wanted.json", "wanted", project)
	write("nested.json", "nested", filepath.Join(project, "child"))
	write("sibling.json", "sibling", other)
	write("unknown.json", "unknown", "")
	write(".json", "", project)

	sessions, err := ListOpenCodeSessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got sessions %#v, want only project and descendant records", sessions)
	}
	got := map[string]bool{}
	for _, item := range sessions {
		got[item.SessionID] = true
	}
	if !got["wanted"] || !got["nested"] {
		t.Fatalf("unexpected project-scoped IDs: %#v", got)
	}
}
