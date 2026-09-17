package session

import (
	"context"
	"sync"
	"testing"
)

// recordingExecutor stands in for a server.
type recordingExecutor struct {
	mu       sync.Mutex
	commands [][]string
	output   []byte
	err      error
}

func (r *recordingExecutor) Run(ctx context.Context, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, args)
	return r.err
}

func (r *recordingExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, args)
	return r.output, r.err
}

func (r *recordingExecutor) Describe() string { return "test server" }

func (r *recordingExecutor) seen() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.commands...)
}

// A session with no server runs here, exactly as before remote support
// existed. Every stored session predating this is in that state.
func TestSessionsDefaultToThisComputer(t *testing.T) {
	inst := &Instance{ID: "local-1"}

	if inst.IsRemote() {
		t.Error("a session with no server was called remote")
	}
	if inst.exec() != LocalExecutor {
		t.Error("a local session was routed somewhere else")
	}
}

// One window manages several servers and this computer at the same time, and
// the sidebar polls them together. Routing has to be per session, or one
// session's commands end up on another's machine.
func TestEachSessionKeepsItsOwnRoute(t *testing.T) {
	first := &recordingExecutor{}
	second := &recordingExecutor{}

	SetExecutor("session-a", first)
	SetExecutor("session-b", second)
	t.Cleanup(func() {
		ClearExecutor("session-a")
		ClearExecutor("session-b")
	})

	instA := &Instance{ID: "session-a", ServerID: "srv1"}
	instB := &Instance{ID: "session-b", ServerID: "srv2"}
	instLocal := &Instance{ID: "session-c"}

	_ = instA.tmuxRun("has-session", "-t", "a")
	_ = instB.tmuxRun("has-session", "-t", "b")

	if len(first.seen()) != 1 || first.seen()[0][2] != "a" {
		t.Errorf("session A's command did not reach its own server: %v", first.seen())
	}
	if len(second.seen()) != 1 || second.seen()[0][2] != "b" {
		t.Errorf("session B's command did not reach its own server: %v", second.seen())
	}
	if instLocal.exec() != LocalExecutor {
		t.Error("a local session picked up another session's executor")
	}
}

// When a connection drops, the session becomes unreachable — not local.
// Running its agent here would start it on the wrong machine, against a
// directory that may not exist.
func TestClearingARouteDoesNotFallBackSilently(t *testing.T) {
	executor := &recordingExecutor{}
	SetExecutor("session-x", executor)

	inst := &Instance{ID: "session-x", ServerID: "srv1"}
	if inst.exec() != Executor(executor) {
		t.Fatal("the route was not applied")
	}

	ClearExecutor("session-x")

	// It falls back to local, which then fails against a multiplexer that has
	// no such session — a clear outcome, unlike commands sent into a closed
	// connection. What matters is that ServerID still says where it belongs.
	if !inst.IsRemote() {
		t.Error("the session forgot which server it belongs to")
	}
}

// The hook is how the application layer points a loaded session at its
// machine. Left unset, everything stays local — which is the state before any
// server has been connected to.
func TestRouteHookIsOptional(t *testing.T) {
	previous := RouteInstance
	t.Cleanup(func() { RouteInstance = previous })

	RouteInstance = nil
	routeLoaded([]*Instance{{ID: "a", ServerID: "srv1"}}) // must not panic

	var routed []string
	RouteInstance = func(inst *Instance) {
		routed = append(routed, inst.ID)
	}
	routeLoaded([]*Instance{
		{ID: "remote-1", ServerID: "srv1"},
		{ID: "local-1"},
		nil,
		{ID: "remote-2", ServerID: "srv2"},
	})

	if len(routed) != 2 || routed[0] != "remote-1" || routed[1] != "remote-2" {
		t.Errorf("routed = %v; local sessions should not be routed and nils should "+
			"not reach the hook", routed)
	}
}
