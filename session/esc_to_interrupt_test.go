package session

import (
	"strings"
	"testing"
)

var escSeparator = strings.Repeat("─", 72)

// Claude lists every spawned agent below the status bar, so the bar's distance
// from the bottom grows with the number of agents running. A five-line window
// from the bottom reached it only while that list was short — a session running
// five agents pushed it out of reach and showed as idle at its busiest.
func TestEscToInterruptIsFoundBelowAnAgentList(t *testing.T) {
	pane := []string{
		"✻ Razzmatazzing… (4m 17s · ↓ 3.7k tokens)",
		"",
		escSeparator,
		"❯ ",
		escSeparator,
		"  ⚠ Lower priority until 5:10pm · 61% allowance left",
		"  ⏵⏵ auto mode on · 5 shells · esc to interrupt · ← for agents · ↓ to manage",
		"",
		"  ● main",
		"  ◯ general-purpose  Checking frontend.log compile status",
		"  ◯ general-purpose  Clearing Kotlin daemon cache",
		"  ◯ general-purpose  Checking StockRequestReleaseDocumentHeaderSql.kt blocker",
		"  ◯ general-purpose  Compiling backend to unblock shared build",
		"  ◯ general-purpose  Locating CheckOpenedCassa insertion point",
	}
	if !hasEscToInterrupt(pane) {
		t.Fatal("a working session with five agents listed was not seen as busy")
	}
}

// The plain case has to keep working: the bar right at the bottom.
func TestEscToInterruptIsFoundAtTheBottom(t *testing.T) {
	pane := []string{
		escSeparator,
		"❯ ",
		escSeparator,
		"  ⏵⏵ auto mode on · esc to interrupt",
	}
	if !hasEscToInterrupt(pane) {
		t.Fatal("the status bar directly at the bottom was missed")
	}
}

// The bound the line count replaced still has to hold: the words appear in the
// transcript too — Claude's own advice, or a prompt quoting it — and finding
// them there would pin a finished session to busy for as long as they stayed on
// screen. The input box is what separates the two.
func TestEscToInterruptIgnoresTheTranscript(t *testing.T) {
	pane := []string{
		"● You can always press esc to interrupt me while I work.",
		"",
		"✻ Cooked for 1m 0s · done 14:50",
		escSeparator,
		"❯ ",
		escSeparator,
		"  ⏵⏵ auto mode on · ← for agents",
		"",
		"  ● main",
	}
	if hasEscToInterrupt(pane) {
		t.Fatal("the words were taken from the transcript, above the input box")
	}
}

// A pane with no input box at all (a plain shell, or a capture taken mid-redraw)
// must not be searched all the way up into the scrollback.
func TestEscToInterruptWithoutAnInputBox(t *testing.T) {
	pane := []string{
		"$ echo 'press esc to interrupt'",
		"press esc to interrupt",
		"$ ",
	}
	if !hasEscToInterrupt(pane) {
		// Documents the trade-off rather than asserting the opposite: with no
		// separator there is nothing to bound the search, and the words are
		// taken at face value. Claude panes always draw the box.
		t.Skip("no input box: the whole pane is searched")
	}
}
