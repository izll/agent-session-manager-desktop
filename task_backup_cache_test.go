package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

func TestRestoreTaskBackupReloadsCachedManagersBeforeNextMutation(t *testing.T) {
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	instance := &session.Instance{ID: "task-backup-session", Name: "tasks", Path: workDir, Status: session.StatusStopped}
	if err := storage.AddInstance(instance); err != nil {
		t.Fatal(err)
	}
	taskManagerMu.Lock()
	oldCache := taskManagerCache
	taskManagerCache = make(map[string]*session.TaskManager)
	taskManagerMu.Unlock()
	defer func() {
		taskManagerMu.Lock()
		taskManagerCache = oldCache
		taskManagerMu.Unlock()
	}()

	app := &App{storage: storage, projectLocked: true}
	if _, err := app.CreateTask(instance.ID, "in backup", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := storage.BackupTaskFiles([]string{workDir}); err != nil {
		t.Fatal(err)
	}
	backups, err := storage.ListTaskBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("task backup missing: backups=%v err=%v", backups, err)
	}
	if _, err := app.CreateTask(instance.ID, "must stay deleted", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreTaskBackup(backups[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateTask(instance.ID, "created after restore", "", "medium", nil, ""); err != nil {
		t.Fatal(err)
	}

	manager := session.NewTaskManager(workDir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	tasks := manager.GetTasks()
	if len(tasks) != 2 || tasks[0].Title != "in backup" || tasks[1].Title != "created after restore" {
		t.Fatalf("stale cache overwrote restored snapshot: %+v", tasks)
	}
}

func TestCreateBackupReportsTaskSnapshotFailure(t *testing.T) {
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	instance := &session.Instance{ID: "oversized-task-backup", Name: "tasks", Path: workDir, Status: session.StatusStopped}
	if err := storage.AddInstance(instance); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(workDir, ".taskmaster", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((64 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage, projectLocked: true}
	err = app.CreateBackup("")
	if err == nil || !strings.Contains(err.Error(), "task backup failed") {
		t.Fatalf("CreateBackup error = %v, want explicit partial-backup error", err)
	}
	backups, listErr := storage.ListBackups()
	if listErr != nil || len(backups) == 0 {
		t.Fatalf("successful canonical backup was not retained: backups=%v err=%v", backups, listErr)
	}
}
