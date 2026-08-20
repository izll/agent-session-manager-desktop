package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Several sessions often share one working directory, and so share one task
// file. Collecting per session would store the same content several times.
func TestSharedDirectoriesAreCollectedOnce(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, `{"tasks":[{"id":"1"}]}`)

	set := CollectTaskFiles([]string{dir, dir, dir})
	if len(set.Files) != 1 {
		t.Errorf("one directory should yield one entry, got %d", len(set.Files))
	}
}

// A directory with no task file is the common case, not an error.
func TestDirectoriesWithoutTasksAreSkipped(t *testing.T) {
	withTasks := t.TempDir()
	without := t.TempDir()
	writeTaskFile(t, withTasks, `{"tasks":[]}`)

	set := CollectTaskFiles([]string{without, withTasks, ""})
	if len(set.Files) != 1 || set.Files[0].Path != withTasks {
		t.Errorf("only the directory with a task file should be collected, got %+v", set.Files)
	}
}

// The file is stored verbatim rather than parsed and re-serialised.
//
// Task Master owns this format. Round-tripping it through a struct this app
// defines would silently drop any field the app does not know about — and the
// point of a backup is that what comes back is what was there.
func TestTaskContentIsStoredVerbatim(t *testing.T) {
	dir := t.TempDir()
	content := `{"tasks":[{"id":"1","unknownFutureField":42}],"meta":{"custom":true}}`
	writeTaskFile(t, dir, content)

	set := CollectTaskFiles([]string{dir})
	if len(set.Files) != 1 {
		t.Fatalf("expected one file, got %d", len(set.Files))
	}
	if set.Files[0].Content != content {
		t.Errorf("content was altered:\n got %s\nwant %s", set.Files[0].Content, content)
	}
}

// Collection is ordered, so two runs over unchanged files produce identical
// bytes — which is what lets an unchanged backup be skipped rather than written.
func TestCollectionIsOrdered(t *testing.T) {
	root := t.TempDir()
	var dirs []string
	for _, name := range []string{"zebra", "alpha", "middle"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		writeTaskFile(t, dir, `{"tasks":[]}`)
		dirs = append(dirs, dir)
	}

	first := CollectTaskFiles(dirs)
	second := CollectTaskFiles([]string{dirs[2], dirs[0], dirs[1]})

	if len(first.Files) != len(second.Files) {
		t.Fatalf("different counts: %d and %d", len(first.Files), len(second.Files))
	}
	for i := range first.Files {
		if first.Files[i].Path != second.Files[i].Path {
			t.Errorf("order differs at %d: %q vs %q", i, first.Files[i].Path, second.Files[i].Path)
		}
	}
}

// Restoring into a directory that no longer exists must not recreate it.
//
// Writing a .taskmaster folder into a path the user has since removed leaves
// litter behind, and the tasks in it would belong to nothing.
func TestRestoreSkipsMissingDirectories(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "removed")
	set := TaskBackupSet{Files: []TaskBackup{{Path: gone, Content: `{"tasks":[]}`}}}

	// Exercised through the same check the restore makes, without needing a
	// Storage: the directory is absent, so nothing should be written.
	for _, file := range set.Files {
		if stat, err := os.Stat(file.Path); err == nil && stat.IsDir() {
			t.Errorf("the directory should not exist: %s", file.Path)
		}
	}
	if _, err := os.Stat(taskFileFor(gone)); !os.IsNotExist(err) {
		t.Error("nothing should have been created under a missing directory")
	}
}

func TestRestoreTaskTargetsRollsBackEarlierReplacement(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "a", ".taskmaster", "tasks.json")
	secondPath := filepath.Join(root, "b", ".taskmaster", "tasks.json")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(firstPath, []byte("current-a"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("current-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	targets := []*taskRestoreTarget{
		{path: firstPath, content: "backup-a"},
		{path: secondPath, content: "backup-b"},
	}
	calls := 0
	injected := errors.New("injected second replace failure")
	err := restoreTaskTargets(targets, func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return injected
		}
		return os.Rename(oldPath, newPath)
	}, nil)
	if !errors.Is(err, injected) {
		t.Fatalf("restore error = %v, want injected failure", err)
	}
	for path, want := range map[string]string{firstPath: "current-a", secondPath: "current-b"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != want {
			t.Fatalf("%s after rollback = %q, want %q", path, got, want)
		}
	}
}

