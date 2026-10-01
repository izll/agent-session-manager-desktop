//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// The App half of moving tabs, end to end against a multiplexer of the test's
// own: the move itself, and everything that names the tab elsewhere — its
// tasks, the running record, the quick-jump list — following it.

// appTestTmux points the session package at a tmux server of the test's own,
// on a socket in a temporary directory. The user's own server is never asked.
func appTestTmux(t *testing.T) func(args ...string) string {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	previous := session.TmuxBinary()
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nexec '" + real + "' -S '" + filepath.Join(dir, "s") + "' -f /dev/null \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "")
	session.SetTmuxBinary(shim)
	t.Cleanup(func() {
		_ = exec.Command(shim, "kill-server").Run()
		session.SetTmuxBinary(previous)
	})
	return func(args ...string) string {
		out, _ := exec.Command(shim, args...).Output()
		return strings.TrimSpace(string(out))
	}
}

type tabMoveFixture struct {
	app     *App
	storage *session.Storage
	tmux    func(args ...string) string
}

func newTabMoveFixture(t *testing.T) *tabMoveFixture {
	t.Helper()
	tmux := appTestTmux(t)
	app, storage, worker := projectTasksApp(t)
	if err := storage.RemoveInstance(worker.ID); err != nil {
		t.Fatal(err)
	}
	return &tabMoveFixture{app: app, storage: storage, tmux: tmux}
}

