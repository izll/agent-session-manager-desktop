package main

import (
	"context"
	"errors"
	"testing"

	"asmgr-desktop/session"
)

type refusingExecutor struct{}

func (refusingExecutor) Run(context.Context, ...string) error { return errors.New("no window") }
func (refusingExecutor) Output(context.Context, ...string) ([]byte, error) {
	return nil, errors.New("no window")
}
func (refusingExecutor) Describe() string { return "test server" }

// After this computer restarts a session, a tab on a server comes back without
// a window there until it is started. The pane used to show that as an attach
// error; it is a tab waiting to be started, and the poll reports it as such.
func TestATabMissingOnItsServerIsReportedAsMissing(t *testing.T) {
	inst := &session.Instance{ID: "missing-status", FollowedWindows: []session.FollowedWindow{
		{Index: 3, Agent: session.AgentTerminal},
		{Index: 10000, Agent: session.AgentTerminal, ServerID: "srv"},
	}}

	// Not connected: that is "unreachable", which has its own text.
	if tabWindowMissing(inst, 10000, false) {
		t.Error("a tab on an unconnected server was called missing")
	}

	session.SetTabExecutor(inst.ID, "srv", refusingExecutor{})
	t.Cleanup(func() { session.ClearTabExecutor(inst.ID, "srv") })

	if !tabWindowMissing(inst, 10000, false) {
		t.Error("a connected server without the tab's window was not reported")
	}
	if tabWindowMissing(inst, 10000, true) {
		t.Error("a tab whose window answered was called missing")
	}
	// A local tab has no server to be waiting on.
	if tabWindowMissing(inst, 3, false) {
		t.Error("a local tab was called missing on a server")
	}
}
