package session

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// A project has a task list and a note of its own, belonging to no session.
//
// Session tasks live in each working directory's .taskmaster/tasks.json,
// because that is where Task Master keeps them. A project has no working
// directory — it is a grouping of sessions, not a place on disk — so its list
// lives beside its sessions.json instead, in the same format and behind the
// same TaskManager: priorities, tags, deadlines, subtasks, dependencies,
// locking and backups all work exactly as they do for a session's list.
//
// The note is a field of sessions.json itself, so it is backed up and
// restored with the rest of the project.

// ProjectDataDir is the directory holding the active project's own files: its
// sessions.json and, under .taskmaster, the project's task list.
func (s *Storage) ProjectDataDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return filepath.Dir(s.configPath)
}

// ProjectDataDirFor is ProjectDataDir for any project, active or not. The
// default project ("") keeps its files in the config root, as it always has.
func (s *Storage) ProjectDataDirFor(projectID string) (string, error) {
	if !validProjectID(projectID) {
		return "", fmt.Errorf("invalid project ID")
	}
	if projectID == "" {
		return s.configDir, nil
	}
	return filepath.Join(s.configDir, "projects", projectID), nil
}

// ProjectNotes returns the active project's note.
func (s *Storage) ProjectNotes() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return "", err
	}
	return data.Notes, nil
}

// SetProjectNotes replaces the active project's note. Loaded and written under
// the storage lock, like every other edit, so no other field is lost to a
// concurrent writer's older snapshot.
func (s *Storage) SetProjectNotes(notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	if data.Notes == notes {
		return nil
	}
	data.Notes = notes
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.writeStorageDataLocked(data, true)
}

// ErrMovedTaskChanged reports that the task was edited between being read for
// a move and being taken out of its list. The move is undone rather than
// losing that edit.
var ErrMovedTaskChanged = errors.New("task changed while it was being moved")

// TransferTask moves a task from one list to another — from the project's
// list into a session's, or back — and returns it as it now stands.
//
// sessionID and tabID are the task's new owner: a session and optionally one
// of its tabs when it moves into a session's list, both empty when it moves to
// the project.
//
// Everything that describes the work travels with it: title, description,
// details, status, priority, tags, deadline, subtasks, dates. The ID is kept
// when the destination does not already use it, and replaced otherwise.
//
// Dependencies do not travel. A dependency names a task in the SAME list, and
// the task has just left it: kept, it would point at nothing — or, worse, at an
// unrelated task that later gets the same ID. So the moved task's own
// dependencies are dropped, and the tasks it leaves behind stop waiting for it.
//
// Two files are written, and no lock covers both. The destination is written
// first: if taking the task out of the source then fails, the copy is removed
// again, so a failure leaves at worst what was there before — never a task in
// neither list.
func TransferTask(from, to *TaskManager, taskID, sessionID, tabID string) (*Task, error) {
	if tabID != "" && sessionID == "" {
		return nil, fmt.Errorf("a tab assignment needs a session")
	}
	if from.projectPath == to.projectPath {
		// Same file: nothing moves, only the owner changes.
		if err := from.UpdateTask(taskID, map[string]interface{}{"sessionId": sessionID, "tabId": tabID}); err != nil {
			return nil, err
		}
		return from.GetTask(taskID)
	}

	if err := from.Load(); err != nil {
		return nil, err
	}
	original, err := from.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	moved := cloneTask(*original)
	moved.SessionID = sessionID
	moved.TabID = tabID
	moved.Dependencies = []string{}
	moved.UpdatedAt = time.Now()
	if moved.Tags == nil {
		moved.Tags = []string{}
	}
	if moved.Subtasks == nil {
		moved.Subtasks = []Subtask{}
	}

	inserted, err := to.insertTask(moved)
	if err != nil {
		return nil, err
	}
	if err := from.takeTask(*original); err != nil {
		if rollbackErr := to.DeleteTask(inserted.ID); rollbackErr != nil {
			return nil, errors.Join(err, fmt.Errorf("the copy in the destination could not be removed: %w", rollbackErr))
		}
		return nil, err
	}
	return &inserted, nil
}

// insertTask appends a task that already exists elsewhere, keeping its ID
// unless this list already has a task of that ID.
func (tm *TaskManager) insertTask(task Task) (Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var inserted Task
	err := tm.mutateLocked(func() error {
		task = cloneTask(task)
		for _, existing := range tm.store.Tasks {
			if existing.ID == task.ID {
				task.ID = tm.generateTaskID()
				break
			}
		}
		if task.ID == "" {
			task.ID = tm.generateTaskID()
		}
		previousMeta := tm.store.Meta
		tm.store.Tasks = append(tm.store.Tasks, task)
		if err := tm.saveLocked(); err != nil {
			tm.store.Tasks = tm.store.Tasks[:len(tm.store.Tasks)-1]
			tm.store.Meta = previousMeta
			return err
		}
		inserted = task
		return nil
	})
	return cloneTask(inserted), err
}

// takeTask removes a task that has been copied elsewhere, and the
// dependencies other tasks in this list had on it.
//
// Refuses when the task is not as it was read: an edit made in the meantime
// exists only here, and removing the task would lose it.
func (tm *TaskManager) takeTask(expected Task) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		index := -1
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == expected.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("task not found: %s", expected.ID)
		}
		if !tm.store.Tasks[index].UpdatedAt.Equal(expected.UpdatedAt) {
			return ErrMovedTaskChanged
		}

		previousTasks := cloneTasks(tm.store.Tasks)
		previousMeta := tm.store.Meta
		remaining := make([]Task, 0, len(tm.store.Tasks)-1)
		for i, task := range tm.store.Tasks {
			if i == index {
				continue
			}
			if dependsOn(task, expected.ID) {
				task.Dependencies = withoutDependency(task.Dependencies, expected.ID)
				task.UpdatedAt = time.Now()
			}
			remaining = append(remaining, task)
		}
		tm.store.Tasks = remaining
		if err := tm.saveLocked(); err != nil {
			tm.store.Tasks = previousTasks
			tm.store.Meta = previousMeta
			return err
		}
		return nil
	})
}

func dependsOn(task Task, id string) bool {
	for _, dependency := range task.Dependencies {
		if dependency == id {
			return true
		}
	}
	return false
}

func withoutDependency(dependencies []string, id string) []string {
	kept := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency != id {
			kept = append(kept, dependency)
		}
	}
	return kept
}
