package main

import (
	"testing"

	"asmgr-desktop/session"
)

// The Cursor entry pointed at `cursor`, which is the GUI editor: starting a
// session opened a window instead of an agent. The CLI is a separate binary.
func TestCursorRunsTheAgentNotTheEditor(t *testing.T) {
	cfg, ok := session.AgentConfigs[session.AgentCursor]
	if !ok {
		t.Fatal("cursor has no agent config")
	}
	if cfg.Command != "cursor-agent" {
		t.Errorf("command = %q; want cursor-agent (plain 'cursor' is the editor)", cfg.Command)
	}
}

// Both flags come from `cursor-agent --help`. The entry claimed neither was
// supported, so resume and auto-approve were hidden for an agent that has them.
func TestCursorResumeAndAutoYesMatchItsCLI(t *testing.T) {
	cfg := session.AgentConfigs[session.AgentCursor]
	if !cfg.SupportsResume || cfg.ResumeFlag != "--resume" {
		t.Errorf("resume = %v %q; want true --resume", cfg.SupportsResume, cfg.ResumeFlag)
	}
	if !cfg.SupportsAutoYes || cfg.AutoYesFlag != "--force" {
		t.Errorf("auto-yes = %v %q; want true --force", cfg.SupportsAutoYes, cfg.AutoYesFlag)
	}
	// Resume is a flag, not a subcommand: `cursor-agent --resume [chatId]`.
	if cfg.ResumeIsSubcommand {
		t.Error("resume is a flag on cursor-agent, not a subcommand")
	}
}

// cursor-agent has no fork subcommand or flag, and the UI must say so rather
// than offering a button that fails.
func TestCursorDoesNotClaimFork(t *testing.T) {
	if cfg := session.AgentConfigs[session.AgentCursor]; cfg.ForkFlag != "" {
		t.Errorf("fork flag = %q; cursor-agent has none", cfg.ForkFlag)
	}
}

// An agent the user can pick has to be pickable: the dialog reads GetAgents,
// and an entry only in AgentConfigs never appears. Cursor sat in the config
// for a while with no way to choose it.
func TestCursorIsOfferedInTheAgentList(t *testing.T) {
	app := &App{}
	for _, a := range app.GetAgents() {
		if a.Type == "cursor" {
			if a.Name != "Cursor" {
				t.Errorf("name = %q; want Cursor", a.Name)
			}
			return
		}
	}
	t.Error("cursor is configured but not offered in the new-tab dialog")
}
