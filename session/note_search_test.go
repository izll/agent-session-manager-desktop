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
	matches := SearchNotes(noteSearchFixture(), "migration")
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
		if got := len(SearchNotes(noteSearchFixture(), query)); got != 4 {
			t.Fatalf("query %q found %d notes, want 4", query, got)
		}
	}
}

// A blank note is no note: whitespace alone must not turn up as a result,
// even for a query that is itself a space.
func TestSearchNotesSkipsEmptyNotes(t *testing.T) {
	for _, m := range SearchNotes(noteSearchFixture(), " ") {
		if m.TabID == "t-4" {
			t.Fatalf("blank note returned: %+v", m)
		}
	}
	if got := SearchNotes(noteSearchFixture(), ""); len(got) != 0 {
		t.Fatalf("empty query returned %d notes", len(got))
	}
	if got := SearchNotes([]*Instance{{ID: "x", Name: "X"}}, "a"); len(got) != 0 {
		t.Fatalf("session without notes returned %+v", got)
	}
}

func TestSearchNotesSnippetSurroundsMatch(t *testing.T) {
	text := strings.Repeat("lorem ipsum ", 20) + "the NEEDLE sits\nhere " + strings.Repeat("dolor sit ", 20)
	matches := SearchNotes([]*Instance{{ID: "s", Name: "S", Notes: text}}, "needle")
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
	matches := SearchNotes([]*Instance{{ID: "s", Name: "S", Notes: text}}, "TÜKÖR")
	if len(matches) != 1 {
		t.Fatalf("got %d matches", len(matches))
	}
	if !utf8.ValidString(matches[0].Snippet) || !strings.Contains(matches[0].Snippet, "tükörfúrógép") {
		t.Fatalf("snippet = %q", matches[0].Snippet)
	}
}

func TestFindNoteByResultID(t *testing.T) {
	instances := noteSearchFixture()
	for _, m := range SearchNotes(instances, "migration") {
		got, ok := FindNote(instances, m.ID())
		if !ok || got.Text != m.Text {
			t.Fatalf("FindNote(%q) = %+v, %v", m.ID(), got, ok)
		}
		if !IsNoteResultID(m.ID()) {
			t.Fatalf("%q not recognised as a note result", m.ID())
		}
	}
	if _, ok := FindNote(instances, NoteResultID("s1", "gone")); ok {
		t.Fatal("a note that no longer exists was found")
	}
	if IsNoteResultID(generateHistoryID()) {
		t.Fatal("a history ID was taken for a note")
	}
}

func TestFuzzySearchNotesToleratesTypos(t *testing.T) {
	matches := FuzzySearchNotes([]*Instance{{ID: "s", Name: "S", Notes: "deployment checklist"}}, "dplymnt")
	if len(matches) != 1 || matches[0].Scope != NoteScopeSession {
		t.Fatalf("fuzzy matches = %+v", matches)
	}
}

// Notes are read from sessions.json, without asking tmux about any session.
func TestLoadStoredInstancesReadsNotes(t *testing.T) {
	storage := newTestStorage(t)
	instances := []*Instance{{ID: "a", Name: "API", Path: "/tmp/api", Status: StatusStopped,
		Notes: "session note", MainTabNotes: "main note",
		FollowedWindows: []FollowedWindow{{ID: "t1", Index: 1, Name: "T", Notes: "tab note"}}}}
	if err := storage.SaveAll(instances, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	loaded, err := storage.LoadStoredInstances()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(SearchNotes(loaded, "NOTE")); got != 3 {
		t.Fatalf("found %d stored notes, want 3", got)
	}
}
