package session

import (
	"strings"
	"testing"
)

// While a spawned agent works, the main thread has already finished speaking —
// so the conversation's last line is a completed thought ("I'll report back
// when v15 is done") while the session is in fact busy doing something else.
// The agent list says what that something is, and it changes as work moves on.

func agentPane(agentLine string) []string {
	return []string{
		"● Szólok, amint a v15 eredménye megvan.",
		"",
		"✻ Waiting for 1 background agent to finish",
		"─────────────────────────────────────────────────────────────────────",
		"❯ ",
		"─────────────────────────────────────────────────────────────────────",
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · ↓ to manage",
		"",
		"  ● main",
		agentLine,
	}
}

func TestRunningAgentActivityBecomesTheStatus(t *testing.T) {
	got := GetClaudeStatusLine(
		agentPane("  ◯ general-purpose  Locating compactToContact in compact.go"), StripANSI)
	if got != "Locating compactToContact in compact.go" {
		t.Fatalf("status = %q, want the agent's activity", got)
	}
}

// The counters tick every second and say nothing about the work. They trail the
// description either after a "·" or pushed to the right margin with spaces.
func TestCountersAreStrippedFromTheActivity(t *testing.T) {
	cases := []string{
		"  ◯ general-purpose  Tracing ratchet convergence in Relax    1m 44s · ↓ 573.1k tokens",
		"  ◯ general-purpose  Tracing ratchet convergence in Relax · 1m 44s · ↓ 573.1k tokens",
		"  ◯ general-purpose  Tracing ratchet convergence in Relax                        6m 33s",
	}
	for _, line := range cases {
		got := GetClaudeStatusLine(agentPane(line), StripANSI)
		if got != "Tracing ratchet convergence in Relax" {
			t.Errorf("status = %q, want the description alone", got)
		}
	}
}

// The list is padded to the pane width; that padding must not reach the sidebar.
func TestActivityHasNoTrailingPadding(t *testing.T) {
	got := GetClaudeStatusLine(
		agentPane("  ◯ general-purpose  Rebuilding nesting-cli                              "), StripANSI)
	if got != strings.TrimSpace(got) {
		t.Fatalf("status kept its padding: %q", got)
	}
}

// The main thread's own bullet is filled, and must not be mistaken for an agent.
func TestMainThreadIsNotTakenForAnAgent(t *testing.T) {
	lines := []string{
		"● Kész vagyok, minden teszt zöld.",
		"─────────────────────────────────────────────────────────────────────",
		"❯ ",
		"─────────────────────────────────────────────────────────────────────",
		"  ● main",
	}
	got := GetClaudeStatusLine(lines, StripANSI)
	if got == "main" {
		t.Fatal("the main thread's list entry was used as the status")
	}
}

// With no agent list open — the ordinary case — the usual search must still run.
func TestWithoutAnAgentListTheConversationIsUsed(t *testing.T) {
	lines := []string{
		"● Nem commitoltam. Indítsd újra, és nézd meg a bal oldali listát.",
		"─────────────────────────────────────────────────────────────────────",
		"❯ ",
		"─────────────────────────────────────────────────────────────────────",
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · ↓ to manage",
	}
	got := GetClaudeStatusLine(lines, StripANSI)
	if !strings.Contains(got, "Nem commitoltam") {
		t.Fatalf("status = %q, want the last thing the session said", got)
	}
}
