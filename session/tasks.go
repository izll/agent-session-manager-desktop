package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusBacklog    TaskStatus = "backlog"
	TaskStatusInProgress TaskStatus = "in-progress"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusDeferred   TaskStatus = "deferred"
)

// TaskPriority represents the priority of a task
type TaskPriority string

const (
	TaskPriorityLow      TaskPriority = "low"
	TaskPriorityMedium   TaskPriority = "medium"
	TaskPriorityHigh     TaskPriority = "high"
	TaskPriorityCritical TaskPriority = "critical"
)

// Task represents a single task
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Details is the long-form body the edit dialog offers alongside the short
	// description. Task Master has a field for it, so the local store needs one
	// too — without it, editing a task through the app's own storage silently
	// dropped whatever was typed there.
	Details      string       `json:"details,omitempty"`
	Status       TaskStatus   `json:"status"`
	Priority     TaskPriority `json:"priority"`
	Tags         []string     `json:"tags"`
	Subtasks     []Subtask    `json:"subtasks"`
	Dependencies []string     `json:"dependencies"` // Task IDs
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	CompletedAt  *time.Time   `json:"completedAt,omitempty"`

	// DueAt is when the task is due, date and time.
	//
	// A pointer, so "no deadline" is distinguishable from the zero time — most
	// tasks never get one, and a zero time would sort as the year 1 and show as
	// overdue by two millennia.
	DueAt *time.Time `json:"dueAt,omitempty"`

	// SessionID ties the task to the session it belongs to, so closing that
	// session can say what is still outstanding. Empty means the task belongs
	// to the project as a whole rather than to any one session.
	SessionID string `json:"sessionId,omitempty"`
}

// Overdue reports whether the deadline has passed and the task is not finished.
//
// A completed task is never overdue however long it sat there: the deadline
// stopped mattering when the work was done.
func (t Task) Overdue(now time.Time) bool {
	return t.DueAt != nil && t.Status != TaskStatusDone && now.After(*t.DueAt)
}

// Unfinished reports whether the task still needs work.
//
// Deferred counts as unfinished: it was put off, not dealt with, and closing a
// session that still has deferred work is exactly the case worth a warning.
func (t Task) Unfinished() bool {
	return t.Status != TaskStatusDone
}

// Subtask represents a subtask within a task
type Subtask struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Details     string     `json:"details,omitempty"`
	Status      TaskStatus `json:"status,omitempty"`
	Done        bool       `json:"done"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// TaskStore holds all tasks for a project
type TaskStore struct {
	Meta  TaskStoreMeta `json:"meta"`
	Tasks []Task        `json:"tasks"`
}

// TaskStoreMeta contains metadata about the task store
type TaskStoreMeta struct {
	Version     string    `json:"version"`
	ProjectName string    `json:"projectName"`
	ProjectPath string    `json:"projectPath"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// TaskManager handles task operations for a project
type TaskManager struct {
	mu          sync.RWMutex
	projectPath string
	store       *TaskStore
}

// NewTaskManager creates a new task manager for a project path
func NewTaskManager(projectPath string) *TaskManager {
	return &TaskManager{
		projectPath: CanonicalProjectPath(projectPath),
	}
}

// CanonicalProjectPath gives every spelling of the same working directory one
// task-store identity. Without resolving symlinks, two sessions could create
// independent caches for /repo and /link-to-repo and overwrite each other's
// complete tasks.json snapshots.
func CanonicalProjectPath(projectPath string) string {
	if absolute, err := filepath.Abs(projectPath); err == nil {
		projectPath = absolute
	}
	projectPath = filepath.Clean(projectPath)
	if resolved, err := filepath.EvalSymlinks(projectPath); err == nil {
		projectPath = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		projectPath = strings.ToLower(projectPath)
	}
	return projectPath
}

// getTaskFilePath returns the path to the tasks.json file
func (tm *TaskManager) getTaskFilePath() string {
	return filepath.Join(tm.projectPath, ".taskmaster", "tasks.json")
}

// ensureTaskDir ensures the .taskmaster directory exists
func (tm *TaskManager) ensureTaskDir() error {
	dir := filepath.Join(tm.projectPath, ".taskmaster")
	return os.MkdirAll(dir, 0755)
}

// Load loads tasks from the project's .taskmaster/tasks.json
func (tm *TaskManager) Load() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.loadLocked()
}

