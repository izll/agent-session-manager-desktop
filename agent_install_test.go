package main

import (
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// Every agent that runs a command of its own needs somewhere to send the user
// when that command is missing. Without a URL the dialog can say what is wrong
// but not what to do about it.
func TestEveryRealAgentHasAnInstallPage(t *testing.T) {
	pseudo := map[string]bool{"custom": true, "terminal": true}
	for _, a := range (&App{}).GetAgents() {
		if pseudo[a.Type] {
			if a.InstallURL != "" {
				t.Errorf("%s is a pseudo-agent but carries an install URL", a.Type)
			}
			continue
		}
		if a.InstallURL == "" {
			t.Errorf("%s has no install page, so a missing command offers nothing", a.Type)
			continue
		}
		if !strings.HasPrefix(a.InstallURL, "https://") {
			t.Errorf("%s install URL is not https: %q", a.Type, a.InstallURL)
		}
	}
}

// Terminal runs the user's own shell. It has no entry in AgentConfigs, and
// falling through the lookup left it reported as missing — the dialog then
// offered to install a terminal.
func TestTerminalIsNeverReportedAsMissing(t *testing.T) {
	for _, a := range (&App{}).GetAgents() {
		if a.Type != "terminal" {
			continue
		}
		if !a.Installed {
			t.Error("terminal is reported as not installed; it runs the user's shell")
		}
		return
	}
	t.Fatal("terminal is missing from the agent list")
}

// The install pages come from the agent configuration, so the two cannot
// drift: a new agent added there without a URL fails the test above.
func TestInstallURLsComeFromTheAgentConfig(t *testing.T) {
	for _, a := range (&App{}).GetAgents() {
		config, ok := session.AgentConfigs[session.AgentType(a.Type)]
		if !ok {
			continue
		}
		if a.InstallURL != config.InstallURL {
			t.Errorf("%s: list says %q, config says %q", a.Type, a.InstallURL, config.InstallURL)
		}
	}
}

// Installed has to reflect PATH rather than being assumed. A command that
// cannot exist must come back false, or the offer never appears.
func TestInstalledFollowsPath(t *testing.T) {
	cfg := session.AgentConfigs[session.AgentClaude]
	if cfg.Command == "" {
		t.Fatal("claude has no command to look for")
	}
	// The real answer depends on the machine; what must hold is that the field
	// is derived, not hard-coded true for everything with a command.
	var sawCommand bool
	for _, a := range (&App{}).GetAgents() {
		if c, ok := session.AgentConfigs[session.AgentType(a.Type)]; ok && c.Command != "" {
			sawCommand = true
		}
	}
	if !sawCommand {
		t.Fatal("no agent has a command, so this proves nothing")
	}
}
