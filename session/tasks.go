package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Status       TaskStatus   `json:"status"`
	Priority     TaskPriority `json:"priority"`
	Tags         []string     `json:"tags"`
	Subtasks     []Subtask    `json:"subtasks"`
	Dependencies []string     `json:"dependencies"` // Task IDs
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	CompletedAt  *time.Time   `json:"completedAt,omitempty"`
}

// Subtask represents a subtask within a task
type Subtask struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"createdAt"`
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
	projectPath string
	store       *TaskStore
}

// NewTaskManager creates a new task manager for a project path
func NewTaskManager(projectPath string) *TaskManager {
	return &TaskManager{
		projectPath: projectPath,
	}
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
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tasks file: %w", err)
	}

	return nil
}

// GetTasks returns all tasks
func (tm *TaskManager) GetTasks() []Task {
	if tm.store == nil {
		return []Task{}
	}
	return tm.store.Tasks
}

// GetTasksByStatus returns tasks filtered by status
func (tm *TaskManager) GetTasksByStatus(status TaskStatus) []Task {
	if tm.store == nil {
		return []Task{}
	}

	var filtered []Task
	for _, task := range tm.store.Tasks {
		if task.Status == status {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

// GetTask returns a task by ID
func (tm *TaskManager) GetTask(id string) (*Task, error) {
	if tm.store == nil {
		return nil, fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == id {
			return &tm.store.Tasks[i], nil
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
	if tm.store == nil {
		if err := tm.Load(); err != nil {
			return nil, err
		}
	}

	task := Task{
		ID:           tm.generateTaskID(),
		Title:        title,
		Description:  description,
		Status:       TaskStatusBacklog,
		Priority:     priority,
		Tags:         tags,
		Subtasks:     []Subtask{},
		Dependencies: []string{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	tm.store.Tasks = append(tm.store.Tasks, task)

	if err := tm.Save(); err != nil {
		return nil, err
	}

	return &task, nil
}

// UpdateTask updates an existing task
func (tm *TaskManager) UpdateTask(id string, updates map[string]interface{}) error {
	if tm.store == nil {
		return fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == id {
			task := &tm.store.Tasks[i]

			if title, ok := updates["title"].(string); ok {
				task.Title = title
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
			if tags, ok := updates["tags"].([]string); ok {
				task.Tags = tags
			}

			task.UpdatedAt = time.Now()
			return tm.Save()
		}
	}

	return fmt.Errorf("task not found: %s", id)
}

// DeleteTask deletes a task by ID
func (tm *TaskManager) DeleteTask(id string) error {
	if tm.store == nil {
		return fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == id {
			tm.store.Tasks = append(tm.store.Tasks[:i], tm.store.Tasks[i+1:]...)
			return tm.Save()
		}
	}

	return fmt.Errorf("task not found: %s", id)
}

// MoveTask changes the status of a task
func (tm *TaskManager) MoveTask(id string, newStatus TaskStatus) error {
	return tm.UpdateTask(id, map[string]interface{}{
		"status": string(newStatus),
	})
}

// AddSubtask adds a subtask to a task
func (tm *TaskManager) AddSubtask(taskID, title string) (*Subtask, error) {
	if tm.store == nil {
		return nil, fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == taskID {
			subtask := Subtask{
				ID:        fmt.Sprintf("subtask_%d", time.Now().UnixNano()),
				Title:     title,
				Done:      false,
				CreatedAt: time.Now(),
			}
			tm.store.Tasks[i].Subtasks = append(tm.store.Tasks[i].Subtasks, subtask)
			tm.store.Tasks[i].UpdatedAt = time.Now()

			if err := tm.Save(); err != nil {
				return nil, err
			}
			return &subtask, nil
		}
	}

	return nil, fmt.Errorf("task not found: %s", taskID)
}

// ToggleSubtask toggles the done status of a subtask
func (tm *TaskManager) ToggleSubtask(taskID, subtaskID string) error {
	if tm.store == nil {
		return fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == taskID {
			for j := range tm.store.Tasks[i].Subtasks {
				if tm.store.Tasks[i].Subtasks[j].ID == subtaskID {
					tm.store.Tasks[i].Subtasks[j].Done = !tm.store.Tasks[i].Subtasks[j].Done
					tm.store.Tasks[i].UpdatedAt = time.Now()
					return tm.Save()
				}
			}
			return fmt.Errorf("subtask not found: %s", subtaskID)
		}
	}

	return fmt.Errorf("task not found: %s", taskID)
}

// DeleteSubtask removes a subtask from a task
func (tm *TaskManager) DeleteSubtask(taskID, subtaskID string) error {
	if tm.store == nil {
		return fmt.Errorf("no task store loaded")
	}

	for i := range tm.store.Tasks {
		if tm.store.Tasks[i].ID == taskID {
			for j := range tm.store.Tasks[i].Subtasks {
				if tm.store.Tasks[i].Subtasks[j].ID == subtaskID {
					tm.store.Tasks[i].Subtasks = append(
						tm.store.Tasks[i].Subtasks[:j],
						tm.store.Tasks[i].Subtasks[j+1:]...,
					)
					tm.store.Tasks[i].UpdatedAt = time.Now()
					return tm.Save()
				}
			}
			return fmt.Errorf("subtask not found: %s", subtaskID)
		}
	}

	return fmt.Errorf("task not found: %s", taskID)
}

// GetNextTask returns the next recommended task to work on
// Based on: dependencies resolved, priority, creation date
func (tm *TaskManager) GetNextTask() *Task {
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

	return &candidates[0]
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
