package session

import (
	"context"
	"testing"
)

// A terminal on a server never kept its directory: the capture asked only the
// local multiplexer, and server tabs were left out so that it could not
// overwrite them with a local pane's path. They are now asked on the server.

func withServerPanes(t *testing.T, panes map[string]map[string]string) *[]string {
	t.Helper()
	asked := []string{}
	original := newServerPaneDirs
	t.Cleanup(func() { newServerPaneDirs = original })
	newServerPaneDirs = func(*Instance) serverPaneDirQuery {
		return func(_ context.Context, serverID, target string) string {
			asked = append(asked, serverID+" "+target)
			return panes[serverID][target]
		}
	}
	return &asked
}

func TestAServerTabKeepsItsDirectory(t *testing.T) {
	root := t.TempDir()
	inst := &Instance{ID: "mixed", Name: "mixed", Path: root, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 10000, Agent: AgentTerminal, ServerID: "srv"},
			{Index: 10001, Agent: AgentClaude, ServerID: "srv"},
		}}
	name := inst.TmuxSessionName()
	asked := withServerPanes(t, map[string]map[string]string{
		"srv": {name + ":10000": "/srv/app/sub\n", name + ":10001": "/srv/app/sub"},
	})
	local := func(context.Context, string) string { return root }

	dirs := inst.terminalDirsNow(context.Background(), local)
	if dirs[10000] != "/srv/app/sub" {
		t.Errorf("a server tab's directory read as %q", dirs[10000])
	}
	if _, ok := dirs[10001]; ok {
		t.Error("an agent tab on a server was moved")
	}
	if len(*asked) != 1 || (*asked)[0] != "srv "+name+":10000" {
		t.Errorf("asked the server %v", *asked)
	}

	if !inst.captureTerminalWorkingDirs(local) || inst.FollowedWindows[0].WorkDir != "/srv/app/sub" {
		t.Errorf("stopping did not keep the server tab's directory: %q", inst.FollowedWindows[0].WorkDir)
	}
}

// A session that runs on a server: its root is a server path, and a tab back
// there has no directory of its own, as locally.
func TestATerminalInAServerSessionKeepsItsDirectory(t *testing.T) {
	inst := &Instance{ID: "remote", Name: "remote", Path: "/srv/app", Status: StatusRunning, ServerID: "srv",
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal},
			{Index: 2, Agent: AgentTerminal, WorkDir: "/srv/app/old"},
			{Index: 3, Agent: AgentTerminal},
		}}
	name := inst.TmuxSessionName()
	withServerPanes(t, map[string]map[string]string{
		"srv": {name + ":1": "/srv/app/sub", name + ":2": "/srv/app/", name + ":3": "relative"},
	})
	dirs := inst.terminalDirsNow(context.Background(), func(context.Context, string) string { return "/local/elsewhere" })
	if dirs[1] != "/srv/app/sub" {
		t.Errorf("tab 1 read as %q", dirs[1])
	}
	if dir, ok := dirs[2]; !ok || dir != "" {
		t.Errorf("tab 2 back at the server root read as %q (present %v)", dir, ok)
	}
	if _, ok := dirs[3]; ok {
		t.Errorf("a relative answer was kept: %q", dirs[3])
	}
}

// It restarts there: the path is on the server, so it cannot be looked for
// here — looking for it here dropped it.
func TestATerminalInAServerSessionRestartsWhereItWas(t *testing.T) {
	inst := &Instance{ID: "remote", Name: "remote", Path: "/srv/only/there", ServerID: "srv",
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal, WorkDir: "/srv/only/there/sub"},
			{Index: 2, Agent: AgentTerminal},
		}}
	if got := inst.terminalRestartDirArgs(inst.FollowedWindows[0]); len(got) != 2 || got[1] != "/srv/only/there/sub" {
		t.Errorf("restarts with %v, want its own directory", got)
	}
	if got := inst.terminalRestartDirArgs(inst.FollowedWindows[1]); len(got) != 2 || got[1] != "/srv/only/there" {
		t.Errorf("a tab at the root restarts with %v, want the session's path", got)
	}
}

// One listing per server and session, not one question per tab.
func TestServerPanesAreListedOnce(t *testing.T) {
	inst := &Instance{ID: "remote", Name: "remote", Path: "/srv/app", ServerID: "nowhere"}
	query := newServerPaneDirs(inst)
	name := inst.TmuxSessionName()
	for _, idx := range []string{":1", ":2", ":3"} {
		if got := query(context.Background(), "nowhere", name+idx); got != "" {
			t.Errorf("an unreachable server answered %q", got)
		}
	}
}
