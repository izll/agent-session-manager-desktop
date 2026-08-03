package main

import (
	"os"
	"strings"
	"testing"
)

// The session selected at shutdown is reopened on the next launch.
//
// The tab within a session was already remembered (Instance.LastWindowIndex),
// which meant a launch landed on the right tab of the wrong session — the half
// that was missing is the session itself.
func TestLastSessionIsPersistedAndRestored(t *testing.T) {
	// Stored alongside the other selection state rather than on an instance:
	// it identifies which instance, so it cannot live on one.
	storage, err := os.ReadFile("session/storage.go")
	if err != nil {
		t.Fatalf("reading storage.go: %v", err)
	}
	if !strings.Contains(string(storage), "LastSessionID") {
		t.Error("settings storage has no LastSessionID, so the selected session " +
			"cannot survive a restart")
	}

	// Carried through the API struct in both directions; a field that is read
	// but never written back is silently lost on the next save.
	app, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("reading app.go: %v", err)
	}
	src := string(app)
	if strings.Count(src, "LastSessionID") < 3 {
		t.Error("LastSessionID does not appear in the settings struct, the read and " +
			"the write; one of those missing means it is dropped on the next save")
	}
	// Restoring is opt-in, so the switch has to travel the same three points.
	// A setting that saves but never loads reads as the toggle not working.
	if strings.Count(src, "RestoreLastSession") < 3 {
		t.Error("RestoreLastSession does not appear in the settings struct, the read " +
			"and the write; the toggle would not survive a restart")
	}
}

// The dashboard stays the default. Landing straight in a session is a
// preference, not an obvious improvement — someone who works across several
// projects wants the overview first.
func TestRestoringLastSessionIsOptIn(t *testing.T) {
	b, err := os.ReadFile("frontend/src/lib/stores/settings.ts")
	if err != nil {
		t.Fatalf("reading settings.ts: %v", err)
	}
	if !strings.Contains(string(b), "restoreLastSession: false") {
		t.Error("restoreLastSession does not default to false, so a fresh install " +
			"would skip the dashboard without being asked")
	}
}
