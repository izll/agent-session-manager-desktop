package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskManagerRejectsTaskDirectorySymlinkOutsideProject(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTaskFile(t, outside, `{"tasks":[{"id":"outside"}]}`)
	outsideTaskPath := taskFileFor(outside)
	before, err := os.ReadFile(outsideTaskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(project, ".taskmaster")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	manager := NewTaskManager(project)
	if err := manager.Load(); err == nil {
		t.Fatal("TaskManager followed an out-of-project .taskmaster symlink")
	}
	if err := manager.RestoreTask(Task{ID: "new", Title: "new", Status: TaskStatusBacklog}); err == nil {
		t.Fatal("task mutation followed an out-of-project .taskmaster symlink")
	}
	after, err := os.ReadFile(outsideTaskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected task mutation changed outside file: %s", after)
	}
}

func TestTaskBackupRejectsTaskDirectorySymlinkOutsideProject(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTaskFile(t, outside, `{"tasks":[]}`)
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(project, ".taskmaster")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	storage := taskBackupTestStorage(t, project)
	if err := storage.BackupTaskFiles([]string{project}); err == nil {
		t.Fatal("task backup followed an out-of-project .taskmaster symlink")
	}
}

func TestTaskRestoreRejectsTaskDirectorySymlinkOutsideProject(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTaskFile(t, outside, `{"tasks":[{"id":"keep"}]}`)
	storage := taskBackupTestStorage(t, project)
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(project, ".taskmaster")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before, err := os.ReadFile(taskFileFor(outside))
	if err != nil {
		t.Fatal(err)
	}
	defaultProject := ""
	set := TaskBackupSet{
		ProjectID: &defaultProject,
		CreatedAt: time.Now().UTC(),
		Files:     []TaskBackup{{Path: project, Content: `{"tasks":[{"id":"overwrite"}]}`}},
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(storage.configDir, "backups", "tasks")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const backupID = "20260821T130000.000000000Z-symlink.json"
	if err := os.WriteFile(filepath.Join(backupDir, backupID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreTaskBackup(backupID); err == nil {
		t.Fatal("task restore followed an out-of-project .taskmaster symlink")
	}
	after, err := os.ReadFile(taskFileFor(outside))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected restore changed outside file: %s", after)
	}
}
