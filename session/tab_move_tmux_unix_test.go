//go:build !windows

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Moving tabs between sessions, against a real multiplexer of the test's own
// (isolatedTmux): what matters is that the process in a moved tab is the same
// process afterwards, and that only the multiplexer can show that.

func startedTerminalSession(t *testing.T, id string, tabs ...FollowedWindow) *Instance {
	t.Helper()
	inst := &Instance{ID: id, Name: id, Path: t.TempDir(), Status: StatusStopped, Agent: AgentTerminal,
		FollowedWindows: tabs}
	if err := inst.Start(); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	t.Cleanup(func() { _ = inst.Stop() })
	return inst
}

func tabNamed(t *testing.T, inst *Instance, name string) FollowedWindow {
	t.Helper()
	for _, fw := range inst.FollowedWindows {
		if fw.Name == name {
			return fw
		}
	}
	t.Fatalf("%s has no tab %q: %+v", inst.ID, name, inst.FollowedWindows)
	return FollowedWindow{}
}

func paneFormat(t *testing.T, tmux func(...string) ([]byte, error), target, format string) string {
	t.Helper()
	out, err := tmux("display-message", "-p", "-t", target, format)
	if err != nil {
		t.Fatalf("display-message %s: %v", target, err)
	}
	return strings.TrimSpace(string(out))
}

func windowIndexes(tmux func(...string) ([]byte, error), session string) []string {
	out, err := tmux("list-windows", "-t", session, "-F", "#{window_index}")
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

func TestMovingARunningTabKeepsItsProcess(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "move-src", FollowedWindow{ID: "tab-a", Name: "build", Agent: AgentTerminal,
		Index: 1, Notes: "remember", TextColor: "#112233", ResumeSessionID: ""})
	dst := startedTerminalSession(t, "move-dst")
	tab := tabNamed(t, src, "build")
	pid := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}")

	move, err := MoveTab(src, dst, tab.Index)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !move.Live || move.TabID != "tab-a" || move.FromTabID != "tab-a" {
		t.Errorf("move = %+v, want a live move keeping the tab's ID", move)
	}
	if got := paneFormat(t, tmux, dst.ID+":"+itoa(move.Index), "#{pane_pid}"); got != pid {
		t.Errorf("the tab runs process %s after the move, %s before: it was restarted", got, pid)
	}
	for _, index := range windowIndexes(tmux, src.ID) {
		if index == itoa(tab.Index) {
			t.Errorf("the source session still has window %d", tab.Index)
		}
	}
	if len(src.FollowedWindows) != 0 {
		t.Errorf("the source still lists %+v", src.FollowedWindows)
	}
	moved := tabNamed(t, dst, "build")
	if moved.ID != "tab-a" || moved.Index != move.Index || moved.Notes != "remember" || moved.TextColor != "#112233" {
		t.Errorf("the tab arrived as %+v", moved)
	}
	if moved.WorkDir != src.Path {
		t.Errorf("the tab's directory is %q, want its old session's %q", moved.WorkDir, src.Path)
	}
	if main, ok := dst.getMainWindowIndex(); !ok || main == move.Index {
		t.Errorf("the target's own window is now %d (ok=%v)", main, ok)
	}
	listed := false
	for _, window := range dst.GetWindowList() {
		if window.Index == move.Index && window.Followed && !window.Dead {
			listed = true
		}
	}
	if !listed {
		t.Errorf("the target's tab bar does not show the moved tab: %+v", dst.GetWindowList())
	}
}

// The terminal attaches through a mirror session linked to the window. It has
// to go with the move: it would otherwise keep showing the tab under the old
// session, and the next tab given the old index would be captured from it.
func TestMovingATabEndsItsMirrorButNotItsProcess(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "mirror-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	dst := startedTerminalSession(t, "mirror-dst")
	tab := tabNamed(t, src, "a")
	mirror := src.ID + "_gui_" + itoa(tab.Index) + "_1"
	if _, err := tmux("new-session", "-d", "-s", mirror); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("link-window", "-k", "-s", src.ID+":"+itoa(tab.Index), "-t", mirror+":"+itoa(tab.Index)); err != nil {
		t.Fatal(err)
	}
	pid := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}")

	move, err := MoveTab(src, dst, tab.Index)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := tmux("has-session", "-t", mirror); err == nil {
		t.Error("the mirror of the moved window is still there")
	}
	if got := paneFormat(t, tmux, dst.ID+":"+itoa(move.Index), "#{pane_pid}"); got != pid {
		t.Errorf("ending the mirror took the process with it (%s, was %s)", got, pid)
	}
}

