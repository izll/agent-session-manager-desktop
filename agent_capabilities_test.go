package main

import (
	"testing"

	"asmgr-desktop/session"
)

// The agent list the UI reads must report what the agent configuration
// actually says.
//
// These were two hand-written lists, and they drifted: Codex grew a `fork`
// subcommand, the configuration knew it, and this list still said
// SupportsFork: false — so the button stayed hidden for a thing that worked.
// Nothing failed; the feature simply was not offered.
func TestAgentCapabilitiesComeFromConfig(t *testing.T) {
	app := &App{}
	agents := app.GetAgents()
	if len(agents) == 0 {
		t.Fatal("no agents reported")
	}

	for _, a := range agents {
		config, ok := session.AgentConfigs[session.AgentType(a.Type)]
		if !ok {
			// Terminal has no agent config: it runs a shell, not a conversation.
			if a.SupportsFork || a.SupportsResume || a.SupportsAutoYes {
				t.Errorf("%s has no config but claims capabilities", a.Type)
			}
			continue
		}
		if want := config.ForkFlag != ""; a.SupportsFork != want {
			t.Errorf("%s: SupportsFork=%v, config says %v", a.Type, a.SupportsFork, want)
		}
		if a.SupportsResume != config.SupportsResume {
			t.Errorf("%s: SupportsResume=%v, config says %v", a.Type, a.SupportsResume, config.SupportsResume)
		}
		if a.SupportsAutoYes != config.SupportsAutoYes {
			t.Errorf("%s: SupportsAutoYes=%v, config says %v", a.Type, a.SupportsAutoYes, config.SupportsAutoYes)
		}
	}
}

// Both agents that can fork must produce a command the agent will accept:
// Claude takes a flag beside its resume, Codex a subcommand of its own.
func TestForkArgumentsMatchEachAgentsShape(t *testing.T) {
	claude := session.AgentConfigs[session.AgentClaude]
	if got := session.ForkArgsForTest(claude, nil, "abc"); len(got) != 3 ||
		got[0] != "--resume" || got[1] != "abc" || got[2] != "--fork-session" {
		t.Errorf("claude fork args = %v; want [--resume abc --fork-session]", got)
	}

	codex := session.AgentConfigs[session.AgentCodex]
	if got := session.ForkArgsForTest(codex, nil, "abc"); len(got) != 2 ||
		got[0] != "fork" || got[1] != "abc" {
		t.Errorf("codex fork args = %v; want [fork abc]", got)
	}
}
