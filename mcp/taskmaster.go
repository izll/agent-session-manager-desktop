package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
)

// TaskMaster provides high-level Task Master MCP operations
type TaskMaster struct {
	client      *Client
	projectRoot string
	mu          sync.Mutex
}

// Task represents a Task Master task
type Task struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Status       string     `json:"status"`
	Priority     string     `json:"priority"`
	Tags         []string   `json:"tags"`
	Subtasks     []Subtask  `json:"subtasks"`
	Dependencies []string   `json:"dependencies"`
	Complexity   *int       `json:"complexity,omitempty"`
	Details      string     `json:"details,omitempty"`
}

// Subtask represents a subtask within a task
type Subtask struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Details     string `json:"details,omitempty"`
}

// TasksResponse represents the response from get_tasks
type TasksResponse struct {
	Tasks []Task `json:"tasks"`
	Total int    `json:"total"`
}

// TaskMasterDataResponse wraps the Task Master MCP response format
type TaskMasterDataResponse struct {
	Data struct {
		Tasks []Task `json:"tasks"`
		Stats struct {
			Total int `json:"total"`
		} `json:"stats"`
	} `json:"data"`
}

// ComplexityReport represents complexity analysis results
type ComplexityReport struct {
	Tasks []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Complexity int    `json:"complexity"`
		Reasoning  string `json:"reasoning"`
	} `json:"tasks"`
	Summary string `json:"summary"`
}

// NewTaskMaster creates a new Task Master instance
// Uses Claude Code provider by default - no API key required
func NewTaskMaster(projectRoot string) *TaskMaster {
	return &TaskMaster{
		client:      NewClient(projectRoot),
		projectRoot: projectRoot,
	}
}

// Start starts the Task Master MCP server
func (tm *TaskMaster) Start() error {
	return tm.client.Start()
}

// Stop stops the Task Master MCP server
func (tm *TaskMaster) Stop() error {
	return tm.client.Stop()
}

// IsRunning returns true if the server is running
func (tm *TaskMaster) IsRunning() bool {
	return tm.client.IsRunning()
}

// GetTools returns the available MCP tools
func (tm *TaskMaster) GetTools() []Tool {
	return tm.client.GetTools()
}

// InitializeProject initializes Task Master in the project
func (tm *TaskMaster) InitializeProject(skipInstall bool) error {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}
	if skipInstall {
		args["skipInstall"] = true
	}

	result, err := tm.client.CallTool("initialize_project", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("initialize failed: %s", GetToolResultText(result))
	}

	return nil
}

// ParsePRD parses a PRD file into tasks
func (tm *TaskMaster) ParsePRD(prdPath string, numTasks int, force bool) error {
	args := map[string]interface{}{
		"input":       prdPath,
		"projectRoot": tm.projectRoot,
	}
	if numTasks > 0 {
		args["numTasks"] = numTasks
	}
	if force {
		args["force"] = true
	}

	result, err := tm.client.CallTool("parse_prd", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("parse_prd failed: %s", GetToolResultText(result))
	}

	return nil
}

// GetTasks returns all tasks
func (tm *TaskMaster) GetTasks(status string, withSubtasks bool) (*TasksResponse, error) {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}
	if status != "" {
		args["status"] = status
	}
	if withSubtasks {
		args["withSubtasks"] = true
	}

	result, err := tm.client.CallTool("get_tasks", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	var response TasksResponse

	// Try parsing as new format: {"data": {"tasks": [...], "stats": {...}}}
	var dataResponse TaskMasterDataResponse
	if err := json.Unmarshal([]byte(text), &dataResponse); err == nil && len(dataResponse.Data.Tasks) > 0 {
		response.Tasks = dataResponse.Data.Tasks
		response.Total = dataResponse.Data.Stats.Total
		if response.Total == 0 {
			response.Total = len(response.Tasks)
		}
		return &response, nil
	}

	// Try parsing as old format: {"tasks": [...], "total": ...}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		// Try parsing as array directly
		var tasks []Task
		if err2 := json.Unmarshal([]byte(text), &tasks); err2 != nil {
			return nil, fmt.Errorf("failed to parse tasks response: %w", err)
		}
		response.Tasks = tasks
		response.Total = len(tasks)
	}

	return &response, nil
}

