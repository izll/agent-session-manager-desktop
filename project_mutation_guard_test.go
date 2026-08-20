package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func guardedTestStorage(t *testing.T) *session.Storage {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestProjectMutatorsRefuseReadOnlyInstance(t *testing.T) {
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	inst := &session.Instance{ID: "session-one", Name: "before", Path: workDir, Status: session.StatusStopped}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: false}

	if err := app.RenameSession(inst.ID, "after"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("RenameSession error = %v, want read-only refusal", err)
	}
	stored, err := storage.GetInstance(inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "before" {
		t.Fatalf("read-only rename changed stored name to %q", stored.Name)
	}

	if _, err := app.CreateTask(inst.ID, "blocked", "", "medium", nil); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("CreateTask error = %v, want read-only refusal", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".taskmaster", "tasks.json")); !os.IsNotExist(err) {
		t.Fatalf("read-only task mutation created a task file: %v", err)
	}
}

func TestProjectMutationsAreSerialized(t *testing.T) {
	app := &App{projectLocked: true}
	releaseFirst, err := app.beginProjectMutation()
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func(), 1)
	go func() {
		release, err := app.beginProjectMutation()
		if err == nil {
			acquired <- release
		}
	}()
	select {
	case release := <-acquired:
		release()
		t.Fatal("a second project mutation overlapped the first")
	case <-time.After(30 * time.Millisecond):
	}
	releaseFirst()
	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("second project mutation did not proceed after release")
	}
}

func TestTerminalAttachPinsProjectUntilRelease(t *testing.T) {
	app := &App{projectLocked: true}
	release, allowed := app.beginTerminalAttach()
	if !allowed || release == nil {
		t.Fatal("project owner should be allowed to begin an attach")
	}
	switched := make(chan struct{})
	go func() {
		app.projectMu.Lock()
		close(switched)
		app.projectMu.Unlock()
	}()
	select {
	case <-switched:
		t.Fatal("project switch acquired the lock before attach setup released it")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case <-switched:
	case <-time.After(time.Second):
		t.Fatal("project switch remained blocked after attach setup released it")
	}

	app.projectLocked = false
	if release, allowed := app.beginTerminalAttach(); allowed || release != nil {
		t.Fatal("read-only project unexpectedly allowed a terminal attach")
	}
}

func TestRestoreDeletedTaskUsesOriginalLocalIdentity(t *testing.T) {
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	inst := &session.Instance{ID: "session-restore", Name: "restore", Path: workDir, Status: session.StatusStopped}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: true}
	due := time.Now().Add(time.Hour).UTC().Round(0)
	snapshot := DeletedTaskSnapshot{
		ID: "stable-task-id", Title: "title", Description: "description", Details: "details",
		Status: "pending", Priority: "high", Tags: []string{"tag"}, Dependencies: []string{"other"},
		DueAt: due.Format(time.RFC3339Nano), SessionID: inst.ID,
		Subtasks: []DeletedSubtaskSnapshot{{ID: "stable-subtask-id", Title: "sub", Description: "sub description", Details: "sub details", Status: "done"}},
	}
	if err := app.RestoreDeletedTask(inst.ID, "local", snapshot); err != nil {
		t.Fatal(err)
	}
	manager, err := app.getTaskManager(inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.GetTask(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != snapshot.ID || got.Status != session.TaskStatusBacklog || got.Details != snapshot.Details || got.DueAt == nil || !got.DueAt.Equal(due) || len(got.Subtasks) != 1 || got.Subtasks[0].ID != "stable-subtask-id" || got.Subtasks[0].Description != "sub description" || got.Subtasks[0].Details != "sub details" || got.Subtasks[0].Status != session.TaskStatusDone || !got.Subtasks[0].Done {
		t.Fatalf("restored local snapshot changed: %+v", got)
	}
	info := convertTask(*got)
	if info.Subtasks[0].Description != "sub description" || info.Subtasks[0].Details != "sub details" || info.Subtasks[0].Status != "done" {
		t.Fatalf("local frontend conversion dropped subtask fields: %+v", info.Subtasks[0])
	}
	if err := manager.DeleteSubtask(snapshot.ID, "stable-subtask-id"); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedSubtask(inst.ID, "local", snapshot.ID, snapshot.Subtasks[0]); err != nil {
		t.Fatal(err)
	}
	restoredAgain, err := manager.GetTask(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredAgain.Subtasks) != 1 || restoredAgain.Subtasks[0].ID != "stable-subtask-id" || restoredAgain.Subtasks[0].Status != session.TaskStatusDone || restoredAgain.Subtasks[0].Details != "sub details" {
		t.Fatalf("standalone subtask undo changed snapshot: %+v", restoredAgain.Subtasks)
	}
}

func TestValidateDiffRootRejectsChangedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	inst := &session.Instance{Path: root, BrowseRoot: root}
	if err := validateDiffRoot(inst, root); err != nil {
		t.Fatalf("matching root rejected: %v", err)
	}
	inst.BrowseRoot = other
	if err := validateDiffRoot(inst, root); err == nil {
		t.Fatal("changed tab working directory was accepted for revert")
	}
}

func TestMCPDeletedTaskSnapshotRoundTripsAllOptionalFields(t *testing.T) {
	snapshot := DeletedTaskSnapshot{
		ID: "stable", Title: "title", Description: "description", Details: "details",
		Status: "done", Priority: "critical", Tags: []string{"tag"}, Dependencies: []string{"dependency"},
		CreatedAt: "2026-08-20T12:00:00Z", UpdatedAt: "2026-08-20T12:30:00Z",
		CompletedAt: "2026-08-20T13:00:00Z", DueAt: "2026-08-21T12:00:00Z", SessionID: "session-one",
		Subtasks: []DeletedSubtaskSnapshot{{
			ID: "stable.1", Title: "sub", Description: "sub description", Details: "sub details",
			Status: "done", CreatedAt: "2026-08-20T12:05:00Z",
		}},
	}
	info := convertMCPTask(mcpTaskFromSnapshot(snapshot))
	if info.UpdatedAt != snapshot.UpdatedAt || info.CompletedAt != snapshot.CompletedAt || info.DueAt != snapshot.DueAt || info.SessionID != snapshot.SessionID {
		t.Fatalf("MCP task optional fields did not round-trip: %+v", info)
	}
	if len(info.Subtasks) != 1 || info.Subtasks[0].Description != snapshot.Subtasks[0].Description || info.Subtasks[0].Details != snapshot.Subtasks[0].Details || info.Subtasks[0].CreatedAt != snapshot.Subtasks[0].CreatedAt {
		t.Fatalf("MCP subtask optional fields did not round-trip: %+v", info.Subtasks)
	}
}
