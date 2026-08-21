package session

import "testing"

// Forking branches the conversation in the tab you are looking at.
//
// ForkSession took no window index and always read the session's main window,
// so someone working in a second Claude tab got a branch of a different
// conversation entirely — named after the work they thought they were
// branching. Nothing warned them; the fork succeeded, and the divergence only
// showed when they read the new tab's transcript.
func TestForkReadsTheWindowItWasAskedFor(t *testing.T) {
	t.Parallel()

	inst := &Instance{
		ID:              "session",
		Agent:           AgentClaude,
		ResumeSessionID: "main-conversation",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentClaude, ResumeSessionID: "tab-conversation"},
			{Index: 3, Agent: AgentTerminal},
		},
	}

	agent, id := inst.conversationInWindow(inst.GetMainWindowIndex())
	if agent != AgentClaude || id != "main-conversation" {
		t.Errorf("main window = (%s, %q), want the session's own conversation", agent, id)
	}

	agent, id = inst.conversationInWindow(2)
	if id != "tab-conversation" {
		t.Errorf("tab 2 = %q, want its own conversation — this is the bug: it "+
			"used to return the main window's", id)
	}
	if agent != AgentClaude {
		t.Errorf("tab 2 agent = %s, want claude", agent)
	}
}

// A tab can run a different agent from the session it lives in, and fork is
// only for Claude. Reading the session's agent offered it on tabs that have no
// conversation at all.
func TestForkSeesEachTabsOwnAgent(t *testing.T) {
	t.Parallel()

	inst := &Instance{
		ID:              "session",
		Agent:           AgentClaude,
		ResumeSessionID: "main-conversation",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentTerminal},
			{Index: 3, Agent: AgentCodex, ResumeSessionID: "codex-conversation"},
		},
	}

	if agent, _ := inst.conversationInWindow(2); agent != AgentTerminal {
		t.Errorf("terminal tab reported as %s — fork would be offered on a tab "+
			"with no conversation", agent)
	}
	if agent, _ := inst.conversationInWindow(3); agent != AgentCodex {
		t.Errorf("codex tab reported as %s", agent)
	}
}

// A tab created without an explicit agent inherits the session's, so it must
// not read as "no agent" and lose its fork button.
func TestATabWithNoAgentInheritsTheSessions(t *testing.T) {
	t.Parallel()

	inst := &Instance{
		Agent:           AgentClaude,
		ResumeSessionID: "main",
		FollowedWindows: []FollowedWindow{{Index: 2, ResumeSessionID: "tab"}},
	}
	if agent, _ := inst.conversationInWindow(2); agent != AgentClaude {
		t.Errorf("agent = %q, want the session's", agent)
	}
}

// An index that matches no window answers for the session rather than inventing
// a conversation — a window can be closed between the click and the read.
func TestAnUnknownWindowFallsBackToTheSession(t *testing.T) {
	t.Parallel()

	inst := &Instance{Agent: AgentClaude, ResumeSessionID: "main"}
	agent, id := inst.conversationInWindow(99)
	if agent != AgentClaude || id != "main" {
		t.Errorf("unknown window = (%s, %q), want the session's own", agent, id)
	}
}

func TestForkRejectsAStaleUnknownWindowInsteadOfBranchingMain(t *testing.T) {
	t.Parallel()

	inst := &Instance{Agent: AgentClaude, ResumeSessionID: "main"}
	if _, err := inst.ForkSession(99); err == nil {
		t.Fatal("stale window index silently forked the main conversation")
	}
}

// Fork refuses a non-Claude window rather than branching something it cannot.
func TestForkRefusesANonClaudeWindow(t *testing.T) {
	t.Parallel()

	inst := &Instance{
		Agent:           AgentClaude,
		ResumeSessionID: "main",
		FollowedWindows: []FollowedWindow{{Index: 2, Agent: AgentTerminal}},
	}
	if _, err := inst.ForkSession(2); err == nil {
		t.Error("forking a terminal tab was allowed")
	}
}

// A window that has not started a conversation has nothing to branch.
func TestForkRefusesAWindowWithNoConversation(t *testing.T) {
	t.Parallel()

	inst := &Instance{
		Agent:           AgentClaude,
		FollowedWindows: []FollowedWindow{{Index: 2, Agent: AgentClaude}},
	}
	if _, err := inst.ForkSession(2); err == nil {
		t.Error("forking a tab with no conversation was allowed")
	}
}
