package session

import (
	"encoding/json"
	"testing"
)

// setPatternsForTest installs a pattern set for the duration of one test and
// restores whatever was there before, so tests that change the configuration
// cannot leak into the ones that do not.
func setPatternsForTest(t *testing.T, p *patternsFile) {
	t.Helper()
	patternsMu.Lock()
	previous := loadedPatterns
	loadedPatterns = p
	patternsMu.Unlock()
	// currentPatterns() runs the once-loader; make sure it has already fired so
	// it cannot overwrite what this test just installed.
	patternsOnce.Do(func() {})
	t.Cleanup(func() {
		patternsMu.Lock()
		loadedPatterns = previous
		patternsMu.Unlock()
	})
}

// The pane as Claude Code actually draws it while a spawned agent works: the
// main thread has finished, so there is no "esc to interrupt", no spinner, and
// no thinking line with an ellipsis. The one thing that says otherwise sits
// above the input separator, where the thinking check stops looking.
//
// Captured from a real session that had been running an agent for over an hour
// while the app showed it idle.
var backgroundAgentPane = []string{
	"❯ /model",
	"  ⎿  Set model to claude-fable-5-1 and saved as your default for new sessions",
	"",
	"✻ Claude resuming /loop wakeup (Sep 2 8:40am)",
	"",
	"● A v14 (két-geometriás passz) még dolgozik.",
	"",
	"● Csendben várom a v14 eredményét — ha megjönnek a számok, azonnal hozom őket.",
	"",
	"✻ Waiting for 1 background agent to finish",
	"",
	"────────────────────────────────────────────────────────────────────────",
	"❯ ",
	"────────────────────────────────────────────────────────────────────────",
	"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · ↓ to manage",
	"",
	"  ● main",
	"  ◯ general-purpose  Updating margin figures in doc.go",
}

func TestBackgroundAgentIsDetected(t *testing.T) {
	if !hasRunningBackgroundAgent(backgroundAgentPane) {
		t.Fatal("a pane waiting on a background agent was not seen as busy")
	}
}

// The whole point: the other indicators do not fire on this pane, which is why
// a separate check was needed. If one of them starts matching, this check has
// become redundant and should be reconsidered rather than left to overlap.
func TestTheOtherBusyIndicatorsMissThisPane(t *testing.T) {
	if hasEscToInterrupt(backgroundAgentPane) {
		t.Error("esc-to-interrupt now matches; the background-agent check may be redundant")
	}
	if hasActiveThinking(backgroundAgentPane, 20) {
		t.Error("the thinking check now matches; the background-agent check may be redundant")
	}
}

func TestFinishedSessionIsNotBusy(t *testing.T) {
	pane := []string{
		"● Kész, minden teszt zöld.",
		"",
		"✻ Cogitated for 32m 18s · done 15:45",
		"────────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────────",
		"  ● main",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("a finished session was reported as still running an agent")
	}
}

func TestSeveralAgentsAreDetected(t *testing.T) {
	if !hasRunningBackgroundAgent([]string{"✻ Waiting for 3 background agents to finish"}) {
		t.Fatal("the plural form was not matched")
	}
}

// Someone discussing the feature in a prompt must not flip the session to busy.
func TestTheWordsAloneAreNotEnough(t *testing.T) {
	pane := []string{
		"❯ how do I tell whether it is waiting for background agents?",
		"● You can check the agent list at the bottom of the screen.",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("prose about background agents was taken for the status line")
	}
}

// The detector must survive the colouring tmux captures with the pane.
func TestAnsiColouringDoesNotHideIt(t *testing.T) {
	pane := []string{"\x1b[2m✻ Waiting for 1 background agent to finish\x1b[0m"}
	if !hasRunningBackgroundAgent(pane) {
		t.Fatal("ANSI codes hid the status line")
	}
}

// The wording belongs to Anthropic, not to us: patterns.json is what makes a
// reword a config change rather than a release. These check the plumbing is
// really wired to the file.
func TestBackgroundAgentPatternsComeFromTheFile(t *testing.T) {
	setPatternsForTest(t, &patternsFile{
		Version:             99,
		BackgroundAgentBusy: []string{`still chewing on \d+ things`},
	})
	if !hasRunningBackgroundAgent([]string{"✻ Still chewing on 2 things"}) {
		t.Fatal("a pattern from the file was not used")
	}
	if hasRunningBackgroundAgent([]string{"✻ Waiting for 1 background agent to finish"}) {
		t.Fatal("the compiled default was used even though the file supplied patterns")
	}
}

func TestCompiledDefaultAppliesWhenTheFileHasNone(t *testing.T) {
	setPatternsForTest(t, &patternsFile{Version: 99})
	if !hasRunningBackgroundAgent([]string{"✻ Waiting for 1 background agent to finish"}) {
		t.Fatal("with no configured patterns the compiled default should still apply")
	}
}

// The file arrives over the network. One unparsable entry must not take the
// others down with it.
func TestAnUnparsablePatternIsSkipped(t *testing.T) {
	setPatternsForTest(t, &patternsFile{
		Version:             99,
		BackgroundAgentBusy: []string{`([unclosed`, `waiting for \d+ background agents? to finish`},
	})
	if !hasRunningBackgroundAgent([]string{"✻ Waiting for 1 background agent to finish"}) {
		t.Fatal("a broken expression stopped the working one being used")
	}
}

// The shipped file must actually carry the pattern, or every install falls back
// to the compiled copy and the file is decorative.
func TestShippedPatternsFileCarriesTheDefault(t *testing.T) {
	var shipped patternsFile
	if err := json.Unmarshal(embeddedPatterns, &shipped); err != nil {
		t.Fatalf("the embedded patterns.json does not parse: %v", err)
	}
	if len(shipped.BackgroundAgentBusy) == 0 {
		t.Fatal("patterns.json ships no backgroundAgentBusy entries")
	}
	setPatternsForTest(t, &shipped)
	if !hasRunningBackgroundAgent(backgroundAgentPane) {
		t.Fatal("the shipped patterns do not match the pane this was written for")
	}
}

// Claude Code abbreviates when the pane is narrow: the full sentence becomes a
// count in the status bar. Both mean the same thing and both must be seen.
func TestAbbreviatedStatusBarFormIsDetected(t *testing.T) {
	pane := []string{
		"✻ Cogitated for 32m 18s · done 15:45",
		"────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────",
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent",
	}
	if !hasRunningBackgroundAgent(pane) {
		t.Fatal("the abbreviated '← 1 agent' form was not seen as busy")
	}
}

// The distinction the count carries: "← for agents" is the resting wording and
// is on screen constantly. Reading it as busy would make every idle Claude
// session look like it were working.
func TestTheIdleAgentsHintIsNotBusy(t *testing.T) {
	pane := []string{
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · ↓ to manage",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("the idle '← for agents' hint was taken for a running agent")
	}
}

func TestSeveralAgentsInTheAbbreviatedForm(t *testing.T) {
	if !hasRunningBackgroundAgent([]string{"· ← 3 agents"}) {
		t.Fatal("the plural abbreviation was not matched")
	}
}
