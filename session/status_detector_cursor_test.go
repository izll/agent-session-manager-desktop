package session

import (
	"context"
	"strings"
	"testing"
)

// cursorIdlePane is a real capture from a finished Cursor turn, trimmed to the
// lines that matter. The Go listing at the top is the point: it was on screen
// because the session had just been asked about these very patterns, and the
// detector — reading the last fifteen lines with no boundary — matched
// "allow once" inside it and reported the session as waiting for an answer.
var cursorIdlePane = `
	WaitingPatterns: []string{
		"allow once",
		"do you want to proceed",

● Folyamatban van a Cursor agent integráció — uncommitted, masteren.

  Még nincs commit. Commitoljam, vagy valami mást nézzünk?

 ` + strings.Repeat("▄", 60) + `
  → Add a follow-up
 ` + strings.Repeat("▀", 60) + `
  Auto · 6.1%                                        Run Everything
  ~/NetBeansProjects/asmgr-desktop · master
`

// cursorBusyPane is a turn in flight: the spinner line leads with U+2800, the
// blank braille cell, and the footer says how to stop it.
var cursorBusyPane = `
● Megnézem a státusz-detektálást.

⠀⠞ Thinking  28 tokens

 ` + strings.Repeat("▄", 60) + `
  → Add a follow-up
 ` + strings.Repeat("▀", 60) + `
  Auto · Run Everything                                ctrl+c to stop
`

// cursorApprovalPane is a real approval prompt, captured from a running
// session. Two things about it drove the code: the dialog is introduced by a
// plain ─ rule with no box around it — the half blocks that bound the input box
// are nowhere on screen — and "Run Everything" appears among the answers, which
// is why that phrase cannot be used to recognise a prompt.
var cursorApprovalPane = `
● Ellenőrzés: új Agent session, /run-everything off, majd kérj egy rm-et.

  most próba: rm /tmp/tisza-deploy17.tar.gz

  $ rm /tmp/tisza-deploy17.tar.gz Waiting for approval...

` + strings.Repeat("─", 120) + `
 $  rm /tmp/tisza-deploy17.tar.gz in .

 Run this command?
 Not in allowlist: rm
  → Run (once) (y)
    Add Shell(rm) to allowlist? (tab)
    Run Everything (shift+tab)
    Skip & tell the agent what to do instead (esc or n)
`

func TestCursorApprovalPromptIsWaiting(t *testing.T) {
	lines := strings.Split(cursorApprovalPane, "\n")
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got != ActivityWaiting {
		t.Errorf("a Cursor approval prompt was detected as %v, want waiting", got)
	}
}

// The dialog carries no half blocks at all, so a boundary looking only for the
// input box finds nothing and the search falls back over the whole transcript.
func TestCursorApprovalDialogIsBounded(t *testing.T) {
	lines := strings.Split(cursorApprovalPane, "\n")
	if cursorInputBoxTop(lines) == 0 {
		t.Error("the ─ rule above the dialog is not recognised as a boundary, so the " +
			"whole transcript is searched for prompts")
	}
}

func TestCursorTranscriptIsNotAPrompt(t *testing.T) {
	lines := strings.Split(cursorIdlePane, "\n")
	got := detectCursorActivity(lines, agentPatterns[AgentCursor], "")
	if got == ActivityWaiting {
		t.Error("a source listing in the transcript was read as an approval prompt: " +
			"the search is not bounded by the input box")
	}
	if got != ActivityIdle {
		t.Errorf("a finished Cursor turn was detected as %v, want idle", got)
	}
}

// The footer answers "is a turn in flight" without waiting for a second capture
// to prove the spinner is animating.
func TestCursorWorkingIsBusy(t *testing.T) {
	lines := strings.Split(cursorBusyPane, "\n")
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got != ActivityBusy {
		t.Errorf("a working Cursor was detected as %v, want busy", got)
	}
}

// "Run Everything" is the footer's standing mode label, present while the agent
// sits idle — a waiting pattern matching it would pin every idle session to
// waiting, which is the same bug from the other side.
func TestCursorModeLabelIsNotAPrompt(t *testing.T) {
	for _, pattern := range agentPatterns[AgentCursor].WaitingPatterns {
		if strings.Contains("auto · 6.1%                run everything", pattern) {
			t.Errorf("waiting pattern %q matches the idle footer, so an idle "+
				"session reports as waiting", pattern)
		}
	}
}

