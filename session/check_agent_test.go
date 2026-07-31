package session

import "testing"

// A terminal session has no agent to find, and a session must never be refused
// because of one that does not apply.
func TestCheckAgentCommandAllowsTerminal(t *testing.T) {
	if err := CheckAgentCommand(&Instance{Agent: AgentTerminal}); err != nil {
		t.Fatalf("terminal sessions must not be refused: %v", err)
	}
}

// A missing agent has to be refused, or the session is created and can never
// start — which leaves a dead entry in the sidebar.
func TestCheckAgentCommandRefusesMissing(t *testing.T) {
	inst := &Instance{Agent: AgentCustom, CustomCommand: "definitely-not-installed-xyz"}
	if err := CheckAgentCommand(inst); err == nil {
		t.Fatal("a command that is not installed must be refused")
	}
}
