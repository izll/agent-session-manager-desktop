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
