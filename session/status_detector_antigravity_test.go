package session

import (
	"context"
	"strings"
	"testing"
)

// All three panes are real captures from agy 1.2.3, trimmed to the lines that
// matter. Antigravity goes through the generic detector, so what these check is
// that the patterns it is given actually match what the agent draws.

// A finished turn: the footer carries the shortcut hint and the model, and
// nothing on screen is a question.
var antigravityIdlePane = `
> Hozz letre egy teszt.txt fajlt a 'hello' tartalommal.

▸ Thought for 30s, 445 tokens

● Edit(/tmp/agtest/teszt.txt) (ctrl+o to expand)

  A teszt.txt fájl sikeresen létrejött a kért hello tartalommal.

` + strings.Repeat("─", 120) + `
>
` + strings.Repeat("─", 120) + `
? for shortcuts                                        Gemini 3.8 Flash · high
`

// A turn in flight. The spinner leads the line and the footer says how to stop.
var antigravityBusyPane = `
> Sorold fel 1-tol 40-ig a szamokat.

⡿  Generating...
` + strings.Repeat("─", 120) + `
>
` + strings.Repeat("─", 120) + `
esc to cancel                                          Gemini 3.8 Flash · high
`

// The permission dialog. Note the footer: "esc to cancel" is on screen here
// too, which is why busy cannot be read from it alone.
var antigravityApprovalPane = `
● Bash(rm -rf /tmp/nemletezo-mappa-teszt-12345) (ctrl+o to expand)

Command
` + strings.Repeat("─", 120) + `

Requesting permission for:
   rm -rf /tmp/nemletezo-mappa-teszt-12345

Run this command?
> 1. Yes, run command
  2. Yes, and always allow in this conversation for commands that start with 'rm -rf'
  4. No, cancel

  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command
esc to cancel                                          Gemini 3.8 Flash · high
`

// The first thing a fresh workspace shows. It blocks before the agent has done
// anything at all, so a session sitting on it is waiting on the user.
var antigravityTrustPane = `
Accessing workspace:

/home/izll/NetBeansProjects/asmgr-desktop

Do you trust the contents of this project?

Antigravity CLI requires permission to read, edit, and execute files here.

> Yes, I trust this folder
  No, exit

  ↑/↓ Navigate · enter Confirm
                                                       Gemini 3.8 Flash · high
`

func TestAntigravityApprovalPromptIsWaiting(t *testing.T) {
	lines := strings.Split(antigravityApprovalPane, "\n")
	got := detectGenericActivityContext(context.Background(), lines,
		agentPatterns[AgentAntigravity], "")
	if got != ActivityWaiting {
		t.Errorf("a permission dialog was detected as %v, want waiting", got)
	}
}

// The trust prompt is the one a new user hits first, and the one most likely to
// sit unnoticed: nothing has happened yet, so an idle-looking session is
// exactly what it is not.
func TestAntigravityTrustPromptIsWaiting(t *testing.T) {
	lines := strings.Split(antigravityTrustPane, "\n")
	got := detectGenericActivityContext(context.Background(), lines,
		agentPatterns[AgentAntigravity], "")
	if got != ActivityWaiting {
		t.Errorf("the trust prompt was detected as %v, want waiting", got)
	}
}

// Waiting has to win over the spinner. "esc to cancel" is in the footer under
// the permission dialog too, so reading busy from the footer alone would hold a
// session on busy while it was blocked on a question.
func TestAntigravityApprovalOutranksTheBusyFooter(t *testing.T) {
	if !strings.Contains(antigravityApprovalPane, "esc to cancel") {
		t.Fatal("this fixture was meant to carry the busy footer as well")
	}
	lines := strings.Split(antigravityApprovalPane, "\n")
	if got := detectGenericActivityContext(context.Background(), lines,
		agentPatterns[AgentAntigravity], ""); got == ActivityBusy {
		t.Error("the footer's cancel hint outranked the question the agent is asking")
	}
}

// Antigravity spins through the denser half of the braille block. None of those
// eight was in the default set, and listing them agent by agent is what this
// stopped doing: a frame nobody wrote down made a working session read as idle.
func TestAntigravitySpinnerIsRecognised(t *testing.T) {
	for _, glyph := range []string{"⡿", "⢿", "⣟", "⣯", "⣷", "⣻", "⣽", "⣾"} {
		line := glyph + "  Generating..."
		if findSpinnerLine([]string{line}, antigravitySpinners, 15) == "" {
			t.Errorf("%q is not seen as a spinner, so that frame reads as idle", glyph)
		}
	}
}

// A finished turn must not read as a question. "No, cancel" is a waiting
// pattern and the word "cancel" alone is not: the footer says "esc to cancel"
// on every pane, idle included.
func TestAntigravityIdleIsNotWaiting(t *testing.T) {
	lines := strings.Split(antigravityIdlePane, "\n")
	if got := detectGenericActivityContext(context.Background(), lines,
		agentPatterns[AgentAntigravity], ""); got == ActivityWaiting {
		t.Error("a finished turn was taken for a question")
	}
}

func TestAntigravityBusyPaneIsNotWaiting(t *testing.T) {
	lines := strings.Split(antigravityBusyPane, "\n")
	if got := detectGenericActivityContext(context.Background(), lines,
		agentPatterns[AgentAntigravity], ""); got == ActivityWaiting {
		t.Error("a running turn was taken for a question")
	}
}
