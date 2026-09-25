package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Captured from a running Codex 0.155.1 in tmux (capture-pane -p -S -50). The
// prompt sits above ~50 blank rows: Codex draws it at the top of a tall pane.
func codexUpdatePromptPane() []string {
	top := []string{
		"╭───────────────────────────────────────╮",
		"│ >_ OpenAI Codex (v0.155.1)            │",
		"│                                       │",
		"│ model:     loading   /model to change │",
		"│ directory: ~/NetBeansProjects/tisza   │",
		"╰───────────────────────────────────────╯",
		"  Resuming session…",
		"",
		"› Ask Codex to do anything",
		"",
		"  ? for shortcuts",
		"",
		"  ✨ Update available! 0.155.1 -> 0.156.1",
		"",
		"  Release notes: https://github.com/openai/codex/releases/latest",
		"",
		"› 1. Update now (runs `npm install -g @openai/codex`)",
		"  2. Skip",
		"  3. Skip until next version",
		"",
		"  Press enter to continue",
	}
	return append(top, make([]string, 50)...)
}

// Captured from a running Claude Code: the footer, right side.
var claudeUpdateInstalledPane = []string{
	"✻ Baked for 1m 49s · done 7:53",
	"────────────────────────────────────────────────────────────",
	"❯ ",
	"────────────────────────────────────────────────────────────",
	"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← 2 agents   ✔ Update installed · Restart to update",
	"",
}

// The same footer in a pane too narrow for all of it.
var claudeUpdateInstalledNarrowPane = []string{
	"❯ ",
	"──────────────────────────────",
	"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 2 agents ✔ Update installed · Restar",
}

// Captured from a running Amazon Q.
var amazonQUpdatePane = []string{
	"A new version of q is available: 2.24.0",
	"Run q update to update to the new version",
	"> ",
}

func TestUpdateNoticeFromRealPanes(t *testing.T) {
	cases := []struct {
		name  string
		agent AgentType
		lines []string
		want  UpdateNotice
	}{
		{"codex prompt", AgentCodex, codexUpdatePromptPane(),
			UpdateNotice{Kind: UpdateAvailable, Blocking: true, Current: "0.155.1", Version: "0.156.1"}},
		{"claude footer", AgentClaude, claudeUpdateInstalledPane,
			UpdateNotice{Kind: UpdateInstalledRestart}},
		{"claude footer cut short", AgentClaude, claudeUpdateInstalledNarrowPane,
			UpdateNotice{Kind: UpdateInstalledRestart}},
		{"amazon q", AgentAmazonQ, amazonQUpdatePane,
			UpdateNotice{Kind: UpdateAvailable, Version: "2.24.0", Command: "q update"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectUpdateNotice(tc.agent, tc.lines)
			if got == nil {
				t.Fatalf("no notice found")
			}
			if *got != tc.want {
				t.Fatalf("got %+v, want %+v", *got, tc.want)
			}
		})
	}
}