func (tm *TaskManager) loadLocked() error {
	filePath := tm.getTaskFilePath()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty store
			tm.store = &TaskStore{
				Meta: TaskStoreMeta{
					Version:     "1.0",
					ProjectName: filepath.Base(tm.projectPath),
					ProjectPath: tm.projectPath,
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				},
				Tasks: []Task{},
			}
			return nil
		}
		return fmt.Errorf("failed to read tasks file: %w", err)
	}

	var store TaskStore
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("failed to parse tasks file: %w", err)
	}

	tm.store = &store
	return nil
}

// Save saves tasks to the project's .taskmaster/tasks.json
func (tm *TaskManager) Save() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return withCrossProcessFileLock(tm.getTaskFilePath()+".lock", tm.saveLocked)
}

func (tm *TaskManager) saveLocked() error {
	if tm.store == nil {
		return fmt.Errorf("no task store loaded")
	}

	if err := tm.ensureTaskDir(); err != nil {
		return fmt.Errorf("failed to create task directory: %w", err)
	}

	tm.store.Meta.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(tm.store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize tasks: %w", err)
	}

	filePath := tm.getTaskFilePath()
	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".tasks-*")
	if err != nil {
		return fmt.Errorf("failed to create tasks temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set tasks temp permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write tasks file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync tasks file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close tasks file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("failed to replace tasks file: %w", err)
	}

	return nil
}

// mutateLocked serialises a complete read-modify-write transaction with every
// other process using this task file. Reloading after the OS lock is acquired
// is essential: the in-memory store may predate another app instance's save.
func (tm *TaskManager) mutateLocked(action func() error) error {
	return withCrossProcessFileLock(tm.getTaskFilePath()+".lock", func() error {
		if err := tm.loadLocked(); err != nil {
			return err
		}
		return action()
	})
}

// GetTasks returns all tasks
func (tm *TaskManager) GetTasks() []Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.store == nil {
		return []Task{}
	}
	return cloneTasks(tm.store.Tasks)
}

// GetTasksByStatus returns tasks filtered by status
func (tm *TaskManager) GetTasksByStatus(status TaskStatus) []Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.store == nil {
		return []Task{}
	}

	var filtered []Task
	for _, task := range tm.store.Tasks {
		if task.Status == status {
			filtered = append(filtered, cloneTask(task))
		}
	}
	return filtered
}

// UnfinishedForSession returns the tasks tied to a session that are not done.
//
// Used to warn before a session is closed or deleted. Only tasks explicitly
// assigned to the session count: a project-wide task is not this session's
// business, and warning about it on every close would train the warning away.
func (tm *TaskManager) UnfinishedForSession(sessionID string) []Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.store == nil || sessionID == "" {
		return nil
	}

	var pending []Task
	for _, task := range tm.store.Tasks {
		if task.SessionID == sessionID && task.Unfinished() {
			pending = append(pending, cloneTask(task))
		}
	}
	return pending
}