// session adds a terminal session with the given tabs, started when running.
func (f *tabMoveFixture) session(t *testing.T, id string, running bool, tabs ...session.FollowedWindow) *session.Instance {
	t.Helper()
	inst := &session.Instance{ID: id, Name: id, Path: t.TempDir(), Status: session.StatusStopped,
		Agent: session.AgentTerminal, FollowedWindows: tabs}
	if running {
		if err := inst.Start(); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		t.Cleanup(func() { _ = inst.Stop() })
	}
	if err := f.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

func (f *tabMoveFixture) stored(t *testing.T, id string) *session.Instance {
	t.Helper()
	instances, _, err := f.storage.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range instances {
		if inst.ID == id {
			return inst
		}
	}
	return nil
}

func (f *tabMoveFixture) pid(target string) string {
	return f.tmux("display-message", "-p", "-t", target, "#{pane_pid}")
}

func (f *tabMoveFixture) task(t *testing.T, inst *session.Instance, title, tabID string) string {
	t.Helper()
	manager, err := cachedTaskManager(inst.Path)
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.CreateTaskForSession(title, "", session.TaskPriorityMedium, nil, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tabID != "" {
		if err := manager.UpdateTask(task.ID, map[string]interface{}{"tabId": tabID}); err != nil {
			t.Fatal(err)
		}
	}
	return task.ID
}

func tasksOf(t *testing.T, inst *session.Instance) map[string]session.Task {
	t.Helper()
	manager := session.NewTaskManager(inst.Path)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]session.Task{}
	for _, task := range manager.GetTasks() {
		byTitle[task.Title] = task
	}
	return byTitle
}

func tabIndex(t *testing.T, inst *session.Instance, id string) int {
	t.Helper()
	for _, fw := range inst.FollowedWindows {
		if fw.ID == id {
			return fw.Index
		}
	}
	t.Fatalf("%s has no tab %s", inst.ID, id)
	return -1
}

func TestMoveTabToSessionTakesItsTasksQuickJumpAndRunningMark(t *testing.T) {
	f := newTabMoveFixture(t)
	src := f.session(t, "src", true, session.FollowedWindow{ID: "tab-a", Name: "build", Agent: session.AgentTerminal, Index: 1})
	dst := f.session(t, "dst", true)
	index := tabIndex(t, src, "tab-a")
	pid := f.pid("src:" + strconv.Itoa(index))
	f.task(t, src, "for the tab", "tab-a")
	f.task(t, src, "for the session", "")
	if err := f.storage.UpdateSettings(func(s *session.Settings) {
		s.QuickJump = []session.QuickJumpEntry{{SessionID: "src", WindowIdx: index, Label: "build"}}
	}); err != nil {
		t.Fatal(err)
	}
	seedRunning(t, f.storage, map[string]session.RunningRecord{"src": {Tabs: []string{session.MainTabID, "tab-a"}}})

	result, err := f.app.MoveTabToSession("src", index, "dst", "")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if result.SessionID != "dst" || result.TabsMoved != 1 || result.TasksMoved != 1 {
		t.Errorf("result = %+v", result)
	}
	if got := f.pid("dst:" + strconv.Itoa(result.WindowIdx)); got != pid {
		t.Errorf("the tab was restarted: pid %s, was %s", got, pid)
	}
	if storedSrc := f.stored(t, "src"); len(storedSrc.FollowedWindows) != 0 {
		t.Errorf("the source still stores %+v", storedSrc.FollowedWindows)
	}
	if storedDst := f.stored(t, "dst"); tabIndex(t, storedDst, "tab-a") != result.WindowIdx {
		t.Errorf("the target stores the tab at another index")
	}

	moved := tasksOf(t, dst)["for the tab"]
	if moved.SessionID != "dst" || moved.TabID != "tab-a" {
		t.Errorf("the tab's task is %+v, want it in the target, still on the tab", moved)
	}
	if _, left := tasksOf(t, src)["for the tab"]; left {
		t.Error("the tab's task is still in the source's list")
	}
	if stayed := tasksOf(t, src)["for the session"]; stayed.SessionID != "src" {
		t.Errorf("a task of the session itself moved: %+v", stayed)
	}

	_, _, settings, err := f.storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if want := []session.QuickJumpEntry{{SessionID: "dst", WindowIdx: result.WindowIdx, Label: "build"}}; !reflect.DeepEqual(settings.QuickJump, want) {
		t.Errorf("quick jump = %+v, want %+v", settings.QuickJump, want)
	}
	running := loadRunning(t, f.storage)
	if !reflect.DeepEqual(running["src"].Tabs, []string{session.MainTabID}) || !reflect.DeepEqual(running["dst"].Tabs, []string{"tab-a"}) {
		t.Errorf("running record = %+v", running)
	}
}

func TestMoveTabToSessionRefusesAndChangesNothing(t *testing.T) {
	f := newTabMoveFixture(t)
	src := f.session(t, "src", true, session.FollowedWindow{ID: "tab-a", Name: "a", Agent: session.AgentTerminal, Index: 1})
	remote := &session.Instance{ID: "remote", Name: "remote", Path: "/srv", Status: session.StatusStopped, ServerID: "srv1"}
	if err := f.storage.AddInstance(remote); err != nil {
		t.Fatal(err)
	}
	index := tabIndex(t, src, "tab-a")

	if _, err := f.app.MoveTabToSession("src", index, "remote", ""); err == nil || err.Error() != "error.tabMoveLocalTabToServer" {
		t.Errorf("err = %v, want the local-tab-to-server refusal", err)
	}
	if _, err := f.app.MoveTabToSession("src", src.GetMainWindowIndex(), "remote", ""); err == nil {
		t.Error("the session's own window was moved")
	}
	if _, err := f.app.MoveTabToSession("src", index, "src", ""); err == nil {
		t.Error("a tab was moved into its own session")
	}
	if _, err := f.app.MoveTabToSession("src", index, "remote", "another-project"); err == nil {
		t.Error("a move for another project was carried out")
	}
	if storedSrc := f.stored(t, "src"); len(storedSrc.FollowedWindows) != 1 {
		t.Error("a refused move changed the source")
	}

	refusals, err := f.app.TabMoveRefusals("src", index)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"remote": "error.tabMoveLocalTabToServer"}; !reflect.DeepEqual(refusals, want) {
		t.Errorf("refusals = %+v, want %+v", refusals, want)
	}
}

