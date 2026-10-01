package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// projectTasksApp is an App over a throwaway config dir, with one session and
// a clean task-manager cache.
func projectTasksApp(t *testing.T) (*App, *session.Storage, *session.Instance) {
	t.Helper()
	storage := guardedTestStorage(t)
	instance := &session.Instance{ID: "worker", Name: "worker", Path: t.TempDir(), Status: session.StatusStopped}
	if err := storage.AddInstance(instance); err != nil {
		t.Fatal(err)
	}
	taskManagerMu.Lock()
	oldCache := taskManagerCache
	taskManagerCache = make(map[string]*session.TaskManager)
	taskManagerMu.Unlock()
	t.Cleanup(func() {
		taskManagerMu.Lock()
		taskManagerCache = oldCache
		taskManagerMu.Unlock()
	})
	return &App{storage: storage, projectLocked: true}, storage, instance
}

func projectTasksOnDisk(t *testing.T, storage *session.Storage) []session.Task {
	t.Helper()
	manager := session.NewTaskManager(storage.ProjectDataDir())
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	return manager.GetTasks()
}

// The project's list is written beside its sessions.json, belongs to no
// session, and is read back through the ordinary task API.
func TestProjectTasksLiveBesideSessionsJSONAndBelongToNoSession(t *testing.T) {
	app, storage, instance := projectTasksApp(t)
	created, err := app.CreateTask(ProjectTasksScope, "plan the release", "", "high", []string{"ops"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if created.SessionID != "" {
		t.Errorf("a project task was tied to session %q", created.SessionID)
	}
	file := filepath.Join(storage.ProjectDataDir(), ".taskmaster", "tasks.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("project task file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(instance.Path, ".taskmaster")); !os.IsNotExist(err) {
		t.Errorf("the session's directory got a task file: %v", err)
	}

	listed, err := app.GetTasks(ProjectTasksScope)
	if err != nil || len(listed) != 1 || listed[0].Title != "plan the release" {
		t.Fatalf("GetTasks(project) = %+v, %v", listed, err)
	}
	if sessionTasks, _ := app.GetTasks(instance.ID); len(sessionTasks) != 0 {
		t.Errorf("the session's list shows project tasks: %+v", sessionTasks)
	}

	// An edit cannot quietly tie it to a session: that is a move.
	if err := app.UpdateTask(ProjectTasksScope, created.ID, map[string]interface{}{
		"title": "renamed", "sessionId": instance.ID, "tabId": "main",
	}, ""); err != nil {
		t.Fatal(err)
	}
	stored := projectTasksOnDisk(t, storage)
	if len(stored) != 1 || stored[0].Title != "renamed" || stored[0].SessionID != "" || stored[0].TabID != "" {
		t.Errorf("after the edit: %+v", stored)
	}
}

// Moving to a session and back goes through the API with the project guard,
// and the task shows up in the right list each time.
func TestMoveTaskToSessionAndBack(t *testing.T) {
	app, storage, instance := projectTasksApp(t)
	created, err := app.CreateTask(ProjectTasksScope, "hand over", "", "medium", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddSubtask(ProjectTasksScope, created.ID, "step", ""); err != nil {
		t.Fatal(err)
	}

	moved, err := app.MoveTaskToSession(created.ID, instance.ID, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.SessionID != instance.ID || moved.TabID != "main" || len(moved.Subtasks) != 1 {
		t.Errorf("moved = %+v", moved)
	}
	if left := projectTasksOnDisk(t, storage); len(left) != 0 {
		t.Errorf("the project list kept the task: %+v", left)
	}
	sessionTasks, err := app.GetTasks(instance.ID)
	if err != nil || len(sessionTasks) != 1 || sessionTasks[0].Title != "hand over" {
		t.Fatalf("the session list = %+v, %v", sessionTasks, err)
	}
	if pending, _ := app.UnfinishedTasksForSession(instance.ID); len(pending) != 1 {
		t.Errorf("a task moved to a session is not that session's outstanding work: %+v", pending)
	}

	back, err := app.MoveTaskToProject(instance.ID, moved.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if back.SessionID != "" || back.TabID != "" {
		t.Errorf("back = %+v", back)
	}
	if remaining, _ := app.GetTasks(instance.ID); len(remaining) != 0 {
		t.Errorf("the session list kept the task: %+v", remaining)
	}
	if got := projectTasksOnDisk(t, storage); len(got) != 1 || len(got[0].Subtasks) != 1 {
		t.Errorf("the project list after the move back: %+v", got)
	}
}

// Task Master keeps its own file, which the session panel then shows; a task
// moved into the app's list would seem to disappear, so the move is refused.
func TestMoveTaskToSessionIsRefusedWhileTaskMasterIsOn(t *testing.T) {
	app, storage, instance := projectTasksApp(t)
	created, err := app.CreateTask(ProjectTasksScope, "stays", "", "medium", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveSettings(&session.Settings{TaskMasterEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MoveTaskToSession(created.ID, instance.ID, "", ""); err == nil || err.Error() != "error.projectTaskMoveTaskMaster" {
		t.Fatalf("error = %v, want error.projectTaskMoveTaskMaster", err)
	}
	if got := projectTasksOnDisk(t, storage); len(got) != 1 {
		t.Errorf("the refused move changed the list: %+v", got)
	}
}

// Every project-task write is pinned to the project the UI showed and refused
// in a read-only instance, like every other mutation.
func TestProjectTaskWritesHonourTheProjectGuard(t *testing.T) {
	app, storage, instance := projectTasksApp(t)
	created, err := app.CreateTask(ProjectTasksScope, "guarded", "", "medium", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	stale := "some-other-project"
	checks := map[string]func() error{
		"CreateTask": func() error {
			_, err := app.CreateTask(ProjectTasksScope, "x", "", "medium", nil, stale)
			return err
		},
		"MoveTaskToSession": func() error {
			_, err := app.MoveTaskToSession(created.ID, instance.ID, "", stale)
			return err
		},
		"MoveTaskToProject": func() error {
			_, err := app.MoveTaskToProject(instance.ID, created.ID, stale)
			return err
		},
		"SendProjectTaskToAgent": func() error {
			return app.SendProjectTaskToAgent(created.ID, instance.ID, "", stale)
		},
		"SetProjectNotePages": func() error { return app.SetProjectNotePages(onePage("x"), stale) },
	}
	for name, call := range checks {
		if err := call(); err == nil || !strings.Contains(err.Error(), "active project changed") {
			t.Errorf("%s with a stale project: %v", name, err)
		}
	}

	app.projectLocked = false
	for name, call := range map[string]func() error{
		"SetProjectNotePages": func() error { return app.SetProjectNotePages(onePage("x"), "") },
		"MoveTaskToSession": func() error {
			_, err := app.MoveTaskToSession(created.ID, instance.ID, "", "")
			return err
		},
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s in a read-only instance: %v", name, err)
		}
	}
	if got := projectTasksOnDisk(t, storage); len(got) != 1 || got[0].SessionID != "" {
		t.Errorf("a refused write changed the project list: %+v", got)
	}
	if pages, _ := storage.ProjectNotePages(); len(pages) != 0 {
		t.Errorf("a refused write changed the note: %+v", pages)
	}
}

func onePage(text string) []session.NotePage {
	return []session.NotePage{{ID: "p", Text: text}}
}

// The notes endpoints read and write the active project's own note, every
// page of it.
func TestProjectNotesAPI(t *testing.T) {
	app, _, _ := projectTasksApp(t)
	if got, err := app.GetProjectNotePages(); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("an empty project note = %#v, %v; want an empty list", got, err)
	}
	pages := []session.NotePage{
		{ID: "a", Title: "Plan", Text: "the plan"},
		{ID: "b", Title: "Risks", Text: "none yet"},
	}
	if err := app.SetProjectNotePages(pages, ""); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetProjectNotePages()
	if err != nil || !slices.Equal(got, pages) {
		t.Errorf("GetProjectNotePages = %+v, %v; want %+v", got, err, pages)
	}
}

// An explicit backup includes the project's own list, and restoring it brings
// the list back — the same safety net a session's list has.
func TestCreateBackupIncludesTheProjectTaskList(t *testing.T) {
	app, storage, _ := projectTasksApp(t)
	if _, err := app.CreateTask(ProjectTasksScope, "in the backup", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CreateBackup(""); err != nil {
		t.Fatal(err)
	}
	backups, err := app.GetTaskBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("task backups = %v, %v", backups, err)
	}
	if _, err := app.CreateTask(ProjectTasksScope, "after the backup", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreTaskBackup(backups[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	got := projectTasksOnDisk(t, storage)
	if len(got) != 1 || got[0].Title != "in the backup" {
		t.Fatalf("restored project list = %+v", got)
	}
	// The cached manager was reloaded: the next write builds on the restore.
	if _, err := app.CreateTask(ProjectTasksScope, "next", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if got := projectTasksOnDisk(t, storage); len(got) != 2 {
		t.Errorf("a stale cache overwrote the restored list: %+v", got)
	}
}

// The all-tasks overview lists every project's own tasks, marked as such and
// tied to no session.
func TestGetAllTasksIncludesProjectTasks(t *testing.T) {
	app, storage, instance := projectTasksApp(t)
	if _, err := app.CreateTask(ProjectTasksScope, "default project task", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateTask(instance.ID, "session task", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	other, err := storage.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	otherDir, err := storage.ProjectDataDirFor(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A project's directory exists once it has been opened.
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	otherManager := session.NewTaskManager(otherDir)
	if err := otherManager.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := otherManager.CreateTask("other project task", "", session.TaskPriorityLow, nil); err != nil {
		t.Fatal(err)
	}

	items, err := app.GetAllTasks()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]TaskOverviewItem{}
	for _, item := range items {
		found[item.Title] = item
	}
	if item, ok := found["default project task"]; !ok || !item.ProjectTask || item.ProjectID != "" || item.SessionID != "" || item.ProjectPath != "" {
		t.Errorf("default project task = %+v (found %v)", item, ok)
	}
	if item, ok := found["other project task"]; !ok || !item.ProjectTask || item.ProjectID != other.ID || item.ProjectName != "other" {
		t.Errorf("other project task = %+v (found %v)", item, ok)
	}
	if item, ok := found["session task"]; !ok || item.ProjectTask || item.SessionID != instance.ID {
		t.Errorf("session task = %+v (found %v)", item, ok)
	}
}
