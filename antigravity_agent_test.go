package main

import (
	"testing"

	"asmgr-desktop/session"
)

// The binary is agy, not antigravity or av: the install script puts `agy` on
// PATH and nothing else answers. Searching the web for the name turns up both
// of the others, which is how this could have gone wrong.
func TestAntigravityRunsAgy(t *testing.T) {
	cfg, ok := session.AgentConfigs[session.AgentAntigravity]
	if !ok {
		t.Fatal("antigravity has no agent config")
	}
	if cfg.Command != "agy" {
		t.Errorf("command = %q; want agy", cfg.Command)
	}
}

// Both flags are from `agy --help` on 1.2.3.
func TestAntigravityResumeAndAutoYesMatchItsCLI(t *testing.T) {
	cfg := session.AgentConfigs[session.AgentAntigravity]
	// --conversation, not --continue: the latter takes no id, so the id was
	// passed as a bare argument and agy rejected it as an unexpected prompt.
	if !cfg.SupportsResume || cfg.ResumeFlag != "--conversation" {
		t.Errorf("resume = %v %q; want true --conversation", cfg.SupportsResume, cfg.ResumeFlag)
	}
	if !cfg.SupportsAutoYes || cfg.AutoYesFlag != "--dangerously-skip-permissions" {
		t.Errorf("auto-yes = %v %q; want true --dangerously-skip-permissions",
			cfg.SupportsAutoYes, cfg.AutoYesFlag)
	}
	// There is no --session-id and no fork flag in its help. Claiming either
	// would show the user a button that cannot work.
	if cfg.SupportsSessionID {
		t.Error("antigravity claims a pre-assigned session id; its CLI has no such flag")
	}
	if cfg.ForkFlag != "" {
		t.Errorf("antigravity claims it can fork with %q; its CLI cannot", cfg.ForkFlag)
	}
}

// Antigravity is listed before Gemini: it is the harness Google moved Gemini
// CLI into, and the one still served to consumer accounts.
func TestAntigravityIsListedBeforeGemini(t *testing.T) {
	app := &App{}
	agents := app.GetAgents()

	ag, gem := -1, -1
	for i, a := range agents {
		switch a.Type {
		case "antigravity":
			ag = i
		case "gemini":
			gem = i
		}
	}
	if ag < 0 {
		t.Fatal("antigravity is missing from the agent list")
	}
	if gem < 0 {
		t.Fatal("gemini is missing from the agent list")
	}
	if ag > gem {
		t.Errorf("antigravity is at %d, after gemini at %d", ag, gem)
	}
	if !agents[ag].SupportsResume || !agents[ag].SupportsAutoYes {
		t.Error("the list does not carry antigravity's resume/auto-yes capability")
	}
}