func TestMoveTabToNewSessionMakesTheTabASession(t *testing.T) {
	f := newTabMoveFixture(t)
	src := f.session(t, "src", true, session.FollowedWindow{ID: "tab-a", Name: "src", Agent: session.AgentTerminal, Index: 1})
	src.GroupID = "g1"
	if err := f.storage.UpdateInstance(src); err != nil {
		t.Fatal(err)
	}
	f.session(t, "after", false)
	index := tabIndex(t, src, "tab-a")
	pid := f.pid("src:" + strconv.Itoa(index))
	f.task(t, src, "for the tab", "tab-a")
	seedRunning(t, f.storage, map[string]session.RunningRecord{"src": {Tabs: []string{"tab-a"}}})

	result, err := f.app.MoveTabToNewSession("src", index, "", "")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command(session.TmuxBinary(), "kill-session", "-t", result.SessionID).Run() })
	if result.SessionName != "src 2" {
		t.Errorf("the new session is called %q; the tab's name is taken by its old session", result.SessionName)
	}
	if got := f.pid(result.SessionID + ":" + strconv.Itoa(result.WindowIdx)); got != pid {
		t.Errorf("the tab was restarted: %s, was %s", got, pid)
	}
	instances, _, err := f.storage.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if instances[1].ID != result.SessionID || instances[1].GroupID != "g1" || instances[1].Status != session.StatusRunning {
		t.Errorf("the new session is stored as %+v (position 1 wanted, after its source)", instances[1])
	}
	task := tasksOf(t, instances[1])["for the tab"]
	if task.SessionID != result.SessionID || task.TabID != session.MainTabID {
		t.Errorf("the tab's task is %+v, want it on the new session's own tab", task)
	}
	if got := loadRunning(t, f.storage)[result.SessionID].Tabs; !reflect.DeepEqual(got, []string{session.MainTabID}) {
		t.Errorf("running record of the new session = %v", got)
	}
}

func TestMergeSessionIntoRemovesTheEmptiedSession(t *testing.T) {
	f := newTabMoveFixture(t)
	src := f.session(t, "src", true, session.FollowedWindow{ID: "tab-a", Name: "a", Agent: session.AgentTerminal, Index: 1})
	dst := f.session(t, "dst", true)
	mainPid := f.pid("src:" + strconv.Itoa(src.GetMainWindowIndex()))
	f.task(t, src, "for its own tab", session.MainTabID)
	f.task(t, src, "for the tab", "tab-a")
	f.task(t, src, "for the session", "")
	if err := f.storage.UpdateSettings(func(s *session.Settings) {
		s.QuickJump = []session.QuickJumpEntry{{SessionID: "src", WindowIdx: -1, Label: "whole"}}
	}); err != nil {
		t.Fatal(err)
	}

	result, err := f.app.MergeSessionInto("src", "dst", "")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.TabsMoved != 2 || result.TasksMoved != 3 {
		t.Errorf("result = %+v", result)
	}
	if f.stored(t, "src") != nil {
		t.Error("the merged session is still stored")
	}
	if trash, _ := f.storage.ListTrash(); len(trash) != 0 {
		t.Errorf("the merged session went to the trash: %+v", trash)
	}
	if got := f.pid("dst:" + strconv.Itoa(result.WindowIdx)); got != mainPid {
		t.Errorf("the merged session's own agent was restarted: %s, was %s", got, mainPid)
	}
	storedDst := f.stored(t, "dst")
	if len(storedDst.FollowedWindows) != 2 {
		t.Fatalf("the target holds %+v", storedDst.FollowedWindows)
	}
	var ownTabID string
	for _, fw := range storedDst.FollowedWindows {
		if fw.Index == result.WindowIdx {
			ownTabID = fw.ID
		}
	}
	tasks := tasksOf(t, dst)
	if got := tasks["for its own tab"]; got.SessionID != "dst" || got.TabID != ownTabID || ownTabID == "" {
		t.Errorf("own-tab task = %+v, want it on tab %q of the target", got, ownTabID)
	}
	if got := tasks["for the tab"]; got.SessionID != "dst" || got.TabID != "tab-a" {
		t.Errorf("tab task = %+v", got)
	}
	if got := tasks["for the session"]; got.SessionID != "dst" || got.TabID != "" {
		t.Errorf("session task = %+v", got)
	}
	_, _, settings, err := f.storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if want := []session.QuickJumpEntry{{SessionID: "dst", WindowIdx: result.WindowIdx, Label: "whole"}}; !reflect.DeepEqual(settings.QuickJump, want) {
		t.Errorf("quick jump = %+v, want %+v", settings.QuickJump, want)
	}
}