// A stopped target is brought up around the tab: parked, with its own tabs
// as stopped placeholders, and nothing running but the tab that moved in.
func TestMovingARunningTabIntoAStoppedSessionStartsItParked(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "park-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	dst := &Instance{ID: "park-dst", Name: "park-dst", Path: t.TempDir(), Status: StatusStopped, Agent: AgentTerminal,
		FollowedWindows: []FollowedWindow{{ID: "own", Name: "own", Agent: AgentTerminal, Index: 1}}}
	t.Cleanup(func() { _ = dst.Stop() })
	tab := tabNamed(t, src, "a")
	pid := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}")

	move, err := MoveTab(src, dst, tab.Index)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if dst.Status != StatusRunning || !dst.MainWindowStopped {
		t.Errorf("target status=%s mainStopped=%v, want running with its own window parked", dst.Status, dst.MainWindowStopped)
	}
	if got := paneFormat(t, tmux, dst.ID+":"+itoa(move.Index), "#{pane_pid}"); got != pid {
		t.Errorf("the tab was restarted: pid %s, was %s", got, pid)
	}
	live, ok := dst.LiveTabsContext(context.Background())
	if !ok || len(live) != 1 || live[0] != "tab-a" {
		t.Errorf("running in the target = %v, want only the moved tab", live)
	}
	own := tabNamed(t, dst, "own")
	if !own.Stopped {
		t.Error("the target's own tab came up running")
	}
}

// A stopped tab has nothing to keep: its placeholder goes, and the target
// gets one of its own so that the index is held by a real window.
func TestMovingAStoppedTabMovesItsRecord(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "rec-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	dst := startedTerminalSession(t, "rec-dst")
	tab := tabNamed(t, src, "a")
	if err := src.StopWindow(tab.Index); err != nil {
		t.Fatal(err)
	}
	tab = tabNamed(t, src, "a")

	move, err := MoveTab(src, dst, tab.Index)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if move.Live {
		t.Error("a stopped tab's placeholder was moved as if it ran something")
	}
	if len(windowIndexes(tmux, src.ID)) != 1 {
		t.Errorf("the source keeps windows %v, want only its own", windowIndexes(tmux, src.ID))
	}
	moved := tabNamed(t, dst, "a")
	if !moved.Stopped || moved.Index != move.Index {
		t.Errorf("the tab arrived as %+v", moved)
	}
	if paneFormat(t, tmux, dst.ID+":"+itoa(move.Index), "#{pane_dead}") != "1" {
		t.Error("the target has no parked window at the tab's index")
	}
}

func TestMovingATabBetweenStoppedSessionsMovesTheRecordOnly(t *testing.T) {
	isolatedTmux(t)
	src := &Instance{ID: "s", Name: "s", Path: "/p/s", Status: StatusStopped,
		FollowedWindows: []FollowedWindow{{ID: "x", Name: "x", Index: 1}, {ID: "y", Name: "y", Index: 2}}}
	src.TabOrder = []int{0, 2, 1}
	dst := &Instance{ID: "d", Name: "d", Path: "/p/s", Status: StatusStopped,
		FollowedWindows: []FollowedWindow{{ID: "z", Name: "z", Index: 1}}}

	move, err := MoveTab(src, dst, 2)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if move.Index != 2 || move.Live {
		t.Errorf("move = %+v, want the record at the next free index", move)
	}
	if got := tabNamed(t, dst, "y"); got.WorkDir != "" || got.Stopped {
		t.Errorf("same directory, stopped session: arrived as %+v", got)
	}
	if len(src.TabOrder) != 2 || src.TabOrder[0] != 0 || src.TabOrder[1] != 1 {
		t.Errorf("source tab order = %v, want the moved tab taken out of it", src.TabOrder)
	}
}

