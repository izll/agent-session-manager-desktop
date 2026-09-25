package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"asmgr-desktop/session"
)

// A YOLO click on a tab turns off whatever makes the tab YOLO, and turns on
// only the tab's own flag. A tab starts with YOLO when either flag is set.
func TestTabYoloToggle(t *testing.T) {
	cases := []struct {
		name                 string
		session, tab         bool
		wantSession, wantTab bool
	}{
		{"off: the tab's own flag is turned on", false, false, false, true},
		{"on by the tab alone: its flag is cleared", false, true, false, false},
		{"on by the session: the session's flag is cleared", true, false, false, false},
		{"on by both: both are cleared", true, true, false, false},
	}
	for _, c := range cases {
		gotSession, gotTab := tabYoloToggle(c.session, c.tab)
		if gotSession != c.wantSession || gotTab != c.wantTab {
			t.Errorf("%s: got session=%t tab=%t, want session=%t tab=%t",
				c.name, gotSession, gotTab, c.wantSession, c.wantTab)
		}
		// The tab's YOLO (either flag) always changes: the button never sticks.
		if (gotSession || gotTab) == (c.session || c.tab) {
			t.Errorf("%s: the tab's YOLO did not change", c.name)
		}
	}
}

// yoloTestApp is an app over a scratch config with one stopped session: a
// Claude main window and a Codex tab at window 2. The multiplexer is a stand-in
// that answers "no such session", so nothing is restarted and no real one is
// asked.
func yoloTestApp(t *testing.T, sessionAutoYes, tabAutoYes bool) (*App, *session.Storage) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in multiplexer is a shell script")
	}
	storage := guardedTestStorage(t)
	bin := t.TempDir()
	writeFakeRecorder(t, bin, "tmux", filepath.Join(bin, "tmux.log"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	inst := &session.Instance{
		ID: "yolo1", Name: "yolo", Path: t.TempDir(), Status: session.StatusStopped,
		Agent: session.AgentClaude, AutoYes: sessionAutoYes,
		FollowedWindows: []session.FollowedWindow{
			{ID: "tab2", Index: 2, Agent: session.AgentCodex, Name: "codex", AutoYes: tabAutoYes},
		},
	}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	return &App{storage: storage, projectLocked: true}, storage
}

func storedYolo(t *testing.T, storage *session.Storage) (sessionAutoYes, tabAutoYes bool) {
	t.Helper()
	inst, err := storage.GetInstance("yolo1")
	if err != nil {
		t.Fatal(err)
	}
	return inst.AutoYes, inst.FollowedWindows[0].AutoYes
}

// The reported bug: a tab with only its own YOLO flag set could not be turned
// off — each click set the SESSION's flag instead, and the tab stayed on.
func TestAYoloClickTurnsOffATabsOwnFlag(t *testing.T) {
	app, storage := yoloTestApp(t, false, true)
	if _, err := app.CycleYoloMode("yolo1", 2, ""); err != nil {
		t.Fatal(err)
	}
	sessionAutoYes, tabAutoYes := storedYolo(t, storage)
	if tabAutoYes || sessionAutoYes {
		t.Fatalf("after the click session=%t tab=%t; the tab is still YOLO", sessionAutoYes, tabAutoYes)
	}
}

// Turning YOLO on for a tab is for that tab: the session's flag, and with it
// the main window and every other tab, is left alone.
func TestAYoloClickOnATabTurnsOnOnlyThatTab(t *testing.T) {
	app, storage := yoloTestApp(t, false, false)
	if _, err := app.CycleYoloMode("yolo1", 2, ""); err != nil {
		t.Fatal(err)
	}
	sessionAutoYes, tabAutoYes := storedYolo(t, storage)
	if !tabAutoYes || sessionAutoYes {
		t.Fatalf("after the click session=%t tab=%t, want only the tab on", sessionAutoYes, tabAutoYes)
	}
}

// A tab that is YOLO through the session's flag is turned off through it.
func TestAYoloClickOnATabClearsTheSessionFlagItInherits(t *testing.T) {
	app, storage := yoloTestApp(t, true, true)
	if _, err := app.CycleYoloMode("yolo1", 2, ""); err != nil {
		t.Fatal(err)
	}
	sessionAutoYes, tabAutoYes := storedYolo(t, storage)
	if tabAutoYes || sessionAutoYes {
		t.Fatalf("after the click session=%t tab=%t; the tab is still YOLO", sessionAutoYes, tabAutoYes)
	}
}

// The main window keeps toggling the session's flag, and leaves the tabs'.
func TestAYoloClickOnTheMainWindowTogglesTheSession(t *testing.T) {
	app, storage := yoloTestApp(t, false, true)
	if _, err := app.CycleYoloMode("yolo1", 0, ""); err != nil {
		t.Fatal(err)
	}
	sessionAutoYes, tabAutoYes := storedYolo(t, storage)
	if !sessionAutoYes || !tabAutoYes {
		t.Fatalf("after the click session=%t tab=%t, want the session on and the tab untouched", sessionAutoYes, tabAutoYes)
	}
}
