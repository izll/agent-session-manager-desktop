package session

import (
	"strings"
	"testing"
)

// Restarting a session rebuilt every tab in the local multiplexer, servers'
// tabs included, and recorded the local index it was handed while keeping the
// server. The tab then named a server that had never had its window, and
// clicking it did nothing. Found on a real session: a terminal tab on a server
// stored as index 8, with the local multiplexer holding a plain shell there.
func TestRestartingASessionLeavesServersTabsToTheirServers(t *testing.T) {
	local := &recordingExecutor{output: []byte("5\n")}
	inst := &Instance{
		ID:     "restart-remote",
		Name:   "restart-remote",
		Path:   "/tmp/project",
		Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal, Name: "here"},
			{Index: remoteWindowIndexBase, Agent: AgentClaude, Name: "there",
				ServerID: "srv", ResumeSessionID: "kept-conversation"},
			{Index: 8, Agent: AgentTerminal, Name: "stray", ServerID: "srv"},
		},
	}
	SetExecutor(inst.ID, local)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	inst.restoreFollowedWindows(allWindows)

	for _, command := range local.seen() {
		joined := strings.Join(command, " ")
		if command[0] == "new-window" &&
			(strings.Contains(joined, "there") || strings.Contains(joined, "stray")) {
			t.Errorf("a server's tab was rebuilt on this computer: %s", joined)
		}
	}

	byName := map[string]FollowedWindow{}
	for _, fw := range inst.FollowedWindows {
		byName[fw.Name] = fw
	}
	if len(byName) != 3 {
		t.Fatalf("tabs after restart: %+v", inst.FollowedWindows)
	}

	there := byName["there"]
	if there.Index != remoteWindowIndexBase || there.ServerID != "srv" {
		t.Errorf("a healthy server tab changed: %+v", there)
	}
	// The server had not answered yet, which is the usual state straight
	// after starting; that must not cost the tab its conversation.
	if there.ResumeSessionID != "kept-conversation" {
		t.Errorf("the saved conversation was dropped while the server was "+
			"unreachable: %q", there.ResumeSessionID)
	}

	stray := byName["stray"]
	if stray.ServerID != "srv" {
		t.Errorf("the stray tab lost its server: %+v", stray)
	}
	if stray.Index < remoteWindowIndexBase {
		t.Errorf("the stray tab still holds local index %d, so its attach keeps "+
			"asking the server for a window it never had", stray.Index)
	}
	if stray.Index == there.Index {
		t.Errorf("the stray tab was moved onto the index another tab holds (%d)",
			stray.Index)
	}

	if here := byName["here"]; here.ServerID != "" || here.Index != 5 {
		t.Errorf("the local tab was not rebuilt locally as before: %+v", here)
	}
}

// A command meant for a server whose connection is not open used to run on this
// computer. For a session's own tabs that does not fail harmlessly: they share
// the session's name, so the local multiplexer has a session of that name and
// the command succeeds — building the server's window here.
func TestAServerWithNoConnectionIsRefusedNotRunHere(t *testing.T) {
	local := &recordingExecutor{}
	inst := &Instance{ID: "no-route", FollowedWindows: []FollowedWindow{
		{Index: remoteWindowIndexBase, ServerID: "srv"},
	}}
	SetExecutor(inst.ID, local)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	err := inst.tmuxRunOn("srv", "new-window", "-t", "no-route:10000")
	if err == nil {
		t.Fatal("a command for an unconnected server succeeded")
	}
	if !strings.Contains(err.Error(), "error.serverNotConnected") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if len(local.seen()) != 0 {
		t.Errorf("the server's command ran on this computer: %v", local.seen())
	}
	if inst.WindowReachable(remoteWindowIndexBase) {
		t.Error("a tab on an unconnected server is reported reachable")
	}

	remote := &recordingExecutor{}
	SetTabExecutor(inst.ID, "srv", remote)
	t.Cleanup(func() { ClearTabExecutor(inst.ID, "srv") })
	if err := inst.tmuxRunOn("srv", "has-session"); err != nil {
		t.Fatalf("once connected, the command was still refused: %v", err)
	}
	if len(remote.seen()) != 1 || !inst.WindowReachable(remoteWindowIndexBase) {
		t.Error("once connected, the command did not go to the server")
	}
}

// A server's session outlives any one of its tabs, so asking only whether the
// session exists called a tab alive whose window was gone. The pane then had
// no way to tell a tab waiting to be started from one that was running.
func TestAServerTabIsAliveOnlyIfItsWindowIs(t *testing.T) {
	remote := &recordingExecutor{}
	inst := &Instance{ID: "alive-check", Name: "alive-check", FollowedWindows: []FollowedWindow{
		{Index: remoteWindowIndexBase, ServerID: "srv"},
	}}
	SetTabExecutor(inst.ID, "srv", remote)
	t.Cleanup(func() { ClearTabExecutor(inst.ID, "srv") })

	inst.windowAliveContext(t.Context(), remoteWindowIndexBase)

	seen := remote.seen()
	if len(seen) != 1 {
		t.Fatalf("commands sent: %v", seen)
	}
	want := inst.TmuxSessionName() + ":10000"
	if got := strings.Join(seen[0], " "); !strings.HasSuffix(got, want) {
		t.Errorf("asked %q; the check has to name the window (%s), not only "+
			"the session", got, want)
	}
}
