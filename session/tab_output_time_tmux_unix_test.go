//go:build !windows

package session

import (
	"context"
	"testing"
	"time"
)

// Against a real server: what makes the time move, and what must not.
func TestATerminalTabsOutputMovesTheTimeAndAnAgentsDoesNot(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{
		ID: "output", Name: "output", Path: t.TempDir(), Agent: AgentClaude,
		FollowedWindows: []FollowedWindow{{ID: "shell", Index: 1, Agent: AgentTerminal}},
	}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-window", "-d", "-t", name+":1", "sh"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	time.Sleep(100 * time.Millisecond)
	before := inst.LastTerminalOutputContext(ctx)
	if before.IsZero() {
		t.Fatal("no time for a tab that started")
	}

	// tmux keeps whole seconds.
	time.Sleep(1100 * time.Millisecond)
	if _, err := tmux("send-keys", "-t", name+":0", "echo agent", "Enter"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := inst.LastTerminalOutputContext(ctx); !got.Equal(before) {
		t.Fatalf("the agent's window moved it: %v, was %v", got, before)
	}

	if _, err := tmux("capture-pane", "-p", "-t", name+":1"); err != nil {
		t.Fatal(err)
	}
	if got := inst.LastTerminalOutputContext(ctx); !got.Equal(before) {
		t.Fatalf("looking at the tab moved it: %v, was %v", got, before)
	}

	if _, err := tmux("send-keys", "-t", name+":1", "echo shell", "Enter"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !inst.LastTerminalOutputContext(ctx).After(before) {
		if time.Now().After(deadline) {
			t.Fatal("the shell's output did not move it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// With no terminal tab there is nothing to ask, and a poll runs for every session.
func TestASessionWithoutATerminalAsksNothing(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{ID: "agents", Name: "agents", Agent: AgentClaude, FollowedWindows: []FollowedWindow{{ID: "a", Index: 1, Agent: AgentClaude}}}
	if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-window", "-d", "-t", inst.TmuxSessionName()+":1", "sh"); err != nil {
		t.Fatal(err)
	}
	if got := inst.LastTerminalOutputContext(context.Background()); !got.IsZero() {
		t.Fatalf("got %v", got)
	}
}

// A session whose own window is a shell counts it too.
func TestASessionThatIsAShellCounts(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{ID: "shell", Name: "shell", Agent: AgentTerminal}
	if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sh"); err != nil {
		t.Fatal(err)
	}
	if inst.LastTerminalOutputContext(context.Background()).IsZero() {
		t.Fatal("no time for the session's own shell")
	}
}

// A tab on a server has an index that belongs to that machine. Here it would be read as
// whatever window has that index locally.
func TestATabOnAServerIsNotInTheListing(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{
		ID: "mixed", Name: "mixed", Agent: AgentClaude,
		FollowedWindows: []FollowedWindow{{ID: "away", Index: 1, Agent: AgentClaude, ServerID: "srv"}},
	}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-window", "-d", "-t", name+":1", "sh"); err != nil {
		t.Fatal(err)
	}
	states := inst.TabStatesContext(context.Background())
	if _, here := states[0]; !here {
		t.Fatalf("states = %v", states)
	}
	if _, here := states[1]; here {
		t.Fatalf("the server's tab was answered from here: %v", states)
	}
}

// One listing for all, read exactly as asking each would have been.
func TestASnapshotAnswersAsAskingWould(t *testing.T) {
	tmux := isolatedTmux(t)
	make := func(id string) *Instance {
		inst := &Instance{ID: id, Name: id, Agent: AgentClaude, Status: StatusRunning,
			FollowedWindows: []FollowedWindow{{ID: "t", Index: 1, Agent: AgentTerminal}}}
		if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sh"); err != nil {
			t.Fatal(err)
		}
		if _, err := tmux("new-window", "-d", "-t", inst.TmuxSessionName()+":1", "sh"); err != nil {
			t.Fatal(err)
		}
		return inst
	}
	first, second := make("one"), make("two")
	away := &Instance{ID: "away", Name: "away", ServerID: "srv", Status: StatusRunning}
	gone := &Instance{ID: "gone", Name: "gone", Status: StatusRunning}
	ctx := context.Background()

	wantStates := first.TabStatesContext(ctx)
	wantMain := first.GetMainWindowIndex()
	if len(wantStates) != 2 {
		t.Fatalf("states = %v", wantStates)
	}
	// A server's session could share its name with one here.
	if _, err := tmux("new-session", "-d", "-s", away.TmuxSessionName(), "sh"); err != nil {
		t.Fatal(err)
	}
	SnapshotLocalWindows(ctx, []*Instance{away, first, second, gone})
	if first.windowSnapshot == nil || second.windowSnapshot == nil {
		t.Fatal("no snapshot")
	}
	if away.windowSnapshot != nil || gone.windowSnapshot != nil {
		t.Fatal("a snapshot for what the listing does not cover")
	}
	got := first.TabStatesContext(ctx)
	if len(got) != 2 || got[0].Identity != wantStates[0].Identity || got[1].LastOutput != wantStates[1].LastOutput {
		t.Fatalf("states = %v, want %v", got, wantStates)
	}
	// The other session's windows do not leak in, though they share the indexes.
	if got[0].Identity == second.TabStatesContext(ctx)[0].Identity {
		t.Fatal("two sessions read as one")
	}
	if main := first.GetMainWindowIndex(); main != wantMain {
		t.Fatalf("main = %d, want %d", main, wantMain)
	}
}

// A listing that names sessions must name them exactly: has-session would also match
// on a prefix, and this must not say a stopped session runs because a longer one does.
func TestStatusesFromOneListing(t *testing.T) {
	tmux := isolatedTmux(t)
	running := &Instance{ID: "a", Name: "abc"}
	stopped := &Instance{ID: "b", Name: "ab", Status: StatusRunning}
	away := &Instance{ID: "c", Name: "c", ServerID: "srv", Status: StatusRunning}
	if _, err := tmux("new-session", "-d", "-s", running.TmuxSessionName(), "sh"); err != nil {
		t.Fatal(err)
	}
	if rest := refreshLocalStatusesTogether(context.Background(), []*Instance{running, stopped, away, nil}); len(rest) != 1 || rest[0] != away {
		t.Fatalf("rest = %v", rest)
	}
	if running.Status != StatusRunning || stopped.Status != StatusStopped {
		t.Fatalf("running = %v, stopped = %v", running.Status, stopped.Status)
	}
	if away.Status != StatusRunning {
		t.Fatalf("a server's session was answered from here: %v", away.Status)
	}
}

// With no server at all the listing fails, and each is asked, which gives stopped.
func TestStatusesWithNoServerRunning(t *testing.T) {
	isolatedTmux(t)
	inst := &Instance{ID: "n", Name: "n", Status: StatusRunning}
	rest := refreshLocalStatusesTogether(context.Background(), []*Instance{inst})
	if len(rest) != 1 || rest[0] != inst {
		t.Fatalf("rest = %v", rest)
	}
	refreshInstanceStatusesContext(context.Background(), []*Instance{inst})
	if inst.Status != StatusStopped {
		t.Fatalf("status = %v", inst.Status)
	}
}

// The marker is what says which window is the session's own, when it is not the first.
func TestASnapshotKnowsTheMainWindowByItsMarker(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{ID: "marked", Name: "marked", Agent: AgentClaude, Status: StatusRunning}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-window", "-d", "-t", name+":3", "sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-w", "-t", name+":3", "@asmgr_main", "1"); err != nil {
		t.Fatal(err)
	}
	SnapshotLocalWindows(context.Background(), []*Instance{inst})
	if main := inst.GetMainWindowIndex(); main != 3 {
		t.Fatalf("main = %d", main)
	}
}