func TestMovingRefusals(t *testing.T) {
	isolatedTmux(t)
	src := startedTerminalSession(t, "ref-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	main := src.GetMainWindowIndex()
	other := &Instance{ID: "ref-other", Name: "o", Path: t.TempDir(), Status: StatusStopped}
	onServer := &Instance{ID: "ref-srv", Name: "r", Path: "/srv", Status: StatusStopped, ServerID: "srv1"}

	cases := []struct {
		name  string
		dst   *Instance
		index int
		want  string
	}{
		{"the session's own window", other, main, errTabMoveMainTab},
		{"into the same session", src, tabNamed(t, src, "a").Index, errTabMoveSameSession},
		{"a local tab into a session on a server", onServer, tabNamed(t, src, "a").Index, errTabMoveLocalToSrv},
		{"an index that is no tab", other, 77, errTabMoveNotFound},
	}
	for _, c := range cases {
		if _, err := MoveTab(src, c.dst, c.index); err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.want)
		}
	}
	if len(src.FollowedWindows) != 1 {
		t.Error("a refused move changed the source")
	}
	if got := src.MoveRefusal(tabNamed(t, src, "a").Index, onServer); got != errTabMoveLocalToSrv {
		t.Errorf("MoveRefusal = %q", got)
	}
	if got := src.MoveRefusal(tabNamed(t, src, "a").Index, other); got != "" {
		t.Errorf("MoveRefusal into a local session = %q, want none", got)
	}
}

// The undo a caller uses when its save fails: the window goes back to its old
// index, process and all.
func TestUndoingAMovePutsTheWindowBack(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "undo-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	dst := startedTerminalSession(t, "undo-dst")
	tab := tabNamed(t, src, "a")
	pid := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}")

	move, err := MoveTab(src, dst, tab.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err := UndoTabMoves([]TabMove{move}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}"); got != pid {
		t.Errorf("after the undo the old index runs %s, want %s", got, pid)
	}
}

func TestSplittingARunningTabMakesItASessionOfItsOwn(t *testing.T) {
	tmux := isolatedTmux(t)
	workDir := t.TempDir()
	src := startedTerminalSession(t, "split-src", FollowedWindow{ID: "tab-a", Name: "worker", Agent: AgentTerminal,
		Index: 1, WorkDir: workDir, Notes: "tab note", BackgroundColor: "#445566", AutoYes: true})
	tab := tabNamed(t, src, "worker")
	pid := paneFormat(t, tmux, src.ID+":"+itoa(tab.Index), "#{pane_pid}")

	inst, move, err := SplitTab(src, tab.Index, "split-new", "worker")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	t.Cleanup(func() { _ = inst.Stop() })
	if !move.Live || move.TabID != MainTabID || move.FromTabID != "tab-a" {
		t.Errorf("move = %+v", move)
	}
	if inst.Status != StatusRunning || inst.MainWindowStopped {
		t.Errorf("new session status=%s parked=%v", inst.Status, inst.MainWindowStopped)
	}
	if got := paneFormat(t, tmux, "split-new:"+itoa(move.Index), "#{pane_pid}"); got != pid {
		t.Errorf("the tab was restarted: %s, was %s", got, pid)
	}
	if got := paneFormat(t, tmux, "split-new:"+itoa(move.Index), "#{@asmgr_main}"); got != "1" {
		t.Errorf("the tab's window is not marked as the new session's own (%q)", got)
	}
	if got := windowIndexes(tmux, "split-new"); len(got) != 1 {
		t.Errorf("the new session holds windows %v, want only the tab", got)
	}
	if main, ok := inst.getMainWindowIndex(); !ok || main != move.Index {
		t.Errorf("the new session's own window is %d (ok=%v), want %d", main, ok, move.Index)
	}
	if inst.Path != workDir || inst.Agent != AgentTerminal || !inst.AutoYes ||
		inst.MainTabNotes != "tab note" || inst.TabBackgroundColor != "#445566" || inst.MainWindowName != "worker" {
		t.Errorf("the session was made as %+v", inst)
	}
	if len(src.FollowedWindows) != 0 {
		t.Error("the source still lists the tab")
	}
}

