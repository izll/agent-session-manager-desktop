package session

import (
	"strings"
	"testing"
)

// Antigravity has two resume flags and only one of them takes an id.
//
// --continue reopens the most recent conversation and accepts no argument;
// --conversation <id> reopens a named one. With --continue configured, the id
// every resume here carries was appended as a bare argument, and agy reads a
// bare argument as a prompt: it printed
//
//	Error: unexpected argument "588acb84-...".
//
// and exited before drawing anything. The tab looked started and empty, and
// with no process there was no presence lock to read the conversation id back
// from — which is how this surfaced as "the status bar shows no id".
func TestAntigravityResumesByConversationID(t *testing.T) {
	cfg := AgentConfigs[AgentAntigravity]

	if cfg.ResumeFlag != "--conversation" {
		t.Errorf("resume flag = %q; --continue and friends take no id, so the id "+
			"is passed as a prompt and the agent exits", cfg.ResumeFlag)
	}
	// The id follows the flag as its value, so this must not be a subcommand:
	// as one, the flag would be placed first and the auto-yes flag between it
	// and its own id.
	if cfg.ResumeIsSubcommand {
		t.Error("resume treated as a subcommand; the id would be separated from its flag")
	}
}

// The flag pair is easy to swap back by accident, since --continue is the one
// the documentation leads with. Pin what each one means.
func TestAntigravityContinueFlagIsNotUsedForIDs(t *testing.T) {
	cfg := AgentConfigs[AgentAntigravity]
	if strings.Contains(cfg.ResumeFlag, "continue") {
		t.Error("--continue takes no conversation id; resuming a named conversation " +
			"needs --conversation")
	}
}
