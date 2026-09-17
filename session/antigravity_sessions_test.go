package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeAntigravitySummaries builds the one table the picker reads, with the
// columns the real 1.2.3 store has.
func writeAntigravitySummaries(t *testing.T, home string, rows [][]any) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE conversation_summaries (
		conversation_id text, title text NOT NULL DEFAULT "",
		preview text NOT NULL DEFAULT "", step_count integer NOT NULL DEFAULT 0,
		last_modified_time datetime NOT NULL, workspace_uris text NOT NULL,
		status text NOT NULL DEFAULT "", last_user_input_time datetime NOT NULL,
		PRIMARY KEY (conversation_id))`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO conversation_summaries
			(conversation_id, title, preview, step_count, last_modified_time,
			 workspace_uris, status, last_user_input_time)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
}

func agRow(id, title, preview string, steps int, modified, workspaces string) []any {
	return []any{id, title, preview, steps, modified, workspaces, "CASCADE_RUN_STATUS_IDLE", modified}
}

// antigravityFileURI builds the file URI the CLI stores for a workspace.
//
// Not "file://" + path: on Windows that puts the drive where the host belongs
// (file://D:\\a\\repo) and leaves backslashes, which are not URI separators —
// the value parses to nothing and every conversation drops out of the picker.
func antigravityFileURI(path string) string {
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(path), "/")
}

func TestAntigravitySessionsAreScopedByWorkspaceURI(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	other := filepath.Join(home, "other")
	for _, d := range []string{project, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeAntigravitySummaries(t, home, [][]any{
		agRow("11111111-1111-4111-8111-111111111111", "Mine", "preview", 9,
			"2026-09-15 15:26:10.12+00:00", `["`+antigravityFileURI(project)+`"]`),
		agRow("22222222-2222-4222-8222-222222222222", "Theirs", "preview", 3,
			"2026-09-15 14:00:00+00:00", `["`+antigravityFileURI(other)+`"]`),
	})

	sessions, err := ListAntigravitySessions(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].FirstPrompt != "Mine" {
		t.Fatalf("workspace scoping failed: %#v", sessions)
	}
	if sessions[0].MessageCount != 9 {
		t.Errorf("step count = %d, want 9", sessions[0].MessageCount)
	}
	if sessions[0].UpdatedAt.IsZero() {
		t.Error("the modified time did not parse, so ordering is meaningless")
	}
}

// A conversation records its workspace only once work has started: one that
// stopped at the trust prompt has the column empty. It cannot be attributed to
// a project, so a project-scoped picker must not offer it.
func TestAntigravitySkipsConversationsWithNoWorkspace(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAntigravitySummaries(t, home, [][]any{
		agRow("33333333-3333-4333-8333-333333333333", "Starting a New Conversation", "", 2,
			"2026-09-15 15:26:10.12+00:00", ""),
	})

	if sessions, _ := ListAntigravitySessions(project); len(sessions) != 0 {
		t.Fatalf("a conversation with no workspace was offered: %#v", sessions)
	}
}

// A conversation can have more than one workspace open; any of them counts.
func TestAntigravityMatchesAnyOfSeveralWorkspaces(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAntigravitySummaries(t, home, [][]any{
		agRow("44444444-4444-4444-8444-444444444444", "Multi", "", 4,
			"2026-09-15 15:26:10.12+00:00",
			`["`+antigravityFileURI(filepath.Join(home, "elsewhere"))+`","`+antigravityFileURI(project)+`"]`),
	})

	if sessions, _ := ListAntigravitySessions(project); len(sessions) != 1 {
		t.Fatalf("a second workspace entry was ignored: %#v", sessions)
	}
}

// The timestamps arrive as SQLite datetimes with a numeric offset, which is
// not one of Go's named layouts — parsed wrong they all land at the epoch and
// the ordering silently stops meaning anything.
func TestAntigravityTimestampsParse(t *testing.T) {
	for _, value := range []string{
		"2026-09-15 15:26:10.122755857+00:00",
		"2026-09-15 13:51:48.36900996+00:00",
		"2026-09-15 15:26:10+00:00",
	} {
		if parseAntigravityTime(value).IsZero() {
			t.Errorf("did not parse: %q", value)
		}
	}
	if !parseAntigravityTime("nonsense").IsZero() {
		t.Error("nonsense parsed as a time")
	}
}

// A workspace on another machine is not a directory we can resume into.
func TestAntigravityIgnoresNonFileWorkspaces(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if antigravityWorkspaceMatches(`["ssh://host/repo"]`, project) {
		t.Error("a remote workspace was treated as a local path")
	}
	if antigravityWorkspaceMatches(`not json`, project) {
		t.Error("a malformed column was treated as a match")
	}
}

// A Windows file URI carries the drive inside the path: file:///C:/Users/...
// parses to "/C:/Users/...". The leading slash has to come off, or the path
// matches nothing and every conversation drops out of the picker.
func TestAntigravityWindowsFileURIKeepsItsDrive(t *testing.T) {
	got := antigravityURIToPath("file:///C:/Users/User/repo")
	if strings.HasPrefix(got, string(filepath.Separator)) && len(got) > 2 && got[2] == ':' {
		t.Errorf("path = %q, the leading separator was left in front of the drive", got)
	}
	if !strings.Contains(got, "C:") {
		t.Errorf("path = %q, the drive letter is gone", got)
	}
	// A POSIX URI keeps its segments. Compared by segment rather than as a
	// string: the separator is platform-specific, and pinning it to "/" made
	// this fail on Windows for the wrong reason.
	posix := antigravityURIToPath("file:///home/izll/repo")
	for _, want := range []string{"home", "izll", "repo"} {
		if !strings.Contains(posix, want) {
			t.Errorf("posix path = %q, %q is missing", posix, want)
		}
	}
	if strings.HasPrefix(posix, "//") {
		t.Errorf("posix path = %q, it gained a leading separator", posix)
	}
}

func TestAntigravityResumeIDValidation(t *testing.T) {
	home := isolateHome(t)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "55555555-5555-4555-8555-555555555555"
	if err := os.WriteFile(filepath.Join(dir, id+".db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ResumeIDExists(AgentAntigravity, id) {
		t.Error("an existing conversation was reported missing")
	}
	if ResumeIDExists(AgentAntigravity, "66666666-6666-4666-8666-666666666666") {
		t.Error("a conversation that does not exist was reported present")
	}
}

// Missing store, no crash, no error — a fresh install has none of this.
func TestAntigravityWithNoStoreAtAll(t *testing.T) {
	home := isolateHome(t)
	sessions, err := ListAntigravitySessions(filepath.Join(home, "repo"))
	if err != nil || len(sessions) != 0 {
		t.Fatalf("got %#v, %v; want empty and no error", sessions, err)
	}
}
