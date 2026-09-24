package main

import (
	"testing"

	"asmgr-desktop/session"
)

func globalSearchNoteFixture() []*session.Instance {
	return []*session.Instance{{
		ID: "s1", Name: "API",
		Notes:           "release plan",
		FollowedWindows: []session.FollowedWindow{{ID: "t-2", Index: 2, Name: "Tests", Notes: "Release blockers"}},
	}}
}

// Note hits come first and carry what the frontend needs to open them.
func TestMergeSearchResultsListsNotesFirst(t *testing.T) {
	history := []session.HistoryEntry{{ID: "h1", Agent: session.AgentClaude, Snippet: "release notes"}}
	got := mergeSearchResults(history, false, globalSearchNoteFixture(), "release")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(got), got)
	}
	sessionNote, tabNote, conversation := got[0], got[1], got[2]
	if sessionNote.Kind != historyKindNote || sessionNote.NoteScope != "session" ||
		sessionNote.WindowIdx != SessionNotesWindow || sessionNote.SessionName != "API" || sessionNote.SessionID != "s1" {
		t.Fatalf("session note = %+v", sessionNote)
	}
	if tabNote.NoteScope != "tab" || tabNote.TabID != "t-2" || tabNote.TabName != "Tests" || tabNote.WindowIdx != 2 {
		t.Fatalf("tab note = %+v", tabNote)
	}
	if conversation.Kind != "" || conversation.ID != "h1" {
		t.Fatalf("conversation = %+v", conversation)
	}
}

// Exact hits anywhere beat typo-tolerant guesses anywhere, as they do within
// the history search itself.
func TestMergeSearchResultsExactNotesDropFuzzyHistory(t *testing.T) {
	fuzzyHistory := []session.HistoryEntry{{ID: "h1", Snippet: "r e l e a s e"}}
	got := mergeSearchResults(fuzzyHistory, true, globalSearchNoteFixture(), "release")
	for _, r := range got {
		if r.Kind != historyKindNote {
			t.Fatalf("fuzzy history result kept beside exact note hits: %+v", r)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want the 2 notes", len(got))
	}
}

func TestMergeSearchResultsFallsBackToFuzzyNotes(t *testing.T) {
	got := mergeSearchResults(nil, false, globalSearchNoteFixture(), "rlsplan")
	if len(got) != 1 || got[0].NoteScope != "session" {
		t.Fatalf("fuzzy fallback = %+v", got)
	}
	// With exact history hits there is no call for guesses.
	exact := []session.HistoryEntry{{ID: "h1", Snippet: "rlsplan"}}
	if got := mergeSearchResults(exact, false, globalSearchNoteFixture(), "rlsplan"); len(got) != 1 || got[0].Kind != "" {
		t.Fatalf("fuzzy notes added beside exact history hits: %+v", got)
	}
}

// The search issues note results in the same window numbering the notes API
// reads them back in.
func TestSessionNotesWindowAgreesWithSearch(t *testing.T) {
	if session.SessionNotesWindow != SessionNotesWindow {
		t.Fatalf("session.SessionNotesWindow = %d, app = %d", session.SessionNotesWindow, SessionNotesWindow)
	}
}