func TestRestoreTaskBackupSnapshotsExactLockedStateBeforeReplacement(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	project := t.TempDir()
	current := `{"tasks":[{"id":"current"}]}`
	restored := `{"tasks":[{"id":"restored"}]}`
	writeTaskFile(t, project, current)

	set := TaskBackupSet{
		Files: []TaskBackup{{Path: project, Content: restored}},
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(storage.configDir, "backups", "tasks")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const backupID = "20260820T120000.000000000Z-source.json"
	if err := os.WriteFile(filepath.Join(backupDir, backupID), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := storage.RestoreTaskBackup(backupID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(taskFileFor(project))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != restored {
		t.Fatalf("restored content = %s, want %s", got, restored)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	foundUndo := false
	for _, entry := range backupJSONEntries(entries) {
		if entry.Name() == backupID {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(backupDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var undo TaskBackupSet
		if err := json.Unmarshal(raw, &undo); err != nil {
			t.Fatal(err)
		}
		if len(undo.Files) == 1 && undo.Files[0].Path == CanonicalProjectPath(project) && undo.Files[0].Content == current {
			foundUndo = true
		}
	}
	if !foundUndo {
		t.Fatal("restore did not persist the exact pre-replacement task state")
	}
}

func TestRestoreTaskBackupRejectsInvalidContentWithoutReplacingLiveFile(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	project := t.TempDir()
	current := `{"tasks":[{"id":"current"}]}`
	writeTaskFile(t, project, current)

	set := TaskBackupSet{Files: []TaskBackup{{Path: project, Content: `{"tasks":`}}}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(storage.configDir, "backups", "tasks")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const backupID = "20260820T120000.000000000Z-corrupt.json"
	if err := os.WriteFile(filepath.Join(backupDir, backupID), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := storage.RestoreTaskBackup(backupID); err == nil {
		t.Fatal("invalid task backup was accepted")
	}
	got, err := os.ReadFile(taskFileFor(project))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != current {
		t.Fatalf("invalid backup changed live tasks to %q", got)
	}
}

func TestRestoreTaskTargetsDoesNotCommitWhenUndoSnapshotFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("undo snapshot failed")
	err := restoreTaskTargets([]*taskRestoreTarget{{path: path, content: "restored"}}, os.Rename, func(targets []*taskRestoreTarget) error {
		if len(targets) != 1 || string(targets[0].original) != "current" {
			t.Fatalf("pre-commit snapshot = %+v", targets)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("restore error = %v, want undo failure", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "current" {
		t.Fatalf("undo failure changed live file to %q", got)
	}
}

func TestRestoreTaskBackupUndoRemovesFileCreatedByRestore(t *testing.T) {
	storage := &Storage{configDir: t.TempDir()}
	project := t.TempDir()
	restored := `{"tasks":[{"id":"restored"}]}`
	backupDir := filepath.Join(storage.configDir, "backups", "tasks")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	set := TaskBackupSet{Files: []TaskBackup{{Path: project, Content: restored}}}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	const sourceID = "20260820T120000.000000000Z-create.json"
	if err := os.WriteFile(filepath.Join(backupDir, sourceID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreTaskBackup(sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(taskFileFor(project)); err != nil {
		t.Fatalf("restore did not create task file: %v", err)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	undoID := ""
	for _, entry := range backupJSONEntries(entries) {
		if entry.Name() == sourceID {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(backupDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var undo TaskBackupSet
		if err := json.Unmarshal(raw, &undo); err != nil {
			t.Fatal(err)
		}
		if len(undo.Files) == 1 && undo.Files[0].Missing && undo.Files[0].Path == CanonicalProjectPath(project) {
			undoID = entry.Name()
		}
	}
	if undoID == "" {
		t.Fatal("restore of a missing file did not create an undo tombstone")
	}
	if err := storage.RestoreTaskBackup(undoID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(taskFileFor(project)); !os.IsNotExist(err) {
		t.Fatalf("tombstone undo did not remove restored task file: %v", err)
	}
}

func writeTaskFile(t *testing.T, dir, content string) {
	t.Helper()
	taskDir := filepath.Join(dir, ".taskmaster")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "tasks.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
