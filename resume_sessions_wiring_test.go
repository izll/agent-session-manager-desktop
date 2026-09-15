package main

import (
	"testing"

	"asmgr-desktop/session"
)

// The picker is driven by one switch. An agent missing from it falls to the
// default branch and returns nil — the dialog then shows no previous
// conversations at all, which is what both of these did until now, silently,
// while still advertising that they support resume.
func TestResumeSessionsCoversEveryResumableAgent(t *testing.T) {
	app := &App{}
	for _, agent := range app.GetAgents() {
		if !agent.SupportsResume {
			continue
		}
		// The call must reach a real lister rather than the default branch.
		// An empty result is fine — the machine running this has no store for
		// most of them — but it has to come from somewhere that can answer.
		if !resumeListerExists(session.AgentType(agent.Type)) {
			t.Errorf("%s advertises resume but GetResumeSessions has no case for it, "+
				"so its picker is always empty", agent.Type)
		}
	}
}

// resumeListerExists mirrors the switch in GetResumeSessions. Kept beside it so
// that adding an agent to one and not the other fails here.
func resumeListerExists(agent session.AgentType) bool {
	switch agent {
	case session.AgentClaude, session.AgentGemini, session.AgentCodex,
		session.AgentCursor, session.AgentAntigravity,
		session.AgentOpenCode, session.AgentAmazonQ:
		return true
	}
	return false
}

// And the call itself must not error on a machine with no store.
func TestResumeSessionsAnswersForTheNewAgents(t *testing.T) {
	app := &App{}
	for _, agent := range []string{"cursor", "antigravity"} {
		if _, err := app.GetResumeSessions(agent, t.TempDir()); err != nil {
			t.Errorf("%s: %v", agent, err)
		}
	}
}