// Samples quoted from each agent's source (or, for the closed-source ones, its
// installed binary), as the notice renders; see update_notice.go for where.
func TestUpdateNoticeFromCitedSources(t *testing.T) {
	cases := []struct {
		name  string
		agent AgentType
		lines []string
		want  UpdateNotice
	}{
		{"codex prompt, current wording", AgentCodex, []string{
			"  Update available · 0.156.1 → 0.157.0",
			"  Release notes: https://github.com/openai/codex/releases/latest",
			"› 1. Update now (runs `npm install -g @openai/codex`)",
			"  2. Skip",
			"  3. Skip until next version",
			"  enter continue · esc skip",
		}, UpdateNotice{Kind: UpdateAvailable, Blocking: true, Current: "0.156.1", Version: "0.157.0"}},
		{"codex transcript notice", AgentCodex, []string{
			"╭──────────────────────────────────────────────────╮",
			"│ ✨ Update available! 0.156.1 -> 0.157.0          │",
			"│ Run npm install -g @openai/codex to update.      │",
			"│                                                  │",
			"│ See full release notes:                          │",
			"│ https://github.com/openai/codex/releases/latest  │",
			"╰──────────────────────────────────────────────────╯",
			"› Ask Codex to do anything",
		}, UpdateNotice{Kind: UpdateAvailable, Current: "0.156.1", Version: "0.157.0"}},
		{"codex updated", AgentCodex, []string{
			"Updating Codex via `npm install -g @openai/codex`...",
			"🎉 Update ran successfully! Please restart Codex.",
			"$ ",
		}, UpdateNotice{Kind: UpdateInstalledRestart}},
		{"claude native installer", AgentClaude, []string{
			"────────────────────────",
			"❯ ",
			"────────────────────────",
			"  ? for shortcuts            ✓ Update installed via native · Restart to apply",
		}, UpdateNotice{Kind: UpdateInstalledRestart}},
		{"claude could not install", AgentClaude, []string{
			"────────────────────────",
			"❯ ",
			"────────────────────────",
			"  ? for shortcuts            Update available! Run: claude update (auto-update failed)",
		}, UpdateNotice{Kind: UpdateAvailable}},
		{"gemini", AgentGemini, []string{
			"╭──────────────────────────────────────────────╮",
			"│ Gemini CLI update available! 0.8.2 → 0.9.0   │",
			"│ Installed with npm. Attempting to automatically update now...",
			"╰──────────────────────────────────────────────╯",
			"╭──────────────────────────────────────────────╮",
			"│ >   Type your message or @path/to/file       │",
			"╰──────────────────────────────────────────────╯",
			"~/project        no sandbox        gemini-2.5-pro",
		}, UpdateNotice{Kind: UpdateAvailable, Current: "0.8.2", Version: "0.9.0"}},
		{"gemini nightly", AgentGemini, []string{
			"A new version of Gemini CLI is available! 0.9.0-nightly.20260901 → 0.9.0-nightly.20260920",
		}, UpdateNotice{Kind: UpdateAvailable, Current: "0.9.0-nightly.20260901", Version: "0.9.0-nightly.20260920"}},
		{"gemini updated", AgentGemini, []string{
			"ℹ Update successful! The new version will be used on your next run.",
			"│ >   Type your message or @path/to/file       │",
		}, UpdateNotice{Kind: UpdateInstalledRestart}},
		{"aider asking", AgentAider, []string{
			"Newer aider version v0.86.1 is available.",
			"/usr/bin/python3 -m pip install --upgrade --upgrade-strategy only-if-needed aider-chat",
			"Run pip install? (Y)es/(N)o [Yes]:",
		}, UpdateNotice{Kind: UpdateAvailable, Blocking: true, Version: "0.86.1"}},
		{"aider answered no", AgentAider, []string{
			"Newer aider version v0.86.1 is available.",
			"/usr/bin/python3 -m pip install --upgrade --upgrade-strategy only-if-needed aider-chat",
			"Run pip install? (Y)es/(N)o [Yes]: n",
			"Aider v0.85.0",
			"Main model: sonnet",
			">",
		}, UpdateNotice{Kind: UpdateAvailable, Version: "0.86.1"}},
		{"aider updated", AgentAider, []string{
			"Re-run aider to use new version.",
			"$ ",
		}, UpdateNotice{Kind: UpdateInstalledRestart}},
		{"opencode dialog", AgentOpenCode, []string{
			"  Update Available                              esc",
			"  A new release v1.3.0 is available. Would you like to update now?",
			"                                   Skip   Confirm",
		}, UpdateNotice{Kind: UpdateAvailable, Blocking: true, Version: "1.3.0"}},
		{"opencode updated", AgentOpenCode, []string{
			"  Update Complete",
			"  Successfully updated to OpenCode v1.3.0. Please restart the application.",
		}, UpdateNotice{Kind: UpdateInstalledRestart, Version: "1.3.0"}},
		{"cursor about", AgentCursor, []string{
			"  Version   2026.09.18-9a7762b",
			"  Latest    2026.09.20-1a2b3c4 (update available — run `agent update`)",
		}, UpdateNotice{Kind: UpdateAvailable, Version: "2026.09.20-1a2b3c4"}},
		{"cursor files replaced", AgentCursor, []string{
			"Error: The installed Cursor Agent files changed while it was running (usually due to an update). Restart the CLI to fix this.",
		}, UpdateNotice{Kind: UpdateInstalledRestart}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectUpdateNotice(tc.agent, tc.lines)
			if got == nil {
				t.Fatalf("no notice found")
			}
			if *got != tc.want {
				t.Fatalf("got %+v, want %+v", *got, tc.want)
			}
		})
	}
}

