package filters

import (
	"strings"
	"testing"
)

// Every line here was on screen in a running agy 1.2.3. The sidebar shows a
// session's last words, so anything the interface draws for itself has to be
// skipped or it stands there instead.
func TestAntigravityChromeIsNotContent(t *testing.T) {
	cfg, ok := LoadFilters()["antigravity"]
	if !ok {
		t.Fatal("the antigravity filter is gone")
	}

	chrome := []string{
		// The footer, idle and busy. Two labels with a wide gap: the hint on
		// the left, the model on the right.
		"? for shortcuts                                        Gemini 3.8 Flash · high",
		"esc to cancel                                          Gemini 3.8 Flash · high",
		// The input box, drawn in light rules.
		strings.Repeat("─", 120),
		// The input line itself, empty and typed into.
		">",
		"> Sorold fel 1-tol 40-ig a szamokat.",
		// The collapsed thinking summary and a tool call.
		"▸ Thought for 30s, 445 tokens",
		"● Edit(/tmp/agtest/teszt.txt) (ctrl+o to expand)",
		"● Bash(rm -rf /tmp/x) (ctrl+o to expand)",
		// The permission dialog's furniture.
		"Command",
		"Requesting permission for:",
		"  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command",
		// The startup banner, half blocks, on screen for the first seconds.
		"      ▄▀▀▄        Antigravity CLI 1.2.3",
		"     ▀▀▀▀▀▀       antalizn@gmail.com (Google AI Plus)",
		"Antigravity CLI",
	}
	for _, line := range chrome {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); !skip {
			t.Errorf("interface furniture was taken for content: %.70q", line)
		}
	}
}

// And it must not swallow what the session actually says.
func TestAntigravityContentSurvivesTheFilter(t *testing.T) {
	cfg, ok := LoadFilters()["antigravity"]
	if !ok {
		t.Fatal("the antigravity filter is gone")
	}
	content := []string{
		"A teszt.txt fájl sikeresen létrejött a kért hello tartalommal.",
		"The request is to create a file with specific content in a designated location.",
		// Prose carrying a dot separator but naming no path.
		"Kész · 3 fájl módosult",
		// The question itself is content: it is what the sidebar should show
		// when a session is waiting on an answer.
		"Run this command?",
		"Do you trust the contents of this project?",
		// Sentences that merely begin with words the filter looks for.
		"Commandó egység érkezett",
		"Thoughtworks a cég neve",
	}
	for _, line := range content {
		if skip, _ := ApplyFilter(cfg, strings.TrimSpace(line)); skip {
			t.Errorf("real output was filtered away: %.70q", line)
		}
	}
}
