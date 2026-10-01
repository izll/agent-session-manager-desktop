package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadedManager(t *testing.T, dir string) *TaskManager {
	t.Helper()
	manager := NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	return manager
}

func tasksOnDisk(t *testing.T, dir string) []Task {
	t.Helper()
	return loadedManager(t, dir).GetTasks()
}

// Moving a task keeps what describes the work — subtasks, deadline, priority,
// tags, status, details, ID — gives it its new owner, and drops dependencies,
// which name tasks in the list it left. The tasks left behind stop waiting for
// it.
func TestTransferTaskCarriesTheWorkAndCutsDependencies(t *testing.T) {
	projectDir, sessionDir := t.TempDir(), t.TempDir()
	project := loadedManager(t, projectDir)
	sessionList := loadedManager(t, sessionDir)

	blocker, err := project.CreateTask("blocker", "", TaskPriorityLow, nil)
	if err != nil {
		t.Fatal(err)
	}
	moving, err := project.CreateTask("moving", "desc", TaskPriorityHigh, []string{"ui"})
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := project.CreateTask("waiter", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	if err := project.UpdateTask(moving.ID, map[string]interface{}{
		"details":      "long body",
		"status":       string(TaskStatusInProgress),
		"dueAt":        due.Format(time.RFC3339),
		"dependencies": []interface{}{blocker.ID},
		"subtasks": []interface{}{
			map[string]interface{}{"id": "1", "title": "first", "status": "done"},
			map[string]interface{}{"id": "2", "title": "second"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := project.UpdateTask(waiter.ID, map[string]interface{}{
		"dependencies": []interface{}{moving.ID, blocker.ID},
	}); err != nil {
		t.Fatal(err)
	}

	moved, err := TransferTask(project, sessionList, moving.ID, "session-a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != moving.ID {
		t.Errorf("a free ID was replaced: %q -> %q", moving.ID, moved.ID)
	}

	arrived := tasksOnDisk(t, sessionDir)
	if len(arrived) != 1 {
		t.Fatalf("session list holds %d tasks, want 1", len(arrived))
	}
	got := arrived[0]
	if got.Title != "moving" || got.Description != "desc" || got.Details != "long body" ||
		got.Priority != TaskPriorityHigh || got.Status != TaskStatusInProgress ||
		len(got.Tags) != 1 || got.Tags[0] != "ui" {
		t.Errorf("the work did not travel intact: %+v", got)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Errorf("deadline lost: %v", got.DueAt)
	}
	if len(got.Subtasks) != 2 || !got.Subtasks[0].Done || got.Subtasks[1].Title != "second" {
		t.Errorf("subtasks lost: %+v", got.Subtasks)
	}
	if got.SessionID != "session-a" || got.TabID != "codex" {
		t.Errorf("owner = %q/%q, want session-a/codex", got.SessionID, got.TabID)
	}
	if len(got.Dependencies) != 0 {
		t.Errorf("dependencies on the old list travelled: %v", got.Dependencies)
	}

	left := tasksOnDisk(t, projectDir)
	if len(left) != 2 {
		t.Fatalf("project list holds %d tasks, want 2", len(left))
	}
	for _, task := range left {
		if task.ID == moving.ID {
			t.Fatal("the moved task is still in the project list")
		}
		if task.ID == waiter.ID && (len(task.Dependencies) != 1 || task.Dependencies[0] != blocker.ID) {
			t.Errorf("the waiter should only wait for the blocker now: %v", task.Dependencies)
		}
	}

	// And back: the task belongs to no session again.
	back, err := TransferTask(sessionList, project, moved.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if back.SessionID != "" || back.TabID != "" {
		t.Errorf("a task moved to the project kept its owner: %q/%q", back.SessionID, back.TabID)
	}
	if len(tasksOnDisk(t, sessionDir)) != 0 || len(tasksOnDisk(t, projectDir)) != 3 {
		t.Error("moving back did not move")
	}
}

// The destination may already use the ID; the moved task then gets a new one
// instead of shadowing the task that has it.
func TestTransferTaskRenumbersOnACollision(t *testing.T) {
	projectDir, sessionDir := t.TempDir(), t.TempDir()
	project := loadedManager(t, projectDir)
	sessionList := loadedManager(t, sessionDir)
	moving, err := project.CreateTask("moving", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionList.RestoreTask(Task{ID: moving.ID, Title: "already here", Status: TaskStatusBacklog}); err != nil {
		t.Fatal(err)
	}

	moved, err := TransferTask(project, sessionList, moving.ID, "s", "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID == moving.ID {
		t.Fatal("the moved task took an ID the destination already uses")
	}
	titles := map[string]string{}
	for _, task := range tasksOnDisk(t, sessionDir) {
		titles[task.ID] = task.Title
	}
	if titles[moving.ID] != "already here" || titles[moved.ID] != "moving" {
		t.Errorf("destination after the move: %v", titles)
	}
}

// A task edited between being read and being taken out stays where it is, and
// the copy is removed: the edit exists only in the source.
func TestTransferTaskUndoesTheCopyWhenTheSourceChanged(t *testing.T) {
	projectDir, sessionDir := t.TempDir(), t.TempDir()
	project := loadedManager(t, projectDir)
	sessionList := loadedManager(t, sessionDir)
	moving, err := project.CreateTask("moving", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := project.GetTask(moving.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Another writer edits the task after it was read for the move.
	time.Sleep(2 * time.Millisecond)
	if err := loadedManager(t, projectDir).UpdateTask(moving.ID, map[string]interface{}{"title": "edited"}); err != nil {
		t.Fatal(err)
	}
	copied, err := sessionList.insertTask(*original)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.takeTask(*original); !errors.Is(err, ErrMovedTaskChanged) {
		t.Fatalf("takeTask error = %v, want ErrMovedTaskChanged", err)
	}
	if err := sessionList.DeleteTask(copied.ID); err != nil {
		t.Fatal(err)
	}
	left := tasksOnDisk(t, projectDir)
	if len(left) != 1 || left[0].Title != "edited" {
		t.Errorf("the edit was lost: %+v", left)
	}
}

// When the source cannot give the task up, TransferTask takes the copy back
// out of the destination: the task is in exactly one list either way.
func TestTransferTaskRollsBackWhenTheSourceCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	projectDir, sessionDir := t.TempDir(), t.TempDir()
	project := loadedManager(t, projectDir)
	sessionList := loadedManager(t, sessionDir)
	moving, err := project.CreateTask("moving", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(projectDir, ".taskmaster")
	if err := os.Chmod(taskDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o755) })

	if _, err := TransferTask(project, sessionList, moving.ID, "s", ""); err == nil {
		t.Skip("this filesystem ignores read-only directories")
	}
	if got := tasksOnDisk(t, sessionDir); len(got) != 0 {
		t.Errorf("the copy was left in the destination: %+v", got)
	}
	if got := tasksOnDisk(t, projectDir); len(got) != 1 {
		t.Errorf("the task left its list: %+v", got)
	}
}

// Both ends in one file — a session opened on the directory that holds the
// list — is only a change of owner.
func TestTransferTaskWithinOneFileChangesOnlyTheOwner(t *testing.T) {
	dir := t.TempDir()
	first := loadedManager(t, dir)
	second := loadedManager(t, dir)
	task, err := first.CreateTask("same file", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := TransferTask(first, second, task.ID, "s", "main")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != task.ID || moved.SessionID != "s" || moved.TabID != "main" {
		t.Errorf("moved = %+v", moved)
	}
	if got := tasksOnDisk(t, dir); len(got) != 1 {
		t.Errorf("a same-file move duplicated or lost the task: %+v", got)
	}
}

// The project's own files — sessions.json and the task list — sit together,
// in the config root for the default project and in its own directory for any
// other.
func TestProjectDataDirIsBesideSessionsJSON(t *testing.T) {
	s := newTestStorage(t)
	if got, want := s.ProjectDataDir(), s.configDir; got != want {
		t.Errorf("default project dir = %q, want %q", got, want)
	}
	project, err := s.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveProject(project.ID); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.configDir, "projects", project.ID)
	if got := s.ProjectDataDir(); got != want {
		t.Errorf("project dir = %q, want %q", got, want)
	}
	if got, err := s.ProjectDataDirFor(project.ID); err != nil || got != want {
		t.Errorf("ProjectDataDirFor = %q, %v", got, err)
	}
	if got, err := s.ProjectDataDirFor(""); err != nil || got != s.configDir {
		t.Errorf("ProjectDataDirFor(default) = %q, %v", got, err)
	}
	if _, err := s.ProjectDataDirFor("../escape"); err == nil {
		t.Error("an invalid project ID was accepted")
	}
}

func textPage(text string) []NotePage {
	return []NotePage{{ID: "p", Text: text}}
}

func projectNoteText(s *Storage) (string, error) {
	pages, err := s.ProjectNotePages()
	return NotePagesText(pages), err
}

// The project note survives the writes that replace everything else in
// sessions.json, and stays with its project.
func TestProjectNotesRoundTripAndStayWithTheirProject(t *testing.T) {
	s := newTestStorage(t)
	if err := s.SetProjectNotePages(textPage("default note")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddInstance(&Instance{ID: "one", Name: "one", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(&Settings{Language: "hu"}); err != nil {
		t.Fatal(err)
	}
	if got, err := projectNoteText(s); err != nil || got != "default note" {
		t.Fatalf("after other writes the note is %q, %v", got, err)
	}

	project, err := s.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := projectNoteText(s); got != "" {
		t.Errorf("another project sees the default project's note: %q", got)
	}
	if err := s.SetProjectNotePages(textPage("other note")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveProject(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := projectNoteText(s); got != "default note" {
		t.Errorf("the default project's note became %q", got)
	}
}

// A project task snapshot is restorable: its directory is the project's own,
// which is not any session's path, and must still count as the project's.
func TestTaskBackupRestoresTheProjectList(t *testing.T) {
	s := newTestStorage(t)
	dir := s.ProjectDataDir()
	manager := loadedManager(t, dir)
	if _, err := manager.CreateTask("kept", "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.BackupTaskFiles([]string{dir}); err != nil {
		t.Fatal(err)
	}
	backups, err := s.ListTaskBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("the project list's snapshot is not listed: %v, %v", backups, err)
	}
	if _, err := manager.CreateTask("after the backup", "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreTaskBackup(backups[0].ID); err != nil {
		t.Fatal(err)
	}
	got := tasksOnDisk(t, dir)
	if len(got) != 1 || got[0].Title != "kept" {
		t.Errorf("restored project list = %+v", got)
	}
}