func TestMergingASessionMovesEveryTabAndItsOwnWindow(t *testing.T) {
	tmux := isolatedTmux(t)
	src := startedTerminalSession(t, "merge-src", FollowedWindow{ID: "tab-a", Name: "a", Agent: AgentTerminal, Index: 1})
	src.MainTabNotes = "own note"
	src.Notes = "session note"
	dst := startedTerminalSession(t, "merge-dst")
	dst.Notes = "target note"
	srcMain := src.GetMainWindowIndex()
	mainPid := paneFormat(t, tmux, src.ID+":"+itoa(srcMain), "#{pane_pid}")
	tabPid := paneFormat(t, tmux, src.ID+":"+itoa(tabNamed(t, src, "a").Index), "#{pane_pid}")
	dstMain := dst.GetMainWindowIndex()

	moves, err := MergeSession(src, dst)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(moves) != 2 {
		t.Fatalf("moves = %+v", moves)
	}
	if _, err := tmux("has-session", "-t", src.ID); err == nil {
		t.Error("the merged session is still in the multiplexer")
	}
	own := moves[1]
	if own.FromTabID != MainTabID || own.TabID == MainTabID || own.TabID == "" {
		t.Errorf("the own window moved as %+v, want a new tab ID", own)
	}
	if got := paneFormat(t, tmux, dst.ID+":"+itoa(own.Index), "#{pane_pid}"); got != mainPid {
		t.Errorf("the merged session's own agent was restarted: %s, was %s", got, mainPid)
	}
	if got := paneFormat(t, tmux, dst.ID+":"+itoa(moves[0].Index), "#{pane_pid}"); got != tabPid {
		t.Errorf("the merged session's tab was restarted: %s, was %s", got, tabPid)
	}
	if main, ok := dst.getMainWindowIndex(); !ok || main != dstMain {
		t.Errorf("the target's own window is %d (ok=%v), want %d still", main, ok, dstMain)
	}
	ownTab := tabNamed(t, dst, "merge-src")
	if ownTab.ID != own.TabID || ownTab.Notes != "own note" {
		t.Errorf("the own window arrived as %+v", ownTab)
	}
	if !strings.Contains(dst.Notes, "target note") || !strings.Contains(dst.Notes, "# merge-src\nsession note") {
		t.Errorf("the session notes were not combined: %q", dst.Notes)
	}
	if len(src.FollowedWindows) != 0 || src.Status != StatusStopped {
		t.Errorf("the merged session was left as %+v", src)
	}
}

// A tab on a server stays on it. Moved into a local session, it becomes that
// session's tab on the server, in the server's band; the move happens in the
// server's multiplexer, here a second isolated tmux standing in for one.
func TestMovingAServerTabKeepsItOnTheServer(t *testing.T) {
	isolatedTmux(t)
	server := fakeServerTmux(t)
	src := startedTerminalSession(t, "srvtab-src")
	dst := startedTerminalSession(t, "srvtab-dst")
	SetTabExecutor(src.ID, "srv1", server)
	SetTabExecutor(dst.ID, "srv1", server)
	t.Cleanup(func() { ClearTabExecutor(src.ID, "srv1"); ClearTabExecutor(dst.ID, "srv1") })

	index, err := src.NewWindowWithNameOn("srv1", "remote", "/")
	if err != nil {
		t.Fatalf("server tab: %v", err)
	}
	pid := paneFormat(t, server.tmux, src.ID+":"+itoa(index), "#{pane_pid}")

	move, err := MoveTab(src, dst, index)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if move.Index < remoteWindowIndexBase || !move.Live {
		t.Errorf("move = %+v, want a live move into the server's band", move)
	}
	if got := paneFormat(t, server.tmux, dst.ID+":"+itoa(move.Index), "#{pane_pid}"); got != pid {
		t.Errorf("the server tab was restarted: %s, was %s", got, pid)
	}
	if moved := tabNamed(t, dst, "remote"); moved.ServerID != "srv1" {
		t.Errorf("the tab lost its server: %+v", moved)
	}
}

// fakeServer is a second multiplexer of the test's own, reached as a server
// would be: through an executor.
type fakeServer struct {
	shim string
}

func (f fakeServer) Run(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, f.shim, args...).Run()
}

func (f fakeServer) Output(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, f.shim, args...).Output()
}

func (f fakeServer) Describe() string { return "fake server" }

func (f fakeServer) tmux(args ...string) ([]byte, error) {
	return exec.Command(f.shim, args...).Output()
}

func fakeServerTmux(t *testing.T) fakeServer {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "srv-tmux")
	script := "#!/bin/sh\nexec '" + real + "' -S '" + filepath.Join(dir, "s") + "' -f /dev/null \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command(shim, "kill-server").Run() })
	return fakeServer{shim: shim}
}

func itoa(n int) string { return strconv.Itoa(n) }
