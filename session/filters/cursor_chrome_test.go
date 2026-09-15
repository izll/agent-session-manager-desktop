package filters

import (
	"strings"
	"testing"
)

// The first cursor filter was copied from Codex's entry and matched almost
// nothing on a real pane: of everything Cursor draws, only the ─ rule was
// skipped, so the sidebar showed the input box's own top border — a bar of
// half blocks — where the session's last words belonged.
func TestCursorChromeIsNotContent(t *testing.T) {
	cfg, ok := LoadFilters()["cursor"]
	if !ok {
		t.Fatal("the cursor filter is gone")
	}

	chrome := []string{
		// The input box: half blocks, like Gemini's, not the light box-drawing
		// set the filter was originally written for.
		strings.Repeat("▄", 120),
		strings.Repeat("▀", 120),
		// The line inside the box. Matching the placeholder's words was not
		// enough: it is only there while the box is empty, and after the first
		// keystroke the line carries whatever is half-written — which then
		// stood on the tab as though the agent had said it. Hence the arrow.
		"→ Add a follow-up",
		"→ Plan, search, build anything",
		"→ ez itt egy felig begepelt mondat",
		"→ commitold be",
		// The footer. "Auto" is the model, on a line of its own — which is why
		// the bar below it carries two dot-separated fields and not Codex's
		// three.
		"Auto · 6.1%                                        Run Everything",
		// The same footer without the mode label beside it. Skipping the bare
		// "Auto" as an exact line was not enough: Cursor appends the context
		// percentage or the mode whenever it has one, and those went to the tab.
		"Auto",
		"Auto · 6.1%",
		"Auto · Plan Mode",
		"Auto · Ask",
		// The banner, on screen for the first seconds of a fresh agent.
		"Cursor Agent",
		"~/NetBeansProjects/asmgr-desktop · master",
		// Busy footer: Cursor words the interrupt hint its own way.
		"Auto · Run Everything                                ctrl+c to stop",
	}
	for _, line := range chrome {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); !skip {
			t.Errorf("interface furniture was taken for content: %.60q", line)
		}
	}
}

// And the filter must not swallow what the session actually says. The path bar
// is skipped on two fields now rather than three, which is the change most
// likely to take real output with it.
func TestCursorContentSurvivesTheFilter(t *testing.T) {
	cfg, ok := LoadFilters()["cursor"]
	if !ok {
		t.Fatal("the cursor filter is gone")
	}
	content := []string{
		"● Folyamatban van a Cursor agent integráció — uncommitted, masteren.",
		"Not in allowlist: rm",
		"Run this command?",
		// Prose that happens to carry a dot separator but names no path.
		"Done · 3 files changed",
		// "Auto" is skipped as an exact line, not as a prefix: as a prefix it
		// took real sentences with it, and Hungarian output starts with those
		// four letters often enough to notice.
		"Autofocus javítva a NewSessionDialog-ban",
		"Automatikusan generált teszt hozzáadva",
		"⠀⠞ Thinking  28 tokens",
	}
	for _, line := range content {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); skip {
			t.Errorf("real output was filtered away: %.60q", line)
		}
	}
}
