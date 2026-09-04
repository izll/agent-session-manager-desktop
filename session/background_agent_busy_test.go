package session

import (
	"encoding/json"
	"strings"
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

// The status bar's "← 1 agent" was tried as a shorter form of the same signal
// and removed. It is a count of the agents the session HAS, not of what is
// running: it stays on screen after the work finishes, so a session that had
// plainly ended — recap printed, "Sautéed for 2m 9s" — was marked busy.
func TestTheAgentCountAloneIsNotBusy(t *testing.T) {
	pane := []string{
		"● Szólj, ha nézzem meg, mi eszi a RAM-ot.",
		"",
		"✻ Sautéed for 2m 9s",
		"",
		"※ recap: Cél a plandoc gép memórianyomásának enyhítése.",
		"────────────────────────────────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────────────────────────────────",
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("a finished session was marked busy by the agent count in the status bar")
	}
}

// The idle hint must not match either — it is on screen constantly.
func TestTheIdleAgentsHintIsNotBusy(t *testing.T) {
	pane := []string{
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · ↓ to manage",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("the idle '← for agents' hint was taken for a running agent")
	}
}

// The notice is not removed when the agent finishes: it scrolls up into the
// transcript and stays on screen. Searching the whole pane therefore kept
// reporting busy long after the work ended — observed on a session that had
// been idle for over an hour with the sentence sitting 77 lines up.
func TestTheNoticeInTheTranscriptIsNotLive(t *testing.T) {
	separator := strings.Repeat("─", 72)
	pane := []string{
		"● Elindítom a hátteret.",
		"",
		"✻ Waiting for 1 background agent to finish",
		"",
		"● Az agent végzett, összefoglalom.",
		"",
		"● A mérés lezárult, minden referencia bitre azonos.",
		"",
		"✻ Cooked for 1m 0s · done 14:50",
		"",
		"※ recap: a következő lépés a te döntésed.",
		"",
		separator,
		"❯ ",
		separator,
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents",
	}
	if hasRunningBackgroundAgent(pane) {
		t.Fatal("a notice left behind in the transcript was taken for a live one")
	}
}

// The live notice sits just above the input box, so the search has to cross the
// separator to see it at all. Guarding against the transcript must not cost us
// the real thing.
func TestTheNoticeAboveTheInputBoxIsLive(t *testing.T) {
	separator := strings.Repeat("─", 72)
	pane := []string{
		"● Elindítom a hátteret.",
		"",
		"✻ Waiting for 2 background agents to finish",
		"",
		separator,
		"❯ ",
		separator,
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 2 agents",
	}
	if !hasRunningBackgroundAgent(pane) {
		t.Fatal("the live notice above the input box was missed")
	}
}
