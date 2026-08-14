package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
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
	// The file's contents verbatim, not a parsed structure. Task Master owns
	// this format; storing what was read means a field this app does not know
	// about survives the round trip instead of being dropped on the way back.
	Content string `json:"content"`
}

// TaskBackupSet is what one backup run collected.
type TaskBackupSet struct {
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
	set := TaskBackupSet{CreatedAt: time.Now().UTC()}

	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		content, err := os.ReadFile(taskFileFor(dir))
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
	set := CollectTaskFiles(dirs)
	if len(set.Files) == 0 {
		return nil
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

	dir := s.taskBackupDirLocked()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	if unchanged, err := latestTaskBackupMatches(dir, comparable); err == nil && unchanged {
		return nil
	}

	raw, err := json.MarshalIndent(&set, "", "  ")
	if err != nil {
		return err
	}

	sum := sha256.Sum256(comparable)
	name := set.CreatedAt.Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(sum[:4]) + ".json"
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return pruneBackupDir(dir)
}

// latestTaskBackupMatches reports whether the newest snapshot holds the same
// files as what is about to be written.
func latestTaskBackupMatches(dir string, comparable []byte) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	files := backupJSONEntries(entries)
	if len(files) == 0 {
		return false, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	previous, err := os.ReadFile(filepath.Join(dir, files[len(files)-1].Name()))
	if err != nil {
		return false, err
	}
	var set TaskBackupSet
	if err := json.Unmarshal(previous, &set); err != nil {
		return false, err
	}
	current, err := json.Marshal(set.Files)
	if err != nil {
		return false, err
	}
	return string(current) == string(comparable), nil
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

	result := make([]BackupInfo, 0, len(entries))
	for _, entry := range backupJSONEntries(entries) {
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
	raw, err := os.ReadFile(path)
	s.mu.Unlock()
	if err != nil {
		return err
	}

	var set TaskBackupSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return err
	}

	// Everything about to be overwritten, kept first.
	var dirs []string
	for _, file := range set.Files {
		dirs = append(dirs, file.Path)
	}
	if err := s.BackupTaskFiles(dirs); err != nil {
		// Not fatal: a snapshot that cannot be taken should not stop a restore
		// the user has asked for, but it is worth saying so.
		fmt.Fprintf(os.Stderr, "task backup before restore failed: %v\n", err)
	}

	for _, file := range set.Files {
		if stat, err := os.Stat(file.Path); err != nil || !stat.IsDir() {
			continue
		}
		target := taskFileFor(file.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, []byte(file.Content), 0644); err != nil {
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}
