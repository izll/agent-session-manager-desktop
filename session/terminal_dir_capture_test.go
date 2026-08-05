package session

import (
	"context"
	"path/filepath"
	"testing"
)

// A terminal tab is a place you navigate to. Stopping the session used to lose
// that, so coming back meant finding your way to the subdirectory again.
func TestTerminalTabRemembersWhereItWasLeft(t *testing.T) {
	sub := t.TempDir()
	root := t.TempDir()

	inst := &Instance{
		Status: StatusRunning,
		Path:   root,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal},
		},
	}

	changed := inst.captureTerminalWorkingDirs(func(context.Context, string) string {
		return sub + "\n" // tmux terminates its answer with a newline
	})

	if !changed {
		t.Fatal("capture reported no change")
	}
	if got := inst.FollowedWindows[0].WorkDir; got != sub {
		t.Errorf("WorkDir = %q, want %q", got, sub)
	}
}

// For an agent the working directory is part of what identifies the session:
// the conversation it resumes and the diff it shows are both anchored to it, so
// a tab navigated elsewhere would come back pointing at the wrong project.
func TestAgentTabsKeepTheirConfiguredDirectory(t *testing.T) {
	elsewhere := t.TempDir()
	root := t.TempDir()

	inst := &Instance{
		Status: StatusRunning,
		Path:   root,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentClaude},
			{Index: 2, Agent: AgentCodex},
		},
	}

	changed := inst.captureTerminalWorkingDirs(func(context.Context, string) string {
		return elsewhere
	})

	if changed {
		t.Error("an agent tab had its directory rewritten")
	}
	for _, w := range inst.FollowedWindows {
		if w.WorkDir != "" {
			t.Errorf("%s tab got WorkDir %q, want it left alone", w.Agent, w.WorkDir)
		}
	}
}

// A pane whose directory was deleted keeps reporting the old path. Storing it
// would make a tab fail to start where it used to simply work.
func TestVanishedDirectoryIsNotStored(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "deleted-since")

	inst := &Instance{
		Status:          StatusRunning,
		Path:            root,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal, WorkDir: root}},
	}

	if inst.captureTerminalWorkingDirs(func(context.Context, string) string { return gone }) {
		t.Error("a directory that no longer exists was stored")
	}
	if inst.FollowedWindows[0].WorkDir != root {
		t.Errorf("the previous directory was overwritten: %q", inst.FollowedWindows[0].WorkDir)
	}
}

// The session's own path is what "no directory of its own" already means.
// Writing it out would pin an inherited path, so moving the session would leave
// its terminals pointing at the old location.
func TestSessionRootIsLeftUnset(t *testing.T) {
	root := t.TempDir()

	inst := &Instance{
		Status:          StatusRunning,
		Path:            root,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}},
	}

	if inst.captureTerminalWorkingDirs(func(context.Context, string) string { return root }) {
		t.Error("the session root was stored as an explicit tab directory")
	}
	if got := inst.FollowedWindows[0].WorkDir; got != "" {
		t.Errorf("WorkDir = %q, want it left empty", got)
	}
}

// A stopped session has no panes to ask, and a stopped tab's pane is already
// gone — asking would either fail or return something stale.
func TestNothingIsCapturedWithoutLivePanes(t *testing.T) {
	dir := t.TempDir()

	stopped := &Instance{
		Status:          StatusStopped,
		Path:            t.TempDir(),
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}},
	}
	if stopped.captureTerminalWorkingDirs(func(context.Context, string) string { return dir }) {
		t.Error("a stopped session was queried")
	}

	stoppedTab := &Instance{
		Status:          StatusRunning,
		Path:            t.TempDir(),
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal, Stopped: true}},
	}
	if stoppedTab.captureTerminalWorkingDirs(func(context.Context, string) string { return dir }) {
		t.Error("a stopped tab was queried")
	}
}

// Stopping one tab must not rewrite the others.
func TestSingleTabCaptureTouchesOnlyThatTab(t *testing.T) {
	sub := t.TempDir()

	inst := &Instance{
		Status: StatusRunning,
		Path:   t.TempDir(),
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal},
			{Index: 2, Agent: AgentTerminal},
		},
	}

	if !inst.captureTerminalWorkingDir(2, func(context.Context, string) string { return sub }) {
		t.Fatal("capture reported no change")
	}
	if inst.FollowedWindows[0].WorkDir != "" {
		t.Errorf("the other tab was modified: %q", inst.FollowedWindows[0].WorkDir)
	}
	if got := inst.FollowedWindows[1].WorkDir; got != sub {
		t.Errorf("WorkDir = %q, want %q", got, sub)
	}
}
