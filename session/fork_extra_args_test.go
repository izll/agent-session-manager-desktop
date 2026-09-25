package session

import (
	"strings"
	"testing"
)

// A forked tab is the same conversation with the same setup, so it has to be
// started the same way — including the session's extra arguments.
//
// NewForkedTab passed an empty string where every other path passes them, so a
// branch ran with a differently-configured agent than the tab it came from:
// no --dangerously-skip-permissions, no --model, whatever the user had set.
// ForkToNewSession does carry them, which is what made the difference visible
// — the same fork behaved differently depending on where it was sent.
func TestForkedTabCarriesExtraArgs(t *testing.T) {
	body := functionBody(t, readSource(t, "instance.go"),
		"func (i *Instance) NewForkedTab(")

	if !strings.Contains(body, "i.agentArgv(config, i.ServerID, args, i.ExtraArgs)") {
		t.Error("NewForkedTab does not pass the session's extra arguments to the " +
			"forked agent, so the branch starts differently configured from the " +
			"tab it was forked from")
	}
}
