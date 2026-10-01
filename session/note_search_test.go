package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func noteSearchFixture() []*Instance {
	return []*Instance{
		{
			ID: "s1", Name: "API",
			Notes:        "Deploy checklist: run the Migration before release",
			MainTabNotes: "main tab: remember the migration flag",
			FollowedWindows: []FollowedWindow{
				{ID: "t-2", Index: 2, Name: "Tests", Notes: "flaky MIGRATION test in CI"},
				{ID: "t-3", Index: 3, Notes: "unnamed tab mentions migration too"},
				{ID: "t-4", Index: 4, Name: "Empty", Notes: "   \n "},
			},
		},
		{ID: "s2", Name: "Web", Notes: "nothing relevant here"},
	}
}

func findMatch(t *testing.T, matches []NoteMatch, sessionID string, scope NoteScope, tabID string) NoteMatch {
	t.Helper()
	for _, m := range matches {
		if m.SessionID == sessionID && m.Scope == scope && m.TabID == tabID {
			return m
		}
	}
	t.Fatalf("no %s note (tab %q) of %s among %+v", scope, tabID, sessionID, matches)
	return NoteMatch{}
}

// Every kind of note is searched — the session's, the main tab's and each
// followed tab's — and each result says where it came from, so opening it
// lands on the right note.
func TestSearchNotesFindsSessionMainAndTabNotes(t *testing.T) {
	matches := SearchNotes(NoteSources{Instances: noteSearchFixture()}, "migration")
	if len(matches) != 4 {
		t.Fatalf("got %d matches, want 4: %+v", len(matches), matches)
	}

	session := findMatch(t, matches, "s1", NoteScopeSession, "")
	if session.WindowIndex != SessionNotesWindow || session.SessionName != "API" || session.TabName != "" {
		t.Fatalf("session note result = %+v", session)
	}

	main := findMatch(t, matches, "s1", NoteScopeTab, MainTabID)
	if main.TabName != "API" {
		t.Fatalf("main tab is labelled with the session name, got %q", main.TabName)
	}

	tab := findMatch(t, matches, "s1", NoteScopeTab, "t-2")
	if tab.WindowIndex != 2 || tab.TabName != "Tests" {
		t.Fatalf("followed tab result = %+v", tab)
	}

	unnamed := findMatch(t, matches, "s1", NoteScopeTab, "t-3")
	if unnamed.TabName != "Tab 3" || unnamed.WindowIndex != 3 {
		t.Fatalf("unnamed tab result = %+v", unnamed)
	}
}

func TestSearchNotesIgnoresCase(t *testing.T) {
	for _, query := range []string{"MIGRATION", "Migration", "migration"} {
		if got := len(SearchNotes(NoteSources{Instances: noteSearchFixture()}, query)); got != 4 {
			t.Fatalf("query %q found %d notes, want 4", query, got)
		}
	}
}

// A blank note is no note: whitespace alone must not turn up as a result,
// even for a query that is itself a space.
func TestSearchNotesSkipsEmptyNotes(t *testing.T) {
	for _, m := range SearchNotes(NoteSources{Instances: noteSearchFixture()}, " ") {
		if m.TabID == "t-4" {
			t.Fatalf("blank note returned: %+v", m)
		}
	}
	if got := SearchNotes(NoteSources{Instances: noteSearchFixture()}, ""); len(got) != 0 {
		t.Fatalf("empty query returned %d notes", len(got))
	}
	if got := SearchNotes(NoteSources{Instances: []*Instance{{ID: "x", Name: "X"}}}, "a"); len(got) != 0 {
		t.Fatalf("session without notes returned %+v", got)
	}
}

func TestSearchNotesSnippetSurroundsMatch(t *testing.T) {
	text := strings.Repeat("lorem ipsum ", 20) + "the NEEDLE sits\nhere " + strings.Repeat("dolor sit ", 20)
	matches := SearchNotes(NoteSources{Instances: []*Instance{{ID: "s", Name: "S", Notes: text}}}, "needle")
	if len(matches) != 1 {
		t.Fatalf("got %d matches", len(matches))
	}
	snippet := matches[0].Snippet
	if !strings.Contains(snippet, "the NEEDLE sits here") {
		t.Fatalf("snippet lacks the match (with its line break folded): %q", snippet)
	}
	if !strings.HasPrefix(snippet, "...") || !strings.HasSuffix(snippet, "...") {
		t.Fatalf("a cut snippet should say so at both ends: %q", snippet)
	}
	if matches[0].Text != text {
		t.Fatal("the full note text must travel with the match for the preview")
	}
}

