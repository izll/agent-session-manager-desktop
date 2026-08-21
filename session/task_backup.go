package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Tasks live outside the store that everything else is backed up from.
//
// sessions.json holds the sessions, groups, settings and trash; tasks are kept
// per working directory, in .taskmaster/tasks.json, because that is where Task
// Master puts them and the two have to agree. Nothing was watching those files,
// so a task deleted by accident — or by an agent editing the file — was gone.
// That matters more now they carry deadlines.

// TaskBackup is one directory's task file, as it stood at a moment in time.
type TaskBackup struct {
	// Absolute path of the working directory the tasks belong to. Restoring
	// writes back to the same place, so a directory that has since been moved
	// or removed is skipped rather than recreated somewhere arbitrary.
	Path string `json:"path"`
	// Missing is a tombstone used by the pre-restore undo snapshot. It records
	// that the target did not exist before a restore created it, so applying the
	// undo snapshot removes that newly created file. Older backups omit this
	// field and therefore retain their original "content exists" meaning.
	Missing bool `json:"missing,omitempty"`
	// The file's contents verbatim, not a parsed structure. Task Master owns
	// this format; storing what was read means a field this app does not know
	// about survives the round trip instead of being dropped on the way back.
	Content string `json:"content"`
}

// TaskBackupSet is what one backup run collected.
type TaskBackupSet struct {
	// ProjectID pins a snapshot to the project that created it. The pointer is
	// intentional: an empty string is the real ID of the default project,
	// while nil identifies a legacy snapshot written before project scoping.
	ProjectID *string      `json:"projectId,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
	Files     []TaskBackup `json:"files"`
}

// taskFileFor returns where a working directory keeps its tasks.
func taskFileFor(dir string) string {
	return filepath.Join(dir, ".taskmaster", "tasks.json")
}

// CollectTaskFiles reads the task file of every distinct directory given.
//
// Directories are deduplicated: several sessions often share one working
// directory, and so share one task file — reading it per session would store
// the same content several times over.
//
// A directory with no task file is not an error; most have none.
func CollectTaskFiles(dirs []string) TaskBackupSet {
	return collectTaskFilesAtMost(dirs, maxCanonicalStorageBytes)
}

func collectTaskFilesAtMost(dirs []string, limit int64) TaskBackupSet {
	set := TaskBackupSet{CreatedAt: time.Now().UTC()}

	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		taskRoot, err := openProjectTaskRoot(dir, false)
		if err != nil {
			continue
		}
		content, err := readRootFileAtMost(taskRoot, "tasks.json", limit)
		_ = taskRoot.Close()
		if err != nil {
			continue
		}
		set.Files = append(set.Files, TaskBackup{Path: dir, Content: string(content)})
	}

	// Sorted so two runs over the same state produce identical bytes, which is
	// what lets the caller skip writing a backup that changed nothing.
	sort.Slice(set.Files, func(i, j int) bool { return set.Files[i].Path < set.Files[j].Path })
	return set
}

// collectTaskFilesForBackup is the fail-closed collection path used by an
// explicit backup. A missing task file is normal; an unreadable or oversized
// one is not. Treating both alike made CreateBackup report success while
// silently omitting the very task data the user meant to protect.
func collectTaskFilesForBackup(dirs []string, limit int64) (TaskBackupSet, error) {
	set := TaskBackupSet{CreatedAt: time.Now().UTC()}
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		canonical := CanonicalProjectPath(dir)
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		taskRoot, err := openProjectTaskRoot(canonical, false)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return TaskBackupSet{}, fmt.Errorf("cannot open task directory for %s: %w", canonical, err)
		}
		content, err := readRootFileAtMost(taskRoot, "tasks.json", limit)
		_ = taskRoot.Close()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return TaskBackupSet{}, fmt.Errorf("cannot back up tasks for %s: %w", canonical, err)
		}
		set.Files = append(set.Files, TaskBackup{Path: canonical, Content: string(content)})
	}
	sort.Slice(set.Files, func(i, j int) bool { return set.Files[i].Path < set.Files[j].Path })
	return set, nil
}

// taskBackupDir is where task snapshots are kept, beside the main backups.
func (s *Storage) taskBackupDirLocked() string {
	return filepath.Join(s.configDir, "backups", "tasks")
}

// BackupTaskFiles writes a snapshot of the task files for the given
// directories, and thins the history the same way the main backups are thinned.
//
// Returns nil when there is nothing to save or nothing has changed — a backup
// identical to the last one is noise that pushes real history out.
func (s *Storage) BackupTaskFiles(dirs []string) error {
	set, err := collectTaskFilesForBackup(dirs, maxCanonicalStorageBytes)
	if err != nil {
		return err
	}
	return s.writeTaskBackupSet(set)
}

// writeTaskBackupSet persists an already collected snapshot. Restore uses this
// while holding every target task-file lock, so the undo point is the exact
// state that will be replaced rather than an earlier, racy observation.
func (s *Storage) writeTaskBackupSet(set TaskBackupSet) error {
	if len(set.Files) == 0 {
		return nil
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = time.Now().UTC()
	}

	// The timestamp is excluded from the comparison: two runs a minute apart
	// over unchanged files differ only there, and writing for that alone would
	// fill the directory with copies of the same state.
	comparable, err := json.Marshal(set.Files)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if set.ProjectID == nil {
		projectID := s.projectID
		set.ProjectID = &projectID
	} else if *set.ProjectID != s.projectID {
		return fmt.Errorf("task backup project changed: expected %q, got %q", *set.ProjectID, s.projectID)
	}

	dir := s.taskBackupDirLocked()
	return withCrossProcessFileLock(filepath.Join(dir, ".backup.lock"), func() error {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}

		if unchanged, err := latestTaskBackupMatches(dir, *set.ProjectID, comparable); err == nil && unchanged {
			return nil
		}

		raw, err := json.MarshalIndent(&set, "", "  ")
		if err != nil {
			return err
		}

		// The directory is shared for backward compatibility with snapshots from
		// before project scoping. Include the project identity in the filename:
		// two projects can otherwise publish the same name when an explicit
		// backup captures identical task files at the same timestamp, and the
		// second rename silently replaces the first project's recovery point.
		hashInput := make([]byte, 0, len(*set.ProjectID)+1+len(comparable))
		hashInput = append(hashInput, (*set.ProjectID)...)
		hashInput = append(hashInput, 0)
		hashInput = append(hashInput, comparable...)
		sum := sha256.Sum256(hashInput)
		name := set.CreatedAt.Format(backupTimestampLayout) + "-" + hex.EncodeToString(sum[:4]) + ".json"
		path := filepath.Join(dir, name)
		tmp, err := os.CreateTemp(dir, ".task-backup-*")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if err := tmp.Chmod(0600); err == nil {
			_, err = tmp.Write(raw)
		}
		if err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
		return pruneTaskBackupDir(dir, *set.ProjectID)
	})
}

// pruneTaskBackupDir applies the normal retention bands and hard ceiling to
// one project's modern snapshots only. Task backups historically shared one
// directory, so using pruneBackupDir here let a busy project thin or evict a
// quiet project's recovery history. Unscoped legacy snapshots are deliberately
// left alone: ownership can only be inferred from the current sessions file,
// and deleting one during another project's write would not be fail-closed.
func pruneTaskBackupDir(dir, projectID string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type scopedBackup struct {
		entry os.DirEntry
		time  time.Time
	}
	files := make([]scopedBackup, 0, len(entries))
	for _, entry := range backupJSONEntries(entries) {
		storedProjectID, scoped, err := taskBackupProjectScope(filepath.Join(dir, entry.Name()))
		if err != nil || !scoped || storedProjectID != projectID {
			continue
		}
		files = append(files, scopedBackup{entry: entry, time: backupTime(entry.Name())})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].entry.Name() < files[j].entry.Name() })
	times := make([]time.Time, len(files))
	for index := range files {
		times[index] = files[index].time
	}
	keep := backupsToKeep(times, time.Now().UTC())
	remaining := make([]scopedBackup, 0, len(files))
	for index, file := range files {
		if !keep[index] {
			if err := os.Remove(filepath.Join(dir, file.entry.Name())); err != nil {
				return err
			}
			continue
		}
		remaining = append(remaining, file)
	}
	ceilingNow := time.Now().UTC()
	for len(remaining) > backupHardCeiling {
		removeIndex := backupCeilingRemovalIndex(remaining[len(remaining)-1].time, len(remaining), ceilingNow)
		if err := os.Remove(filepath.Join(dir, remaining[removeIndex].entry.Name())); err != nil {
			return err
		}
		remaining = append(remaining[:removeIndex], remaining[removeIndex+1:]...)
	}
	return nil
}

// latestTaskBackupMatches reports whether the newest snapshot holds the same
// files as what is about to be written.
func latestTaskBackupMatches(dir, projectID string, comparable []byte) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	files := backupJSONEntries(entries)
	if len(files) == 0 {
		return false, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	for index := len(files) - 1; index >= 0; index-- {
		previous, err := readFileAtMost(filepath.Join(dir, files[index].Name()), maxCanonicalStorageBytes)
		if err != nil {
			continue
		}
		var set TaskBackupSet
		if err := json.Unmarshal(previous, &set); err != nil || set.ProjectID == nil || *set.ProjectID != projectID {
			continue
		}
		current, err := json.Marshal(set.Files)
		if err != nil {
			return false, err
		}
		return string(current) == string(comparable), nil
	}
	return false, nil
}

// ListTaskBackups returns the task snapshots, newest first.
func (s *Storage) ListTaskBackups() ([]BackupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.taskBackupDirLocked())
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}

	data, err := s.loadStorageDataLocked()
	if err != nil {
		return nil, err
	}
	allowed := taskBackupAllowedPaths(data)
	result := make([]BackupInfo, 0, len(entries))
	for _, entry := range backupJSONEntries(entries) {
		backupPath := filepath.Join(s.taskBackupDirLocked(), entry.Name())
		storedProjectID, scoped, err := taskBackupProjectScope(backupPath)
		if err != nil {
			continue
		}
		if scoped {
			if storedProjectID != s.projectID {
				continue
			}
		} else {
			// Legacy snapshots have no project ID. They are uncommon and need a
			// one-time full path inspection to infer their owner safely.
			raw, err := readFileAtMost(backupPath, maxCanonicalStorageBytes)
			if err != nil {
				continue
			}
			var set TaskBackupSet
			if json.Unmarshal(raw, &set) != nil || !taskBackupBelongsToProject(&set, s.projectID, allowed) {
				continue
			}
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, BackupInfo{
			ID:        entry.Name(),
			CreatedAt: backupTime(entry.Name()),
			Size:      info.Size(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

// taskBackupProjectScope reads only the leading metadata of a modern backup.
// Full task contents may be tens of megabytes; ListTaskBackups must not decode
// every content string merely to decide which project's list it belongs in.
func taskBackupProjectScope(path string) (projectID string, scoped bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxCanonicalStorageBytes+1))
	token, err := decoder.Token()
	if err != nil {
		return "", false, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return "", false, fmt.Errorf("invalid task backup object")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", false, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return "", false, fmt.Errorf("invalid task backup key")
		}
		if key == "projectId" {
			if err := decoder.Decode(&projectID); err != nil {
				return "", false, err
			}
			return projectID, true, nil
		}
		if key == "files" {
			return "", false, nil
		}
		var ignored json.RawMessage
		if err := decoder.Decode(&ignored); err != nil {
			return "", false, err
		}
	}
	return "", false, nil
}

func taskBackupAllowedPaths(data *StorageData) map[string]struct{} {
	allowed := make(map[string]struct{})
	add := func(instance *Instance) {
		if instance != nil && instance.Path != "" {
			allowed[CanonicalProjectPath(instance.Path)] = struct{}{}
		}
	}
	if data == nil {
		return allowed
	}
	for _, instance := range data.Instances {
		add(instance)
	}
	for _, entry := range data.Trash {
		if entry != nil {
			add(entry.Session)
		}
	}
	return allowed
}

func taskBackupBelongsToProject(set *TaskBackupSet, projectID string, allowed map[string]struct{}) bool {
	if set == nil || len(set.Files) == 0 {
		return false
	}
	if set.ProjectID != nil && *set.ProjectID != projectID {
		return false
	}
	// Paths are validated even for newly scoped snapshots. Backup files are
	// local input and may be corrupted or synced from another machine; a forged
	// projectId must not turn restore into an arbitrary tasks.json write.
	for _, file := range set.Files {
		if strings.TrimSpace(file.Path) == "" {
			return false
		}
		if _, ok := allowed[CanonicalProjectPath(file.Path)]; !ok {
			return false
		}
	}
	return true
}

// RestoreTaskBackup writes a snapshot's task files back where they came from.
//
// A directory that no longer exists is skipped rather than recreated: putting a
// .taskmaster folder into a path the user has since removed leaves litter, and
// the tasks would belong to nothing.
//
// The current state is snapshotted first, so restoring is itself undoable.
func (s *Storage) RestoreTaskBackup(id string) error {
	if !validBackupID(id) {
		return fmt.Errorf("invalid backup id")
	}

	s.mu.Lock()
	path := filepath.Join(s.taskBackupDirLocked(), id)
	raw, err := readFileAtMost(path, maxCanonicalStorageBytes)
	var currentProjectID string
	var allowed map[string]struct{}
	if err == nil {
		currentProjectID = s.projectID
		data, loadErr := s.loadStorageDataLocked()
		if loadErr != nil {
			err = loadErr
		} else {
			allowed = taskBackupAllowedPaths(data)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}

	var set TaskBackupSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return err
	}
	if !taskBackupBelongsToProject(&set, currentProjectID, allowed) {
		return fmt.Errorf("task backup does not belong to the active project")
	}

	targetByPath := make(map[string]*taskRestoreTarget)
	defer func() {
		for _, target := range targetByPath {
			if target.root != nil {
				_ = target.root.Close()
			}
		}
	}()
	for _, file := range set.Files {
		if !file.Missing {
			if err := validateTaskBackupContent(file.Content); err != nil {
				return fmt.Errorf("invalid task snapshot for %s: %w", file.Path, err)
			}
		} else if file.Content != "" {
			return fmt.Errorf("invalid task tombstone for %s: contains file content", file.Path)
		}
		projectPath := CanonicalProjectPath(file.Path)
		if stat, err := os.Stat(projectPath); err != nil || !stat.IsDir() {
			continue
		}
		taskRoot, err := openProjectTaskRoot(projectPath, !file.Missing)
		if os.IsNotExist(err) && file.Missing {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot safely open task directory for %s: %w", projectPath, err)
		}
		target := taskFileFor(projectPath)
		if existing := targetByPath[target]; existing != nil {
			_ = taskRoot.Close()
			if existing.remove != file.Missing || existing.content != file.Content {
				return fmt.Errorf("backup contains conflicting snapshots for %s", target)
			}
			continue
		}
		targetByPath[target] = &taskRestoreTarget{path: target, content: file.Content, remove: file.Missing, root: taskRoot}
	}
	targets := make([]*taskRestoreTarget, 0, len(targetByPath))
	for _, target := range targetByPath {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].path < targets[j].path })
	return restoreTaskTargets(targets, os.Rename, func(lockedTargets []*taskRestoreTarget) error {
		undo := TaskBackupSet{ProjectID: &currentProjectID, CreatedAt: time.Now().UTC()}
		for _, target := range lockedTargets {
			entry := TaskBackup{
				Path:    filepath.Dir(filepath.Dir(target.path)),
				Content: string(target.original),
			}
			if !target.existed {
				entry.Content = ""
				entry.Missing = true
			}
			undo.Files = append(undo.Files, entry)
		}
		if err := s.writeTaskBackupSet(undo); err != nil {
			return fmt.Errorf("failed to create task backup before restore: %w", err)
		}
		return nil
	})
}

func validateTaskBackupContent(content string) error {
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("contains multiple JSON values")
		}
		return fmt.Errorf("contains trailing data: %w", err)
	}
	if _, ok := document["tasks"].([]interface{}); !ok {
		return fmt.Errorf("tasks field is not an array")
	}
	return nil
}

type taskRestoreTarget struct {
	path         string
	content      string
	remove       bool
	root         *os.Root
	stagedPath   string
	original     []byte
	originalMode os.FileMode
	existed      bool
}

func restoreTaskTargets(targets []*taskRestoreTarget, replace func(string, string) error, beforeCommit func([]*taskRestoreTarget) error) error {
	return withTaskRestoreTargetLocks(targets, func() error {
		for _, target := range targets {
			if target.remove {
				continue
			}
			if err := stageTaskRestoreTarget(target); err != nil {
				cleanupTaskRestoreStages(targets)
				return err
			}
		}
		defer cleanupTaskRestoreStages(targets)

		for _, target := range targets {
			info, err := target.stat()
			switch {
			case err == nil && !info.Mode().IsRegular():
				return fmt.Errorf("task restore target is not a regular file: %s", target.path)
			case err == nil:
				target.original, err = target.read(maxCanonicalStorageBytes)
				if err != nil {
					return err
				}
				target.originalMode = info.Mode().Perm()
				target.existed = true
			case os.IsNotExist(err):
				target.originalMode = 0o644
			case err != nil:
				return err
			}
			if !target.remove {
				if err := target.chmodStaged(target.originalMode); err != nil {
					return err
				}
			}
		}
		if beforeCommit != nil {
			if err := beforeCommit(targets); err != nil {
				return err
			}
		}

		committed := 0
		for i, target := range targets {
			var err error
			if target.remove {
				err = target.removeFile()
				if os.IsNotExist(err) {
					err = nil
				}
			} else {
				err = target.replaceStaged(replace)
			}
			if err != nil {
				rollbackErr := rollbackTaskRestoreTargets(targets[:committed])
				return errors.Join(fmt.Errorf("failed to restore %s: %w", target.path, err), rollbackErr)
			}
			if !target.remove {
				target.stagedPath = ""
			}
			committed = i + 1
		}
		return nil
	})
}

func withTaskRestoreTargetLocks(targets []*taskRestoreTarget, action func() error) error {
	var acquire func(int) error
	acquire = func(index int) error {
		if index == len(targets) {
			return action()
		}
		target := targets[index]
		if target.root != nil {
			return withCrossProcessRootFileLock(target.root, "tasks.json.lock", func() error { return acquire(index + 1) })
		}
		return withCrossProcessFileLock(target.path+".lock", func() error { return acquire(index + 1) })
	}
	return acquire(0)
}

func (target *taskRestoreTarget) stat() (os.FileInfo, error) {
	if target.root != nil {
		return target.root.Stat("tasks.json")
	}
	return os.Stat(target.path)
}

func (target *taskRestoreTarget) read(limit int64) ([]byte, error) {
	if target.root != nil {
		return readRootFileAtMost(target.root, "tasks.json", limit)
	}
	return readFileAtMost(target.path, limit)
}

func (target *taskRestoreTarget) chmodStaged(mode os.FileMode) error {
	if target.root != nil {
		return target.root.Chmod(target.stagedPath, mode)
	}
	return os.Chmod(target.stagedPath, mode)
}

func (target *taskRestoreTarget) removeFile() error {
	if target.root != nil {
		return target.root.Remove("tasks.json")
	}
	return os.Remove(target.path)
}

func (target *taskRestoreTarget) replaceStaged(replace func(string, string) error) error {
	if target.root != nil {
		return target.root.Rename(target.stagedPath, "tasks.json")
	}
	return replace(target.stagedPath, target.path)
}

func (target *taskRestoreTarget) restoreBytes(content []byte, mode os.FileMode) error {
	if target.root == nil {
		return atomicRestoreTaskBytes(target.path, content, mode)
	}
	tmpName := ".task-rollback-" + uuid.NewString()
	tmp, err := target.root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer target.root.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return target.root.Rename(tmpName, "tasks.json")
}

func stageTaskRestoreTarget(target *taskRestoreTarget) error {
	if target.root != nil {
		target.stagedPath = ".task-restore-" + uuid.NewString()
		tmp, err := target.root.OpenFile(target.stagedPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := tmp.Chmod(0o644); err == nil {
			_, err = tmp.Write([]byte(target.content))
		}
		if err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target.path), ".task-restore-*")
	if err != nil {
		return err
	}
	target.stagedPath = tmp.Name()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write([]byte(target.content)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	return tmp.Close()
}

func cleanupTaskRestoreStages(targets []*taskRestoreTarget) {
	for _, target := range targets {
		if target.stagedPath != "" {
			if target.root != nil {
				_ = target.root.Remove(target.stagedPath)
			} else {
				_ = os.Remove(target.stagedPath)
			}
			target.stagedPath = ""
		}
	}
}

func rollbackTaskRestoreTargets(targets []*taskRestoreTarget) error {
	var rollbackErr error
	for i := len(targets) - 1; i >= 0; i-- {
		target := targets[i]
		if !target.existed {
			if err := target.removeFile(); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("failed to remove restored %s: %w", target.path, err))
			}
			continue
		}
		if err := target.restoreBytes(target.original, target.originalMode); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("failed to roll back %s: %w", target.path, err))
		}
	}
	return rollbackErr
}

func atomicRestoreTaskBytes(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".task-rollback-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
