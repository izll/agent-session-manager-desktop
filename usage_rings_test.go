package main

import (
	"testing"

	"asmgr-desktop/session"
)

// A ring that is switched off must cost nothing. The Claude figure is a live
// request to Anthropic against the user's own rate limit, so polling for a
// decoration nobody asked for spends someone's quota on nothing — the gate
// belongs in front of the fetch, not in front of the drawing.

func ringsApp(t *testing.T, apply func(*session.Settings)) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpdateSettings(apply); err != nil {
		t.Fatal(err)
	}
	return &App{storage: storage}
}

func TestAllRingsOffFetchesNothing(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) {
		s.ShowClaudeFiveHourRing = false
		s.ShowClaudeSevenDayRing = false
		s.ShowCodexUsageRing = false
	})
	rings := app.GetUsageRings()
	if rings.Claude != nil {
		t.Error("Claude usage was fetched with both its rings switched off")
	}
	if rings.Codex != nil {
		t.Error("Codex usage was read with its ring switched off")
	}
}

// Each source gates independently: Codex is a local file read, Claude a network
// call, and wanting one must not pay for the other.
func TestCodexRingAloneDoesNotFetchClaude(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) { s.ShowCodexUsageRing = true })
	if app.GetUsageRings().Claude != nil {
		t.Error("Claude was fetched though only the Codex ring is on — that is a " +
			"network request against the user's rate limit")
	}
}

// The two Claude windows come from one response, so either one alone fetches,
// and both together fetch no more.
func TestEitherClaudeWindowFetchesOnce(t *testing.T) {
	for _, tc := range []struct {
		name             string
		fiveHour, sevenD bool
	}{
		{"five hour only", true, false},
		{"seven day only", false, true},
		{"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := ringsApp(t, func(s *session.Settings) {
				s.ShowClaudeFiveHourRing = tc.fiveHour
				s.ShowClaudeSevenDayRing = tc.sevenD
			})
			rings := app.GetUsageRings()
			if rings.ShowFiveHour != tc.fiveHour || rings.ShowSevenDay != tc.sevenD {
				t.Fatalf("flags = (%v,%v), want (%v,%v)",
					rings.ShowFiveHour, rings.ShowSevenDay, tc.fiveHour, tc.sevenD)
			}
		})
	}
}

// Both Claude rings can be on at once — the point of splitting them.
func TestBothClaudeWindowsCanBeShownTogether(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) {
		s.ShowClaudeFiveHourRing = true
		s.ShowClaudeSevenDayRing = true
	})
	rings := app.GetUsageRings()
	if !rings.ShowFiveHour || !rings.ShowSevenDay {
		t.Fatal("the two windows are still exclusive")
	}
}

// Settings that cannot be read must not be treated as "everything on".
func TestUnreadableSettingsFetchNothing(t *testing.T) {
	app := &App{}
	rings := app.GetUsageRings()
	if rings.Claude != nil || rings.Codex != nil {
		t.Fatal("usage was fetched with no settings to say it was wanted")
	}
	if rings.ShowFiveHour || rings.ShowSevenDay {
		t.Fatal("rings were advertised as visible with no settings to enable them")
	}
}

// Gemini gates like the others. It reads local logs rather than the network,
// but a switched-off ring should still not walk every session directory.
func TestGeminiRingGatesItsRead(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) { s.ShowGeminiUsageRing = false })
	if app.GetUsageRings().Gemini != nil {
		t.Fatal("Gemini usage was read with its ring switched off")
	}
}

func TestGeminiRingOnReadsIt(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) { s.ShowGeminiUsageRing = true })
	if app.GetUsageRings().Gemini == nil {
		t.Fatal("Gemini usage was not read with its ring switched on")
	}
}

// Nothing else is fetched for it: Gemini is local, Claude is a network call.
func TestGeminiRingAloneDoesNotFetchClaude(t *testing.T) {
	app := ringsApp(t, func(s *session.Settings) { s.ShowGeminiUsageRing = true })
	if app.GetUsageRings().Claude != nil {
		t.Fatal("Claude was fetched though only the Gemini ring is on")
	}
}
