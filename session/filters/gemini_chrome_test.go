package filters

import (
	"strings"
	"testing"
)

// Gemini draws its input box with half-block characters rather than the light
// box-drawing set the filters knew about. A row of ▄ or ▀ is a border, and the
// sidebar was showing one as though the session had said it — a bar of blocks
// where the status should be.
func TestGeminiChromeIsNotContent(t *testing.T) {
	cfg, ok := LoadFilters()["gemini"]
	if !ok {
		t.Fatal("the gemini filter is gone")
	}

	chrome := []string{
		strings.Repeat("▄", 120),
		strings.Repeat("▀", 120),
		"workspace (/directory)   branch   sandbox   /model",
		"? for shortcuts",
		"Shift+Tab to accept edits",
		">   Type your message or @path/to/file",
		"~/NetBeansProjects/asmgr-desktop   master   no sandbox   Auto",
	}
	for _, line := range chrome {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); !skip {
			t.Errorf("interface furniture was taken for content: %.60q", line)
		}
	}
}

// And the guard must not swallow what the session actually says — an error the
// user needs to see least of all.
func TestGeminiContentSurvivesTheFilter(t *testing.T) {
	cfg, ok := LoadFilters()["gemini"]
	if !ok {
		t.Fatal("the gemini filter is gone")
	}
	content := []string{
		"[API Error: You have exhausted your daily quota on this model.]",
		"Found 3 matches",
		"Read lines 1-100 of 173 from dictation/punctuation_commands.json",
	}
	for _, line := range content {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); skip {
			t.Errorf("real output was filtered away: %.60q", line)
		}
	}
}
