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

// Claude Code 2.1.310, captured while two agents worked and the main thread sat
// at the prompt. The "waiting for background agents" notice is gone: the agent
// list under the status bar is the only sign, and the app showed the tab idle.
var agentListPane = []string{
	"● Az S2B forrásvak tesztjei elkészültek: 31 teszt, a régi kódon zöldek.",
	"● Agent \"Source-blind S2B review tests\" finished · 24m 19s",
	strings.Repeat("─", 120),
	"❯ ",
	strings.Repeat("─", 120),
	"  ⏵⏵ auto mode on · 1 shell · ← 2 agents · ↓ to manage",
	"  ● main",
	"  ◯ general-purpose  Hashing PERF-S2B-PROPOSAL.md and PROVENANCE.md" + strings.Repeat(" ", 60) + "1h 0m 27s · ↓ 298.8k tokens",
	"  ◯ general-purpose  Timing n250 Grid bottom-left path" + strings.Repeat(" ", 60) + "26m 50s · ↓ 341.6k tokens",
}

func TestAnAgentInTheListIsBusy(t *testing.T) {
	if !hasBackgroundAgentRow(agentListPane) {
		t.Fatal("an agent named in the list under the status bar was not seen")
	}
	if got := detectClaudeActivity(agentListPane, getAgentPatterns(AgentClaude), ""); got != ActivityBusy {
		t.Fatalf("detectClaudeActivity = %v, want busy", got)
	}
}

// Why the list needed a check of its own: nothing else on this pane says busy.
func TestTheOtherBusyIndicatorsMissTheAgentList(t *testing.T) {
	if hasRunningBackgroundAgent(agentListPane) || hasEscToInterrupt(agentListPane) || hasActiveThinking(agentListPane, 20) {
		t.Error("another indicator now matches; the agent list check may be redundant")
	}
}

// As tmux hands it over: the row is drawn dim, each part coloured separately.
func TestAnAgentRowSurvivesItsColouring(t *testing.T) {
	pane := []string{
		strings.Repeat("─", 72),
		"\x1b[38;5;246m \x1b[39m \x1b[38;5;246m◯\x1b[39m \x1b[38;5;246mgeneral-purpose\x1b[39m  \x1b[38;5;246mTiming n250 Grid bottom-left path\x1b[39m   \x1b[38;5;246m26m 50s\x1b[39m",
	}
	if !hasBackgroundAgentRow(pane) {
		t.Fatal("ANSI codes hid the agent row")
	}
}

// The main thread's row is on screen whenever the list is, and the count in the
// status bar outlives the agents. Neither is an agent at work.
func TestTheMainRowAndTheCountAreNotAnAgent(t *testing.T) {
	pane := []string{
		"✻ Sautéed for 32s · done vasárnap 13:00",
		strings.Repeat("─", 72),
		"❯ ",
		strings.Repeat("─", 72),
		"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← 2 agents · ↓ to manage",
		"  ● main",
	}
	if hasBackgroundAgentRow(pane) {
		t.Fatal("a session with no agent left was marked busy")
	}
}

// A row quoted in the transcript — pasted, or in an answer about this very
// feature — sits above the input box and must not count.
func TestAnAgentRowInTheTranscriptIsNotLive(t *testing.T) {
	pane := []string{
		"  ◯ general-purpose  Timing n250 Grid bottom-left path   26m 50s · ↓ 341.6k tokens",
		"● That row means the agent is still running.",
		strings.Repeat("─", 72),
		"❯ ",
		strings.Repeat("─", 72),
		"  ⏵⏵ auto mode on (shift+tab to cycle)",
	}
	if hasBackgroundAgentRow(pane) {
		t.Fatal("a row quoted in the transcript was taken for a live agent")
	}
}

func TestAgentRowPatternsComeFromTheFile(t *testing.T) {
	setPatternsForTest(t, &patternsFile{
		Version:            99,
		BackgroundAgentRow: []string{`^\s+» helper`},
	})
	if !hasBackgroundAgentRow([]string{"  » helper doing things"}) {
		t.Fatal("a row pattern from the file was not used")
	}
	if hasBackgroundAgentRow(agentListPane) {
		t.Fatal("the compiled default was used even though the file supplied patterns")
	}
}

func TestShippedPatternsFileCarriesTheAgentRow(t *testing.T) {
	var shipped patternsFile
	if err := json.Unmarshal(embeddedPatterns, &shipped); err != nil {
		t.Fatalf("the embedded patterns.json does not parse: %v", err)
	}
	if !slicesEqual(shipped.BackgroundAgentRow, defaultBackgroundAgentRowPatterns) {
		t.Fatalf("patterns.json row patterns %q differ from the compiled default %q",
			shipped.BackgroundAgentRow, defaultBackgroundAgentRowPatterns)
	}
	setPatternsForTest(t, &shipped)
	if !hasBackgroundAgentRow(agentListPane) {
		t.Fatal("the shipped patterns do not match the pane this was written for")
	}
}

// madisocad, Claude Code 2.1.310: the frontend and backend dev servers Claude
// started run as background shells, and the status bar counts them. A count
// was tried as a sign of work — a test gate Claude waits on looks the same —
// and removed: servers run for days, and the tab never went idle.
var devServersPane = []string{
	"● Fut a frontend és a backend, a böngészőben megnézheted.",
	"",
	"✻ Baked for 2m 3s · done 9:40 · 2 shells still running",
	"",
	strings.Repeat("─", 120),
	"❯ ",
	strings.Repeat("─", 120),
	"  ⏵⏵ auto mode on · 2 shells · ← 2 agents · ↓ to manage",
}

func TestRunningShellsAreNotWork(t *testing.T) {
	if hasBackgroundAgentRow(devServersPane) {
		t.Fatal("the status bar's shell count was taken for an agent at work")
	}
	if got := detectClaudeActivity(devServersPane, getAgentPatterns(AgentClaude), ""); got != ActivityIdle {
		t.Fatalf("detectClaudeActivity = %v, want idle", got)
	}
}

// The shipped file must not bring the shell count back: version 5 carried it,
// and only a higher version replaces a copy an install already downloaded.
func TestShippedPatternsIgnoreTheShellCount(t *testing.T) {
	var shipped patternsFile
	if err := json.Unmarshal(embeddedPatterns, &shipped); err != nil {
		t.Fatalf("the embedded patterns.json does not parse: %v", err)
	}
	if shipped.Version <= 5 {
		t.Fatalf("patterns.json version %d does not replace version 5, which counted shells", shipped.Version)
	}
	setPatternsForTest(t, &shipped)
	if hasBackgroundAgentRow(devServersPane) {
		t.Fatal("the shipped patterns read the shell count as work")
	}
}