// GetTask returns a task by ID
func (tm *TaskManager) GetTask(id string) (*Task, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.store == nil {
		return nil, fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == id {
			copy := cloneTask(tm.store.Tasks[i])
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("task not found: %s", id)
}

// generateTaskID generates a unique task ID
func (tm *TaskManager) generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

// CreateTask creates a new task
func (tm *TaskManager) CreateTask(title, description string, priority TaskPriority, tags []string) (*Task, error) {
	return tm.createTask(title, description, priority, tags, "")
}

// CreateTaskForSession persists ownership in the same transaction as creation.
func (tm *TaskManager) CreateTaskForSession(title, description string, priority TaskPriority, tags []string, sessionID string) (*Task, error) {
	return tm.createTask(title, description, priority, tags, sessionID)
}

func (tm *TaskManager) createTask(title, description string, priority TaskPriority, tags []string, sessionID string) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var created Task
	err := tm.mutateLocked(func() error {
		created = Task{
			ID:           tm.generateTaskID(),
			Title:        title,
			Description:  description,
			Status:       TaskStatusBacklog,
			Priority:     priority,
			Tags:         append([]string(nil), tags...),
			Subtasks:     []Subtask{},
			Dependencies: []string{},
			SessionID:    sessionID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		previousMeta := tm.store.Meta
		tm.store.Tasks = append(tm.store.Tasks, created)
		if err := tm.saveLocked(); err != nil {
			tm.store.Tasks = tm.store.Tasks[:len(tm.store.Tasks)-1]
			tm.store.Meta = previousMeta
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	copy := cloneTask(created)
	return &copy, nil
}

// UpdateTask updates an existing task
func (tm *TaskManager) UpdateTask(id string, updates map[string]interface{}) error {
	var dueAt *time.Time
	_, dueAtPresent := updates["dueAt"]
	if dueAtPresent {
		text, ok := updates["dueAt"].(string)
		if !ok {
			return fmt.Errorf("dueAt must be an RFC 3339 string")
		}
		if text != "" {
			parsed, err := time.Parse(time.RFC3339, text)
			if err != nil {
				return fmt.Errorf("invalid dueAt: %w", err)
			}
			dueAt = &parsed
		}
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == id {
				previous := cloneTask(tm.store.Tasks[i])
				previousMeta := tm.store.Meta
				task := &tm.store.Tasks[i]

				if title, ok := updates["title"].(string); ok {
					task.Title = title
				}
				if details, ok := updates["details"].(string); ok {
					task.Details = details
				}
				if desc, ok := updates["description"].(string); ok {
					task.Description = desc
				}
				if status, ok := updates["status"].(string); ok {
					task.Status = TaskStatus(status)
					if task.Status == TaskStatusDone {
						now := time.Now()
						task.CompletedAt = &now
					} else {
						task.CompletedAt = nil
					}
				}
				if priority, ok := updates["priority"].(string); ok {
					task.Priority = TaskPriority(priority)
				}
				// The deadline arrives as an RFC 3339 string, and an empty one
				// clears it — the edit dialog has to be able to take a deadline
				// back off a task, not only put one on.
				//
				// Checked with a comma-ok on the key rather than on the type: a
				// caller that is not touching the deadline omits the key entirely,
				// and a caller clearing it sends "". Without the distinction, every
				// unrelated edit would wipe the deadline.
				if dueAtPresent {
					task.DueAt = dueAt
				}
				if raw, present := updates["sessionId"]; present {
					if text, ok := raw.(string); ok {
						task.SessionID = text
					}
				}
				// Subtasks and dependencies arrive as whole lists rather than as
				// add/remove operations: the caller already holds the task it is
				// editing, and replacing the list is one write instead of a pair of
				// endpoints per collection. They come over the Wails bridge as
				// []interface{}, so each element is converted rather than asserted.
				if subtasks, ok := updates["subtasks"].([]interface{}); ok {
					task.Subtasks = toSubtasks(subtasks)
				}
				if deps, ok := updates["dependencies"].([]interface{}); ok {
					task.Dependencies = toStringList(deps)
				}
				if tags, ok := updates["tags"].([]string); ok {
					task.Tags = append([]string(nil), tags...)
				} else if tags, ok := updates["tags"].([]interface{}); ok {
					task.Tags = toStringList(tags)
				}

				task.UpdatedAt = time.Now()
				if err := tm.saveLocked(); err != nil {
					tm.store.Tasks[i] = previous
					tm.store.Meta = previousMeta
					return err
				}
				return nil
			}
		}

		return fmt.Errorf("task not found: %s", id)
	})
}

// toSubtasks converts what the frontend sends into the stored shape. Anything
// that is not a well-formed subtask is skipped rather than stored half-built.
func toSubtasks(raw []interface{}) []Subtask {
	out := make([]Subtask, 0, len(raw))
	for _, item := range raw {
		fields, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := fields["id"].(string)
		title, _ := fields["title"].(string)
		if id == "" || title == "" {
			continue
		}
		description, _ := fields["description"].(string)
		details, _ := fields["details"].(string)
		sub := Subtask{ID: id, Title: title, Description: description, Details: details}
		if status, ok := fields["status"].(string); ok {
			sub.Status = normalizeSubtaskStatus(status)
			sub.Done = sub.Status == TaskStatusDone
		} else if done, ok := fields["done"].(bool); ok {
			sub.Done = done
			if done {
				sub.Status = TaskStatusDone
			} else {
				sub.Status = TaskStatusBacklog
			}
		} else {
			sub.Status = TaskStatusBacklog
		}
		if created, ok := fields["createdAt"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, created); err == nil {
				sub.CreatedAt = parsed
			}
		}
		if sub.CreatedAt.IsZero() {
			sub.CreatedAt = time.Now()
		}
		out = append(out, sub)
	}
	return out
}

func normalizeSubtaskStatus(status string) TaskStatus {
	if status == "" || status == "pending" {
		return TaskStatusBacklog
	}
	return TaskStatus(status)
}

// toStringList converts a JSON array of strings, dropping anything else.
func toStringList(raw []interface{}) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DeleteTask deletes a task by ID
func (tm *TaskManager) DeleteTask(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == id {
				previousTasks := cloneTasks(tm.store.Tasks)
				previousMeta := tm.store.Meta
				tm.store.Tasks = append(tm.store.Tasks[:i], tm.store.Tasks[i+1:]...)
				if err := tm.saveLocked(); err != nil {
					tm.store.Tasks = previousTasks
					tm.store.Meta = previousMeta
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("task not found: %s", id)
	})
}

// RestoreTask atomically puts a previously deleted task back with its original
// identity. Keeping the ID is essential: other tasks may still depend on it,
// and re-creating through CreateTask would silently break those reverse links.
func (tm *TaskManager) RestoreTask(task Task) error {
	if task.ID == "" {
		return fmt.Errorf("restored task has no ID")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == task.ID {
				return fmt.Errorf("task already exists: %s", task.ID)
			}
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now()
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
		}
		task = cloneTask(task)
		previousMeta := tm.store.Meta
		tm.store.Tasks = append(tm.store.Tasks, task)
		if err := tm.saveLocked(); err != nil {
			tm.store.Tasks = tm.store.Tasks[:len(tm.store.Tasks)-1]
			tm.store.Meta = previousMeta
			return err
		}
		return nil
	})
}