// The Codex headline without its prompt below it is a passing message, not a
// question the agent is waiting on.
func TestCodexUpdateHeadlineAloneDoesNotBlock(t *testing.T) {
	lines := []string{
		"• Ran go test ./...",
		"  ✨ Update available! 0.155.1 -> 0.156.1",
		"  Run npm install -g @openai/codex to update.",
		"› Ask Codex to do anything",
	}
	got := DetectUpdateNotice(AgentCodex, lines)
	if got == nil || got.Blocking {
		t.Fatalf("got %+v, want a non-blocking notice", got)
	}
}

func TestUpdateNoticeNonMatches(t *testing.T) {
	cases := []struct {
		name  string
		agent AgentType
		lines []string
	}{
		{"ordinary talk about updates", AgentClaude, []string{
			"● I'll update the README and check for an update available in go.mod.",
			"  The new version of the parser is installed.",
			"● The update installed cleanly; restart to update the cache.",
			"❯ ",
		}},
		{"codex editing update code", AgentCodex, []string{
			"• Edited src/updates.rs (+3 -1)",
			"    let msg = \"Update available!\";",
			"› Ask Codex to do anything",
		}},
		{"claude quoting its footer in the transcript", AgentClaude, []string{
			"● The footer then reads \"✔ Update installed · Restart to update\".",
			"────────────────────────",
			"❯ ",
			"────────────────────────",
			"  ? for shortcuts",
		}},
		{"gemini talking about versions", AgentGemini, []string{
			"✦ The changelog says a Gemini CLI update is available from 0.8 to 0.9.",
		}},
		{"aider talking about pip", AgentAider, []string{
			"You can run pip install aider-chat to get the newer aider version.",
			">",
		}},
		{"another agent's notice", AgentGemini, claudeUpdateInstalledPane},
		{"q notice in a claude pane", AgentClaude, amazonQUpdatePane},
		{"terminal tab", AgentTerminal, amazonQUpdatePane},
		{"notice scrolled into the transcript", AgentCodex, append(
			[]string{"  ✨ Update available! 0.155.1 -> 0.156.1"},
			strings.Split(strings.Repeat("• did some work\n", 12), "\n")...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectUpdateNotice(tc.agent, tc.lines); got != nil {
				t.Fatalf("got %+v, want none", *got)
			}
		})
	}
}

// Codex's update prompt blocks the agent until Enter, yet nothing on that
// screen is one of its approval phrases, so the tab read as idle.
func TestBlockingUpdatePromptReadsAsWaiting(t *testing.T) {
	lines := codexUpdatePromptPane()
	activity, update := classifyPaneContext(context.Background(), AgentCodex, lines, "test:0")
	if activity != ActivityWaiting {
		t.Fatalf("activity = %v, want waiting", activity)
	}
	if update == nil || !update.Blocking {
		t.Fatalf("update = %+v, want a blocking notice", update)
	}
}

// A passing notice leaves the activity alone.
func TestNonBlockingUpdateLeavesActivity(t *testing.T) {
	activity, update := classifyPaneContext(context.Background(), AgentAmazonQ, amazonQUpdatePane, "test:0")
	if update == nil || update.Blocking {
		t.Fatalf("update = %+v, want a passing notice", update)
	}
	if activity == ActivityWaiting {
		t.Fatalf("a passing notice made the tab waiting")
	}
}

func TestUpdateNoticeJSONShape(t *testing.T) {
	data, err := json.Marshal(UpdateNotice{Kind: UpdateInstalledRestart})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"kind":"installed-restart","blocking":false}` {
		t.Fatalf("got %s", data)
	}
}

// Every entry names at least one agent, a pattern and a kind:
// an entry without an agent would never fire.
func TestUpdatePatternTableEntriesHaveAgents(t *testing.T) {
	for i, p := range updatePatterns {
		if len(p.agents) == 0 || p.match == nil || p.kind == "" {
			t.Errorf("entry %d is incomplete: %+v", i, p)
		}
	}
}

func TestHideUpdateBadgeDefaultsToShown(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"compact_list":true}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.HideUpdateBadge {
		t.Error("legacy settings hid the update badge; it should stay visible")
	}
	data, _ := json.Marshal(Settings{HideUpdateBadge: true})
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil || !loaded.HideUpdateBadge {
		t.Fatalf("flag lost in a round trip: %s", data)
	}
}
