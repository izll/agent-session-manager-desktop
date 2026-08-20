package main

import (
	"encoding/json"
	"fmt"
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

func TestSelectProjectRejectsUnknownIDAndRestoresPreviousOwnership(t *testing.T) {
	storage := guardedTestStorage(t)
	if err := storage.LockProject(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.UnlockProject)
	app := &App{storage: storage, projectLocked: true}

	if err := app.SelectProject("proj_unknown"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown project switch error = %v, want not found", err)
	}
	if got := storage.GetActiveProjectID(); got != "" {
		t.Fatalf("active project after rollback = %q, want default", got)
	}
	if !app.projectLocked {
		t.Fatal("failed switch did not reacquire previous project ownership")
	}
}

func TestSelectProjectRollsBackWhenTargetDeletionHasStarted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	project, err := storage.AddProject("deleting")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.LockProjectForUse(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.UnlockProject)
	deletionMarker := filepath.Join(home, ".config", "agent-session-manager-desktop", "project-locks", project.ID+".lock.deleting")
	if err := os.MkdirAll(filepath.Dir(deletionMarker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deletionMarker, []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage, projectLocked: true}
	if err := app.SelectProject(project.ID); err == nil || !strings.Contains(err.Error(), "being deleted") {
		t.Fatalf("switch during deletion error = %v", err)
	}
	if got := storage.GetActiveProjectID(); got != "" || !app.projectLocked {
		t.Fatalf("failed switch did not restore default ownership: project=%q locked=%v", got, app.projectLocked)
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

func TestOrphanCleanupPinsActiveProjectBeforeOwnershipCheck(t *testing.T) {
	app := &App{projectLocked: false}
	app.projectMu.Lock()
	done := make(chan struct{})
	go func() {
		app.cleanupOrphanedGUISessions()
		close(done)
	}()
	select {
	case <-done:
		app.projectMu.Unlock()
		t.Fatal("orphan cleanup read project ownership without pinning the active project")
	case <-time.After(30 * time.Millisecond):
	}
	app.projectMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("orphan cleanup remained blocked after the project switch lock was released")
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
	if _, err := validateDiffRoot(inst, root); err != nil {
		t.Fatalf("matching root rejected: %v", err)
	}
	inst.BrowseRoot = other
	if _, err := validateDiffRoot(inst, root); err == nil {
		t.Fatal("changed tab working directory was accepted for revert")
	}
}

func TestValidateBrowseRootRejectsChangedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	inst := &session.Instance{Path: root, BrowseRoot: alias}
	resolved, err := validateBrowseRoot(inst, alias)
	if err != nil {
		t.Fatalf("matching browser root rejected: %v", err)
	}
	if filepath.Clean(resolved) != filepath.Clean(root) {
		t.Fatalf("save root was not pinned to the resolved directory: got %q, want %q", resolved, root)
	}
	inst.BrowseRoot = other
	if _, err := validateBrowseRoot(inst, alias); err == nil {
		t.Fatal("changed tab working directory was accepted for save")
	}
	if _, err := validateBrowseRoot(inst, ""); err == nil {
		t.Fatal("missing expected root was accepted for save")
	}
}

func TestListSessionDirectoryCreatesAndEnforcesRootSnapshot(t *testing.T) {
	storage := guardedTestStorage(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{ID: "browse-root", Name: "browse", Path: root, Status: session.StatusStopped}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage}
	listing, err := app.ListSessionDirectory(inst.ID, "", -1, "")
	if err != nil {
		t.Fatalf("create root snapshot: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if listing.AbsPath != resolvedRoot {
		t.Fatalf("root snapshot = %q, want canonical %q", listing.AbsPath, resolvedRoot)
	}
	if _, err := app.ReadSessionDirectoryFile(inst.ID, "visible.txt", -1, listing.AbsPath); err != nil {
		t.Fatalf("read through matching root snapshot: %v", err)
	}
	if _, err := app.ListSessionDirectory(inst.ID, "subdirectory", -1, ""); err == nil {
		t.Fatal("nested listing accepted a missing root snapshot")
	}
}

func TestReloadTaskManagerCacheContinuesAfterOneStoreFails(t *testing.T) {
	badRoot := t.TempDir()
	goodRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(badRoot, ".taskmaster"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(goodRoot, ".taskmaster"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRoot, ".taskmaster", "tasks.json"), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	goodPath := filepath.Join(goodRoot, ".taskmaster", "tasks.json")
	if err := os.WriteFile(goodPath, []byte(`{"meta":{},"tasks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	badManager := session.NewTaskManager(badRoot)
	goodManager := session.NewTaskManager(goodRoot)
	if err := goodManager.Load(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodPath, []byte(`{"meta":{},"tasks":[{"id":"reloaded","title":"new","status":"backlog","priority":"medium"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	taskManagerMu.Lock()
	previous := taskManagerCache
	taskManagerCache = map[string]*session.TaskManager{"bad": badManager, "good": goodManager}
	taskManagerMu.Unlock()
	t.Cleanup(func() {
		taskManagerMu.Lock()
		taskManagerCache = previous
		taskManagerMu.Unlock()
	})

	if err := reloadTaskManagerCache(); err == nil {
		t.Fatal("invalid task store did not report a reload error")
	}
	if tasks := goodManager.GetTasks(); len(tasks) != 1 || tasks[0].ID != "reloaded" {
		t.Fatalf("valid cache was not reloaded after another store failed: %+v", tasks)
	}
}

func TestUpdateTaskMasterFileDirectWritesOneProviderSnapshot(t *testing.T) {
	tasksFile := filepath.Join(t.TempDir(), "tasks.json")
	original := map[string]interface{}{
		"master": map[string]interface{}{
			"tasks": []map[string]interface{}{{
				"id": "7", "title": "old", "description": "old description",
				"details": "old details", "priority": "low", "dueAt": "old due", "sessionId": "old session",
			}},
			"metadata": map[string]interface{}{"kept": true},
			"custom":   map[string]interface{}{"future": "kept"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasksFile, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateTaskMasterFileDirect(tasksFile, "7", "new", "", "", "critical", "2026-08-21T12:00:00Z", "session-one"); err != nil {
		t.Fatal(err)
	}
	readTask := func() map[string]interface{} {
		out, err := os.ReadFile(tasksFile)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]struct {
			Tasks    []map[string]interface{} `json:"tasks"`
			Metadata map[string]interface{}   `json:"metadata"`
			Custom   map[string]interface{}   `json:"custom"`
		}
		if err := json.Unmarshal(out, &root); err != nil {
			t.Fatal(err)
		}
		if root["master"].Metadata["kept"] != true || root["master"].Custom["future"] != "kept" || len(root["master"].Tasks) != 1 {
			t.Fatalf("unrelated Task Master context data changed: %#v", root)
		}
		return root["master"].Tasks[0]
	}
	task := readTask()
	if task["title"] != "new" || task["description"] != "" || task["details"] != "" || task["priority"] != "critical" || task["dueAt"] != "2026-08-21T12:00:00Z" || task["sessionId"] != "session-one" {
		t.Fatalf("atomic MCP edit lost fields: %#v", task)
	}

	if err := updateTaskMasterFileDirect(tasksFile, "7", "new", "", "", "critical", "", ""); err != nil {
		t.Fatal(err)
	}
	task = readTask()
	if _, ok := task["dueAt"]; ok {
		t.Fatalf("cleared deadline remained in MCP task: %#v", task)
	}
	if _, ok := task["sessionId"]; ok {
		t.Fatalf("cleared session remained in MCP task: %#v", task)
	}
}

func TestUpdateTaskMasterFileDirectPrefersMasterContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	data := `{"feature":{"tasks":[{"id":"7","title":"feature"}]},"master":{"tasks":[{"id":"7","title":"master"}]}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateTaskMasterFileDirect(path, "7", "updated", "", "", "high", "", ""); err != nil {
		t.Fatal(err)
	}
	var root map[string]struct {
		Tasks []map[string]interface{} `json:"tasks"`
	}
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root["master"].Tasks[0]["title"] != "updated" || root["feature"].Tasks[0]["title"] != "feature" {
		t.Fatalf("direct edit chose wrong context: %#v", root)
	}
}

func TestUpdateTaskMasterFileDirectRejectsAmbiguousNonMasterContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	data := `{"feature-a":{"tasks":[{"id":"7","title":"a"}]},"feature-b":{"tasks":[{"id":"7","title":"b"}]},"master":{"tasks":[]}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateTaskMasterFileDirect(path, "7", "updated", "", "", "high", "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous direct edit error = %v", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != data {
		t.Fatalf("ambiguous edit changed file: %s", raw)
	}
}

func TestMCPDeletedTaskSnapshotRoundTripsAllOptionalFields(t *testing.T) {
	snapshot := DeletedTaskSnapshot{
		ID: "stable", Title: "title", Description: "description", Details: "details",
		Status: "done", Priority: "critical", Tags: []string{"tag"}, Dependencies: []string{"dependency"},
		CreatedAt: "2026-08-20T12:00:00Z", UpdatedAt: "2026-08-20T12:30:00Z",
		CompletedAt: "2026-08-20T13:00:00Z", DueAt: "2026-08-21T12:00:00Z", SessionID: "session-one",
		TestStrategy: "task tests", RawJSON: `{"id":"stable","future":true}`,
		Subtasks: []DeletedSubtaskSnapshot{{
			ID: "stable.1", Title: "sub", Description: "sub description", Details: "sub details",
			Status: "done", CreatedAt: "2026-08-20T12:05:00Z", Dependencies: []string{"2"},
			ParentID: "stable", TestStrategy: "sub tests", RawJSON: `{"id":"stable.1","futureSub":true}`,
		}},
	}
	info := convertMCPTask(mcpTaskFromSnapshot(snapshot))
	if info.UpdatedAt != snapshot.UpdatedAt || info.CompletedAt != snapshot.CompletedAt || info.DueAt != snapshot.DueAt || info.SessionID != snapshot.SessionID || info.TestStrategy != snapshot.TestStrategy || info.RawJSON != snapshot.RawJSON {
		t.Fatalf("MCP task optional fields did not round-trip: %+v", info)
	}
	if len(info.Subtasks) != 1 || info.Subtasks[0].Description != snapshot.Subtasks[0].Description || info.Subtasks[0].Details != snapshot.Subtasks[0].Details || info.Subtasks[0].CreatedAt != snapshot.Subtasks[0].CreatedAt || info.Subtasks[0].ParentID != snapshot.Subtasks[0].ParentID || info.Subtasks[0].TestStrategy != snapshot.Subtasks[0].TestStrategy || info.Subtasks[0].RawJSON != snapshot.Subtasks[0].RawJSON || len(info.Subtasks[0].Dependencies) != 1 {
		t.Fatalf("MCP subtask optional fields did not round-trip: %+v", info.Subtasks)
	}
}