// Lower-casing changes the byte length of some characters, so a byte offset
// found in the lowered text cut the original mid-character.
func TestSearchNotesSnippetStaysValidUTF8(t *testing.T) {
	text := strings.Repeat("İé", 60) + "árvíztűrő tükörfúrógép" + strings.Repeat("İ", 80)
	matches := SearchNotes(NoteSources{Instances: []*Instance{{ID: "s", Name: "S", Notes: text}}}, "TÜKÖR")
	if len(matches) != 1 {
		t.Fatalf("got %d matches", len(matches))
	}
	if !utf8.ValidString(matches[0].Snippet) || !strings.Contains(matches[0].Snippet, "tükörfúrógép") {
		t.Fatalf("snippet = %q", matches[0].Snippet)
	}
}

func TestFindNoteByResultID(t *testing.T) {
	src := NoteSources{Instances: noteSearchFixture()}
	for _, m := range SearchNotes(src, "migration") {
		got, ok := FindNote(src, m.ID())
		if !ok || got.Text != m.Text {
			t.Fatalf("FindNote(%q) = %+v, %v", m.ID(), got, ok)
		}
		if !IsNoteResultID(m.ID()) {
			t.Fatalf("%q not recognised as a note result", m.ID())
		}
	}
	if _, ok := FindNote(src, NoteResultID("s1", "gone", LegacyNotePageID)); ok {
		t.Fatal("a note that no longer exists was found")
	}
	if IsNoteResultID(generateHistoryID()) {
		t.Fatal("a history ID was taken for a note")
	}
}

func TestFuzzySearchNotesToleratesTypos(t *testing.T) {
	matches := FuzzySearchNotes(NoteSources{Instances: []*Instance{{ID: "s", Name: "S", Notes: "deployment checklist"}}}, "dplymnt")
	if len(matches) != 1 || matches[0].Scope != NoteScopeSession {
		t.Fatalf("fuzzy matches = %+v", matches)
	}
}

// Notes are read from sessions.json, without asking tmux about any session —
// the project's own note with the sessions'.
func TestLoadNoteSourcesReadsNotes(t *testing.T) {
	storage := newTestStorage(t)
	instances := []*Instance{{ID: "a", Name: "API", Path: "/tmp/api", Status: StatusStopped,
		Notes: "session note", MainTabNotes: "main note",
		FollowedWindows: []FollowedWindow{{ID: "t1", Index: 1, Name: "T", Notes: "tab note"}}}}
	if err := storage.SaveAll(instances, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetProjectNotePages([]NotePage{{ID: "p1", Title: "Roadmap", Text: "project note"}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := storage.LoadNoteSources()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(SearchNotes(loaded, "NOTE")); got != 4 {
		t.Fatalf("found %d stored notes, want 4", got)
	}
}

// The project's own note is searched, page by page, before the sessions'
// notes; a result names no session, and its ID finds it again.
func TestSearchNotesFindsTheProjectNote(t *testing.T) {
	src := NoteSources{
		Instances: noteSearchFixture(),
		ProjectPages: []NotePage{
			{ID: "p-plan", Title: "Plan", Text: "nothing to see"},
			{ID: "p-db", Title: "Database", Text: "the migration window is Sunday"},
		},
	}
	matches := SearchNotes(src, "migration")
	if len(matches) != 5 {
		t.Fatalf("got %d matches, want 5: %+v", len(matches), matches)
	}
	project := matches[0]
	if project.Scope != NoteScopeProject || project.SessionID != "" || project.SessionName != "" ||
		project.TabID != "" || project.WindowIndex != SessionNotesWindow ||
		project.PageID != "p-db" || project.PageTitle != "Database" || project.PageIndex != 1 || project.PageCount != 2 ||
		!strings.Contains(project.Snippet, "migration") {
		t.Fatalf("project note result = %+v", project)
	}
	if project.ID() != NoteResultID("", "project", "p-db") {
		t.Fatalf("project note ID = %q", project.ID())
	}
	found, ok := FindNote(src, project.ID())
	if !ok || found.Text != "the migration window is Sunday" {
		t.Fatalf("FindNote(%q) = %+v, %v", project.ID(), found, ok)
	}
	// By its title, and loosely.
	if got := SearchNotes(src, "plan"); len(got) != 1 || got[0].Scope != NoteScopeProject || got[0].PageID != "p-plan" {
		t.Fatalf("title search = %+v", got)
	}
	if got := FuzzySearchNotes(NoteSources{ProjectPages: src.ProjectPages}, "dtbase"); len(got) == 0 || got[0].PageID != "p-db" {
		t.Fatalf("fuzzy search = %+v", got)
	}
	// An empty project note adds nothing.
	if got := SearchNotes(NoteSources{ProjectPages: []NotePage{{ID: "x"}}}, " "); len(got) != 0 {
		t.Fatalf("empty project note matched: %+v", got)
	}
}