// MoveTask changes the status of a task
func (tm *TaskManager) MoveTask(id string, newStatus TaskStatus) error {
	return tm.UpdateTask(id, map[string]interface{}{
		"status": string(newStatus),
	})
}

// AddSubtask adds a subtask to a task
func (tm *TaskManager) AddSubtask(taskID, title string) (*Subtask, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var created Subtask
	err := tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID != taskID {
				continue
			}
			previous := cloneTask(tm.store.Tasks[i])
			previousMeta := tm.store.Meta
			created = Subtask{
				ID:        fmt.Sprintf("subtask_%d", time.Now().UnixNano()),
				Title:     title,
				Status:    TaskStatusBacklog,
				Done:      false,
				CreatedAt: time.Now(),
			}
			tm.store.Tasks[i].Subtasks = append(tm.store.Tasks[i].Subtasks, created)
			tm.store.Tasks[i].UpdatedAt = time.Now()

			if err := tm.saveLocked(); err != nil {
				tm.store.Tasks[i] = previous
				tm.store.Meta = previousMeta
				return err
			}
			return nil
		}
		return fmt.Errorf("task not found: %s", taskID)
	})
	if err != nil {
		return nil, err
	}
	copy := created
	return &copy, nil
}

// ToggleSubtask toggles the done status of a subtask
func (tm *TaskManager) ToggleSubtask(taskID, subtaskID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == taskID {
				for j := range tm.store.Tasks[i].Subtasks {
					if tm.store.Tasks[i].Subtasks[j].ID == subtaskID {
						previous := cloneTask(tm.store.Tasks[i])
						previousMeta := tm.store.Meta
						tm.store.Tasks[i].Subtasks[j].Done = !tm.store.Tasks[i].Subtasks[j].Done
						if tm.store.Tasks[i].Subtasks[j].Done {
							tm.store.Tasks[i].Subtasks[j].Status = TaskStatusDone
						} else {
							tm.store.Tasks[i].Subtasks[j].Status = TaskStatusBacklog
						}
						tm.store.Tasks[i].UpdatedAt = time.Now()
						if err := tm.saveLocked(); err != nil {
							tm.store.Tasks[i] = previous
							tm.store.Meta = previousMeta
							return err
						}
						return nil
					}
				}
				return fmt.Errorf("subtask not found: %s", subtaskID)
			}
		}
		return fmt.Errorf("task not found: %s", taskID)
	})
}

// DeleteSubtask removes a subtask from a task
func (tm *TaskManager) DeleteSubtask(taskID, subtaskID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID == taskID {
				for j := range tm.store.Tasks[i].Subtasks {
					if tm.store.Tasks[i].Subtasks[j].ID == subtaskID {
						previous := cloneTask(tm.store.Tasks[i])
						previousMeta := tm.store.Meta
						tm.store.Tasks[i].Subtasks = append(
							tm.store.Tasks[i].Subtasks[:j],
							tm.store.Tasks[i].Subtasks[j+1:]...,
						)
						tm.store.Tasks[i].UpdatedAt = time.Now()
						if err := tm.saveLocked(); err != nil {
							tm.store.Tasks[i] = previous
							tm.store.Meta = previousMeta
							return err
						}
						return nil
					}
				}
				return fmt.Errorf("subtask not found: %s", subtaskID)
			}
		}
		return fmt.Errorf("task not found: %s", taskID)
	})
}

