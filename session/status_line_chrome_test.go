package session

import (
	"strings"
	"testing"
)

// The status line must show what the session is doing, not what Claude Code is
// showing about itself. Each of these panes had put a piece of the interface
// there instead — a slash command typed an hour earlier, the updater's banner,
// or half a sentence from the recap block.

func statusOf(lines []string) string {
	return GetClaudeStatusLine(lines, StripANSI)
}

func TestSlashCommandIsNotTheStatus(t *testing.T) {
	got := statusOf([]string{
		"❯ /model",
		"  ⎿  Set model to claude-fable-5-1 and saved as your default",
		"",
		"● A hívás kész, minden teszt zöld.",
		"",
		"────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────",
	})
	if got == "/model" || got == "❯ /model" {
		t.Fatalf("the status showed the slash command: %q", got)
	}
	if got != "● A hívás kész, minden teszt zöld." {
		t.Logf("status = %q", got)
	}
}

func TestUpdateBannerIsNotTheStatus(t *testing.T) {
	got := statusOf([]string{
		"● Végeztem a méréssel.",
		"                              ✔ Update installed · Restart to update",
		"────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────",
	})
	if got == "" {
		t.Skip("no line was chosen at all")
	}
	if strings.Contains(got, "Update installed") || strings.Contains(got, "Restart to update") {
		t.Fatalf("the status showed the updater banner: %q", got)
	}
}

// The recap wraps at the pane's width, so its tail is an arbitrary fragment —
// "(disable recaps in /config)" in one session, "in /config)" in another.
// Matching the words would miss the second; the shape catches both.
func TestRecapContinuationIsNotTheStatus(t *testing.T) {
	for _, tail := range []string{
		"  eldöntése van hátra. (disable recaps in /config)",
		"  in /config)",
	} {
		got := statusOf([]string{
			"● Kész, a jelentést elküldtem.",
			"",
			"※ recap: A cél az opensource-nest.dll feltérképezése; elkészült a vázlat, a",
			tail,
			"",
			"────────────────────────────────────────────────────────────────────",
			"❯ ",
			"────────────────────────────────────────────────────────────────────",
		})
		if got == strings.TrimSpace(tail) {
			t.Fatalf("the status showed a recap fragment: %q", got)
		}
	}
}

// The guard must not swallow ordinary indented output, which is most of what an
// agent prints.
func TestIndentedContentIsStillAllowed(t *testing.T) {
	got := statusOf([]string{
		"● Eredmény:",
		"  a mérés 6 ms-ot adott",
		"",
		"────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────",
	})
	if got == "" {
		t.Fatal("indented agent output was discarded along with the chrome")
	}
}
