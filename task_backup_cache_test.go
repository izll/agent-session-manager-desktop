package main

import (
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
	if _, err := app.CreateTask(instance.ID, "in backup", "", "medium", nil); err != nil {
		t.Fatal(err)
	}
	if err := storage.BackupTaskFiles([]string{workDir}); err != nil {
		t.Fatal(err)
	}
	backups, err := storage.ListTaskBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("task backup missing: backups=%v err=%v", backups, err)
	}
	if _, err := app.CreateTask(instance.ID, "must stay deleted", "", "medium", nil); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreTaskBackup(backups[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateTask(instance.ID, "created after restore", "", "medium", nil); err != nil {
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