// GetTask returns a specific task by ID
func (tm *TaskMaster) GetTask(taskID string) (*Task, error) {
	args := map[string]interface{}{
		"id":          taskID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("get_task", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)

	// Try parsing as new format: {"data": {"task": {...}}}
	var dataResponse struct {
		Data struct {
			Task *Task `json:"task"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &dataResponse); err == nil && dataResponse.Data.Task != nil {
		return dataResponse.Data.Task, nil
	}

	// Try parsing as direct Task object
	var task Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// NextTask returns the next task to work on
func (tm *TaskMaster) NextTask() (*Task, error) {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("next_task", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	if text == "" || text == "null" {
		return nil, nil // No next task
	}

	// Try parsing as new format: {"data": {"nextTask": {...}}} or {"data": {"task": {...}}}
	var dataResponse struct {
		Data struct {
			NextTask *Task  `json:"nextTask"`
			Task     *Task  `json:"task"`
			Message  string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &dataResponse); err == nil {
		if dataResponse.Data.NextTask != nil {
			return dataResponse.Data.NextTask, nil
		}
		if dataResponse.Data.Task != nil {
			return dataResponse.Data.Task, nil
		}
		// If no task but has message, return nil (no next task available)
		if dataResponse.Data.Message != "" {
			return nil, nil
		}
	}

	// Try parsing as direct Task object
	var task Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		// Check if it's an error message (starts with text, not JSON)
		if len(text) > 0 && text[0] != '{' && text[0] != '[' {
			return nil, nil // Likely an error message like "No pending tasks"
		}
		return nil, fmt.Errorf("failed to parse next task: %w", err)
	}

	return &task, nil
}

// SetTaskStatus updates the status of a task
func (tm *TaskMaster) SetTaskStatus(taskID, status string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"status":      status,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("set_task_status", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("set_task_status failed: %s", GetToolResultText(result))
	}

	return nil
}

// AddTask adds a new task
func (tm *TaskMaster) AddTask(prompt string, research bool, priority string, dependencies []string) (*Task, error) {
	args := map[string]interface{}{
		"prompt":      prompt,
		"projectRoot": tm.projectRoot,
	}
	if research {
		args["research"] = true
	}
	if priority != "" {
		args["priority"] = priority
	}
	if len(dependencies) > 0 {
		args["dependencies"] = dependencies
	}

	result, err := tm.client.CallTool("add_task", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	var task Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		// Task might just return success message
		return nil, nil
	}

	return &task, nil
}

// AddManualTask adds a new task without using AI (manual mode)
func (tm *TaskMaster) AddManualTask(title, description, details, priority string, dependencies []string) (*Task, error) {
	// Task Master requires both title AND description for manual task creation
	if description == "" {
		description = title // Use title as description if not provided
	}
	args := map[string]interface{}{
		"title":       title,
		"description": description,
		"projectRoot": tm.projectRoot,
	}
	if details != "" {
		args["details"] = details
	}
	if priority != "" {
		args["priority"] = priority
	}
	if len(dependencies) > 0 {
		args["dependencies"] = dependencies
	}

	result, err := tm.client.CallTool("add_task", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	var task Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		// Task might just return success message
		return nil, nil
	}

	return &task, nil
}

// UpdateTask updates an existing task
func (tm *TaskMaster) UpdateTask(taskID, prompt string, research bool) error {
	args := map[string]interface{}{
		"id":          taskID,
		"prompt":      prompt,
		"projectRoot": tm.projectRoot,
	}
	if research {
		args["research"] = true
	}

	result, err := tm.client.CallTool("update_task", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("update_task failed: %s", GetToolResultText(result))
	}

	return nil
}

// UpdateSubtask updates a subtask with implementation notes
func (tm *TaskMaster) UpdateSubtask(taskID, prompt string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"prompt":      prompt,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("update_subtask", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("update_subtask failed: %s", GetToolResultText(result))
	}

	return nil
}

// ExpandTask expands a task into subtasks
func (tm *TaskMaster) ExpandTask(taskID string, research, force bool, numSubtasks int) error {
	args := map[string]interface{}{
		"id":          taskID,
		"projectRoot": tm.projectRoot,
	}
	if research {
		args["research"] = true
	}
	if force {
		args["force"] = true
	}
	if numSubtasks > 0 {
		args["num"] = numSubtasks
	}

	result, err := tm.client.CallTool("expand_task", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("expand_task failed: %s", GetToolResultText(result))
	}

	return nil
}

// ExpandAllTasks expands all eligible tasks
func (tm *TaskMaster) ExpandAllTasks(research, force bool) error {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}
	if research {
		args["research"] = true
	}
	if force {
		args["force"] = true
	}

	result, err := tm.client.CallTool("expand_all", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("expand_all failed: %s", GetToolResultText(result))
	}

	return nil
}

// AnalyzeComplexity analyzes task complexity
func (tm *TaskMaster) AnalyzeComplexity(research bool) (*ComplexityReport, error) {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}
	if research {
		args["research"] = true
	}

	result, err := tm.client.CallTool("analyze_project_complexity", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	var report ComplexityReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		// Return just the text as summary
		return &ComplexityReport{Summary: text}, nil
	}

	return &report, nil
}

// GetComplexityReport returns the complexity report
func (tm *TaskMaster) GetComplexityReport() (*ComplexityReport, error) {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("complexity_report", args)
	if err != nil {
		return nil, err
	}

	text := GetToolResultText(result)
	var report ComplexityReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		return &ComplexityReport{Summary: text}, nil
	}

	return &report, nil
}

// AddDependency adds a dependency to a task
func (tm *TaskMaster) AddDependency(taskID, dependsOnID string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"dependsOn":   dependsOnID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("add_dependency", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("add_dependency failed: %s", GetToolResultText(result))
	}

	return nil
}

// RemoveDependency removes a dependency from a task
func (tm *TaskMaster) RemoveDependency(taskID, dependsOnID string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"dependsOn":   dependsOnID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("remove_dependency", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("remove_dependency failed: %s", GetToolResultText(result))
	}

	return nil
}

// ValidateDependencies validates all task dependencies
func (tm *TaskMaster) ValidateDependencies() (string, error) {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("validate_dependencies", args)
	if err != nil {
		return "", err
	}

	return GetToolResultText(result), nil
}

// RemoveTask removes a task
func (tm *TaskMaster) RemoveTask(taskID string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("remove_task", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("remove_task failed: %s", GetToolResultText(result))
	}

	return nil
}

// Generate regenerates task markdown files
func (tm *TaskMaster) Generate() error {
	args := map[string]interface{}{
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("generate", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("generate failed: %s", GetToolResultText(result))
	}

	return nil
}

// AddSubtask adds a new subtask to a task
func (tm *TaskMaster) AddSubtask(taskID, title, description string) (*Subtask, error) {
	args := map[string]interface{}{
		"id":          taskID,
		"title":       title,
		"projectRoot": tm.projectRoot,
	}
	if description != "" {
		args["description"] = description
	}

	result, err := tm.client.CallTool("add_subtask", args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("add_subtask failed: %s", GetToolResultText(result))
	}

	text := GetToolResultText(result)
	var subtask Subtask
	if err := json.Unmarshal([]byte(text), &subtask); err != nil {
		return nil, nil // Success but no subtask returned
	}

	return &subtask, nil
}

// RemoveSubtask removes a specific subtask
func (tm *TaskMaster) RemoveSubtask(subtaskID string) error {
	args := map[string]interface{}{
		"id":          subtaskID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("remove_subtask", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("remove_subtask failed: %s", GetToolResultText(result))
	}

	return nil
}

// ClearSubtasks removes all subtasks from a task
func (tm *TaskMaster) ClearSubtasks(taskID string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("clear_subtasks", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("clear_subtasks failed: %s", GetToolResultText(result))
	}

	return nil
}

// SetSubtaskStatus sets the status of a subtask
func (tm *TaskMaster) SetSubtaskStatus(subtaskID, status string) error {
	args := map[string]interface{}{
		"id":          subtaskID,
		"status":      status,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallTool("set_task_status", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("set_subtask_status failed: %s", GetToolResultText(result))
	}

	return nil
}

// UpdateTaskDirect updates a task with direct field values (no AI)
func (tm *TaskMaster) UpdateTaskDirect(taskID, title, description, details, priority string) error {
	args := map[string]interface{}{
		"id":          taskID,
		"projectRoot": tm.projectRoot,
	}
	if title != "" {
		args["title"] = title
	}
	if description != "" {
		args["description"] = description
	}
	if details != "" {
		args["details"] = details
	}
	if priority != "" {
		args["priority"] = priority
	}

	result, err := tm.client.CallTool("update", args)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("update failed: %s", GetToolResultText(result))
	}

	return nil
}

// FormatTaskForPrompt formats a task as a prompt for an AI agent
func FormatTaskForPrompt(task *Task) string {
	prompt := fmt.Sprintf("## Task %s: %s\n\n", task.ID, task.Title)

	if task.Description != "" {
		prompt += fmt.Sprintf("%s\n\n", task.Description)
	}

	if task.Details != "" {
		prompt += fmt.Sprintf("### Details:\n%s\n\n", task.Details)
	}

	if len(task.Subtasks) > 0 {
		prompt += "### Subtasks:\n"
		for _, st := range task.Subtasks {
			status := "[ ]"
			if st.Status == "done" {
				status = "[x]"
			}
			prompt += fmt.Sprintf("- %s %s: %s\n", status, st.ID, st.Title)
			if st.Description != "" {
				prompt += fmt.Sprintf("  %s\n", st.Description)
			}
		}
		prompt += "\n"
	}

	if len(task.Dependencies) > 0 {
		prompt += fmt.Sprintf("### Dependencies: %v\n", task.Dependencies)
	}

	if len(task.Tags) > 0 {
		prompt += fmt.Sprintf("### Tags: %v\n", task.Tags)
	}

	prompt += fmt.Sprintf("### Priority: %s\n", task.Priority)
	prompt += fmt.Sprintf("### Status: %s\n", task.Status)

	return prompt
}
