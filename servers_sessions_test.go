package main

import (
	"os"
	"strings"
	"testing"
)

// A server is shared: it can hold sessions from this computer, from another of
// the user's machines, and ones started by hand. They all look alike in a
// listing — a name and a window count — so what the app creates is tagged with
// the machine, project and path behind it.
//
// The tags are multiplexer user options rather than a file on the server:
// they live and die with the session, need no cleanup, and every session's
// tags come back in the one listing command.
func TestSessionsOnAServerCarryTheirOwnership(t *testing.T) {
	source, err := os.ReadFile("session/instance.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	for _, option := range []string{
		"@asmgr_owner", "@asmgr_project", "@asmgr_path", "@asmgr_agent",
	} {
		if !strings.Contains(text, option) {
			t.Errorf("sessions no longer record %s, so a session on a server "+
				"cannot say where it came from", option)
		}
	}

	// Written when the session is created, or a session made before the app
	// restarts would never be tagged at all.
	at := strings.Index(text, "func (i *Instance) ensureRemoteSessionFor(")
	if at < 0 {
		t.Fatal("remote session creation is gone; this test needs rewriting")
	}
	end := strings.Index(text[at:], "\n}\n")
	if !strings.Contains(text[at:at+end], "tagRemoteSession") {
		t.Error("a session created on a server is not tagged")
	}
}

// The listing asks for everything in one command. A round trip per session
// would be slowest on exactly the server that has the most of them.
func TestTheServerListingIsOneCommand(t *testing.T) {
	source, err := os.ReadFile("servers_api.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	at := strings.Index(text, "func (a *App) ListServerSessions(")
	if at < 0 {
		t.Fatal("ListServerSessions is gone; this test needs rewriting")
	}
	end := strings.Index(text[at:], "\n}\n")
	body := text[at : at+end]

	if strings.Count(body, "executor.Output(") != 1 {
		t.Error("the listing no longer takes a single command")
	}
	// tmux 2.6 — what the servers people have tend to run — leaves
	// session_created_string empty, so the timestamp is the only reliable
	// field. Measured on a live 2.6 server.
	if strings.Contains(body, "session_created_string") {
		t.Error("the listing asks for a formatted date that tmux 2.6 leaves empty")
	}
}

// A session the app does not know about must not be presented as the user's
// own work, and one this machine made but lost track of must be visible as
// exactly that.
func TestForeignSessionsAreDistinguishable(t *testing.T) {
	source, err := os.ReadFile("servers_api.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	for _, field := range []string{"Ours", "ThisMachine", "View"} {
		if !strings.Contains(text, field) {
			t.Errorf("the listing no longer reports %s, so a session from "+
				"elsewhere cannot be told from one of ours", field)
		}
	}
}

// A view session is described by the tab it shows, by name.
//
// The window index is an internal number: remote tabs are numbered from 100 so
// they cannot collide with local ones, and that number appears nowhere else in
// the interface. Shown in a list it explains nothing — "view of tab 100" is a
// riddle, "view of claude tab" is an answer.
func TestAViewIsDescribedByTheTabItShows(t *testing.T) {
	source, err := os.ReadFile("servers_api.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	at := strings.Index(text, "func (a *App) viewTabNames(")
	if at < 0 {
		t.Fatal("views are no longer resolved to tab names; the raw index will " +
			"show up in the list again")
	}
	end := strings.Index(text[at:], "\n}\n")
	body := text[at : at+end]

	if !strings.Contains(body, "window_name") {
		t.Error("the tab's name is not read; only its index is available")
	}
	// One command for every session: a round trip per view would be slowest on
	// the server with the most of them, and this runs while a dialog waits.
	if strings.Count(body, "executor.Output(") != 1 {
		t.Error("resolving names no longer takes a single command")
	}
	// The placeholder window a view is created with is not what the view shows.
	if !strings.Contains(body, `fields[1]) == "0"`) {
		t.Error("the view's own placeholder window can be taken for the tab")
	}
}

// A view says which session it belongs to, not just which tab.
//
// A server can hold several sessions, and a tab called "Terminal" appears in
// most of them — the name alone does not say whose it is.
func TestAViewNamesTheSessionBehindIt(t *testing.T) {
	// The shape the app creates: asmgr_view_<session>_<window index>. Session
	// names carry underscores of their own, so only the trailing number may be
	// cut off.
	if got := baseSessionOf("asmgr_view_asm_claude_my_project_1768202100_100"); got != "asm_claude_my_project_1768202100" {
		t.Errorf("base session = %q", got)
	}

	// Not a view at all.
	if got := baseSessionOf("asm_claude_my_project_1768202100"); got != "" {
		t.Errorf("a plain session was read as a view: %q", got)
	}
	// A view-looking name with no index: refused rather than guessed at, since
	// cutting at the wrong underscore would invent a session that never
	// existed.
	if got := baseSessionOf("asmgr_view_something"); got != "" {
		t.Errorf("a name with no window index produced %q", got)
	}
	if got := baseSessionOf("asmgr_view_session_"); got != "" {
		t.Errorf("a trailing underscore produced %q", got)
	}
}

// A session's readable name, for the sessions that predate tagging.
//
// The app builds session ids as "asm_<agent>_<name>_<timestamp>", so the name
// the user gave is in there — and those untagged sessions are exactly the ones
// on a server right now. Showing the raw id instead filled the whole row and
// pushed out what the row was for.
func TestASessionsNameIsRecoveredFromItsID(t *testing.T) {
	cases := map[string]string{
		"asm_claude_asmgr-desktop_1768202100175893157":   "asmgr-desktop",
		"asm_codex_my_project_name_1768202100175893157":  "my_project_name",
		// Not one of ours: whatever it is called is the answer.
		"kezi-munka": "kezi-munka",
		// Our prefix but not our shape — no timestamp to cut at, so nothing is
		// assumed.
		"asm_claude_x": "asm_claude_x",
		"asm_claude_":  "asm_claude_",
	}
	for id, want := range cases {
		if got := sessionDisplayName(id, ""); got != want {
			t.Errorf("sessionDisplayName(%q) = %q, want %q", id, got, want)
		}
	}
}

// A tag always wins: it is what the user called the session, recorded at the
// time, rather than something reconstructed from a name.
func TestATagBeatsTheRecoveredName(t *testing.T) {
	got := sessionDisplayName("asm_claude_old-name_1768202100175893157", "Renamed Project")
	if got != "Renamed Project" {
		t.Errorf("got %q, want the tag", got)
	}
}
