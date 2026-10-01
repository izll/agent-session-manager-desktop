package main

import (
	"testing"

	"asmgr-desktop/session"
)

// Notes can be written for the session as a whole or for one tab. The main
// tab's note used to BE the session's note; it is its own now, so choosing
// "session" on the main tab and "this tab" there reach different notes.
func TestNotesReachTheSessionOrTheTab(t *testing.T) {
	inst := &session.Instance{
		Notes: "session",
		FollowedWindows: []session.FollowedWindow{
			{Index: 2, Notes: "tab 2"},
		},
	}

	if got := session.NotePagesText(noteSlot(inst, SessionNotesWindow).Pages()); got != "session" {
		t.Errorf("the session target reads %q", got)
	}
	if got := session.NotePagesText(noteSlot(inst, 2).Pages()); got != "tab 2" {
		t.Errorf("a followed tab reads %q", got)
	}

	noteSlot(inst, 0).SetPages([]session.NotePage{{ID: "p", Text: "main tab"}})
	if inst.MainTabNotes != "main tab" || inst.Notes != "session" {
		t.Errorf("writing the main tab's note changed the session's: main=%q session=%q",
			inst.MainTabNotes, inst.Notes)
	}
}

// The main tab was recognised as index 0, true only while tmux's base-index is
// 0. With base-index 1 its note could not be saved at all. Anything that is not
// a followed tab is the main tab.
func TestTheMainTabIsFoundWhateverItsIndex(t *testing.T) {
	inst := &session.Instance{FollowedWindows: []session.FollowedWindow{{Index: 2}}}
	noteSlot(inst, 1).SetPages([]session.NotePage{{ID: "p", Text: "on base-index 1"}})
	if inst.MainTabNotes != "on base-index 1" {
		t.Errorf("the main tab at index 1 did not get its note: %+v", inst)
	}
}

// The notes API reads and writes whole notes, every page at once, for the
// session's note and each tab's, and an empty note is an empty list.
func TestTabNotePagesAPI(t *testing.T) {
	app, _, instance := projectTasksApp(t)
	if got, err := app.GetTabNotePages(instance.ID, SessionNotesWindow); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("an empty note = %#v, %v; want an empty list", got, err)
	}
	pages := []session.NotePage{
		{ID: "a", Title: "Plan", Text: "the plan"},
		{ID: "b", Title: "", Text: "untitled"},
	}
	if err := app.SetTabNotePages(instance.ID, SessionNotesWindow, pages, ""); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetTabNotePages(instance.ID, SessionNotesWindow)
	if err != nil || len(got) != 2 || got[0] != pages[0] || got[1] != pages[1] {
		t.Fatalf("session note pages = %+v, %v", got, err)
	}
	if main, _ := app.GetTabNotePages(instance.ID, 0); len(main) != 0 {
		t.Fatalf("writing the session note wrote the main tab's: %+v", main)
	}
	if err := app.SetTabNotePages(instance.ID, SessionNotesWindow, pages, "another-project"); err == nil {
		t.Fatal("a write for another project was accepted")
	}
}

// The view defaults are stored empty for "the one used last", so configs
// written before they existed keep today's behaviour, and only the known fixed
// values are kept — anything else would be a default no view understands.
func TestViewDefaultsRoundTrip(t *testing.T) {
	if got := lastUsedIfUnset(""); got != "last" {
		t.Errorf("an unset default reads as %q, want last", got)
	}
	if got := lastUsedIfUnset("session"); got != "session" {
		t.Errorf("a fixed default reads as %q", got)
	}
	for in, want := range map[string]string{"tab": "tab", "session": "session", "last": "", "": "", "bogus": ""} {
		if got := storedViewDefault(in, "tab", "session"); got != want {
			t.Errorf("storing %q gave %q, want %q", in, got, want)
		}
	}
}