// RestoreSubtask puts a deleted subtask back with its original identity and
// metadata, so Undo does not turn a completed item into a new pending one.
func (tm *TaskManager) RestoreSubtask(taskID string, subtask Subtask) error {
	if taskID == "" || subtask.ID == "" {
		return fmt.Errorf("restored subtask has no parent or ID")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.mutateLocked(func() error {
		for i := range tm.store.Tasks {
			if tm.store.Tasks[i].ID != taskID {
				continue
			}
			for _, existing := range tm.store.Tasks[i].Subtasks {
				if existing.ID == subtask.ID {
					return fmt.Errorf("subtask already exists: %s", subtask.ID)
				}
			}
			if subtask.Status == "" {
				if subtask.Done {
					subtask.Status = TaskStatusDone
				} else {
					subtask.Status = TaskStatusBacklog
				}
			}
			subtask.Done = subtask.Status == TaskStatusDone
			if subtask.CreatedAt.IsZero() {
				subtask.CreatedAt = time.Now()
			}
			previous := cloneTask(tm.store.Tasks[i])
			previousMeta := tm.store.Meta
			tm.store.Tasks[i].Subtasks = append(tm.store.Tasks[i].Subtasks, subtask)
			tm.store.Tasks[i].UpdatedAt = time.Now()
			if err := tm.saveLocked(); err != nil {
				tm.store.Tasks[i] = previous
				tm.store.Meta = previousMeta
				return err
			}
			return nil
		}
		return fmt.Errorf("task not found: %s", taskID)
	})
}

// GetNextTask returns the next recommended task to work on
// Based on: dependencies resolved, priority, creation date
func (tm *TaskManager) GetNextTask() *Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.store == nil || len(tm.store.Tasks) == 0 {
		return nil
	}

	// Get all completed task IDs
	completedIDs := make(map[string]bool)
	for _, task := range tm.store.Tasks {
		if task.Status == TaskStatusDone {
			completedIDs[task.ID] = true
		}
	}

	// Find tasks that are not done and have all dependencies resolved
	var candidates []Task
	for _, task := range tm.store.Tasks {
		if task.Status == TaskStatusDone || task.Status == TaskStatusDeferred {
			continue
		}

		// Check if all dependencies are completed
		allDepsResolved := true
		for _, depID := range task.Dependencies {
			if !completedIDs[depID] {
				allDepsResolved = false
				break
			}
		}

		if allDepsResolved {
			candidates = append(candidates, task)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by priority (critical > high > medium > low) then by creation date
	priorityOrder := map[TaskPriority]int{
		TaskPriorityCritical: 0,
		TaskPriorityHigh:     1,
		TaskPriorityMedium:   2,
		TaskPriorityLow:      3,
	}

	sort.Slice(candidates, func(i, j int) bool {
		pi := priorityOrder[candidates[i].Priority]
		pj := priorityOrder[candidates[j].Priority]
		if pi != pj {
			return pi < pj
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	copy := cloneTask(candidates[0])
	return &copy
}

func cloneTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i := range tasks {
		out[i] = cloneTask(tasks[i])
	}
	return out
}

func cloneTask(task Task) Task {
	task.Tags = append([]string(nil), task.Tags...)
	task.Subtasks = append([]Subtask(nil), task.Subtasks...)
	task.Dependencies = append([]string(nil), task.Dependencies...)
	if task.CompletedAt != nil {
		completed := *task.CompletedAt
		task.CompletedAt = &completed
	}
	if task.DueAt != nil {
		due := *task.DueAt
		task.DueAt = &due
	}
	return task
}

// FormatTaskForAgent formats a task as a prompt for an AI agent
func (tm *TaskManager) FormatTaskForAgent(taskID string) (string, error) {
	task, err := tm.GetTask(taskID)
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf("## Task: %s\n\n", task.Title)

	if task.Description != "" {
		prompt += fmt.Sprintf("%s\n\n", task.Description)
	}

	if len(task.Subtasks) > 0 {
		prompt += "### Subtasks:\n"
		for _, st := range task.Subtasks {
			status := "[ ]"
			if st.Done {
				status = "[x]"
			}
			prompt += fmt.Sprintf("- %s %s\n", status, st.Title)
		}
		prompt += "\n"
	}

	if len(task.Tags) > 0 {
		prompt += fmt.Sprintf("### Tags: %v\n", task.Tags)
	}

	prompt += fmt.Sprintf("### Priority: %s\n", task.Priority)

	return prompt, nil
}