// The spinner line begins with the blank braille cell, not with the animating
// glyph. Matching on a prefix with the default set found nothing, which is why
// a working Cursor could never be seen as busy through the spinner.
func TestCursorSpinnerLineIsFound(t *testing.T) {
	line := "⠀⠞ Thinking  28 tokens"
	if findSpinnerLine([]string{line}, defaultSpinners, 15) != "" {
		t.Error("the default set now matches; cursorSpinners may be redundant")
	}
	if findSpinnerLine([]string{line}, cursorSpinners, 15) == "" {
		t.Error("Cursor's own spinner set does not match its Thinking line, so the " +
			"spinner can never report busy")
	}
}

// The boundary has to come from the box on screen now, not from one scrolled up
// in the transcript: an old box would put the search back over the transcript.
func TestCursorInputBoxTopFindsTheCurrentBox(t *testing.T) {
	lines := strings.Split(cursorIdlePane, "\n")
	top := cursorInputBoxTop(lines)
	if top == 0 {
		t.Fatal("the input box was not found, so the whole pane is searched")
	}
	for j := top; j < len(lines); j++ {
		if strings.Contains(lines[j], "allow once") {
			t.Error("the boundary sits above the transcript, which it was meant to exclude")
		}
	}
}

// A pane with no box at all — a capture taken mid-redraw — must not crash or
// silently search nothing.
func TestCursorWithoutAnInputBox(t *testing.T) {
	lines := []string{"$ cursor-agent", "starting…"}
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got != ActivityIdle {
		t.Errorf("a pane with no input box was detected as %v, want idle", got)
	}
}

// cursorSpinnerAbovePane is what the user photographed: a turn in flight, the
// spinner drawn above the input box, and no "ctrl+c to stop" anywhere on
// screen. Every character of "⠴ Exploring 205s" was already in the spinner set
// — the line was simply never looked at, because the search ran from the input
// box downwards.
var cursorSpinnerAbovePane = `
● Megnézem a státusz-detektálást.

⠴ Exploring 205s

 ` + strings.Repeat("▄", 60) + `
  → Add a follow-up
 ` + strings.Repeat("▀", 60) + `
  Auto · 6.2%
  ~/NetBeansProjects/asmgr-desktop · master
`

func TestCursorSpinnerAboveTheInputBoxIsBusy(t *testing.T) {
	lines := strings.Split(cursorSpinnerAbovePane, "\n")
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got != ActivityBusy {
		t.Errorf("a spinner above the input box was detected as %v, want busy", got)
	}
}

// The counter in "Exploring 205s" ticks once a second, so two captures 60ms
// apart usually show the same line. Waiting for the spinner to change before
// believing it is the other half of why this pane read as idle.
func TestCursorSpinnerNeedsNoAnimation(t *testing.T) {
	lines := strings.Split(cursorSpinnerAbovePane, "\n")
	// An empty target makes any re-capture fail, which is what the animation
	// check does when it cannot prove movement: it gives up and says no.
	if got := detectCursorActivityContext(context.Background(), lines,
		agentPatterns[AgentCursor], ""); got != ActivityBusy {
		t.Errorf("the spinner only counts when it can be caught moving: got %v", got)
	}
}

// Cursor draws no input box while it is starting up. Slicing the search to
// "above the box" has to fall back to the whole capture there, or the one
// moment with nothing but a spinner to go on reports idle.
func TestCursorSpinnerWithNoInputBoxIsBusy(t *testing.T) {
	lines := strings.Split("● Indulás\n\n⠴ Exploring 205s\n", "\n")
	if cursorInputBoxTop(lines) != 0 {
		t.Fatal("this pane was meant to have no input box")
	}
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got != ActivityBusy {
		t.Errorf("a spinner with no input box was detected as %v, want busy", got)
	}
}

// The transcript is not the screen: a braille character quoted in it — a table,
// a spinner someone pasted — must not hold the session on busy. Only the few
// lines immediately above the box count.
func TestCursorBrailleInTheTranscriptIsNotASpinner(t *testing.T) {
	pane := `
● Itt egy példa a dokumentációból:

    ⠴ Exploring 205s

  Ez csak idézet volt. Kérdezz bátran!

  Nincs más dolgom.

 ` + strings.Repeat("▄", 60) + `
  → Add a follow-up
 ` + strings.Repeat("▀", 60) + `
  Auto · 6.2%
`
	lines := strings.Split(pane, "\n")
	if got := detectCursorActivity(lines, agentPatterns[AgentCursor], ""); got == ActivityBusy {
		t.Error("a braille character quoted in the transcript was read as a live spinner")
	}
}
