package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// aiTimeout is the timeout for AI-based MCP tool calls (add_task, update_task, expand, etc.)
const aiTimeout = 5 * time.Minute

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
	CreatedAt    string     `json:"createdAt,omitempty"`
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

	result, err := tm.client.CallToolWithTimeout("parse_prd", args, aiTimeout)
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

	if result.IsError {
		return nil, fmt.Errorf("get_tasks failed: %s", GetToolResultText(result))
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

	if result.IsError {
		return nil, fmt.Errorf("get_task failed: %s", GetToolResultText(result))
	}

	text := GetToolResultText(result)

	// Parse into generic map first to handle numeric IDs and various formats
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse task response: %w", err)
	}

	// Log top-level keys for debugging
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	log.Printf("[TaskMaster MCP] GetTask response top-level keys: %v", keys)

	// Try to find the task in various response formats
	taskMap := findTaskMap(raw)
	if taskMap == nil {
		log.Printf("[TaskMaster MCP] Could not find task data in response. Raw (first 1000): %.1000s", text)
		return nil, fmt.Errorf("could not find task data in MCP response")
	}

	task := taskFromMap(taskMap)
	log.Printf("[TaskMaster MCP] Parsed task: ID=%s Title=%.50s Status=%s Priority=%s", task.ID, task.Title, task.Status, task.Priority)
	return task, nil
}

// findTaskMap tries various response formats to locate the task data map
func findTaskMap(raw map[string]interface{}) map[string]interface{} {
	// Format 1: {"data": {"task": {...}}}
	if data, ok := raw["data"].(map[string]interface{}); ok {
		if t, ok := data["task"].(map[string]interface{}); ok {
			log.Printf("[TaskMaster MCP] Found task in data.task format")
			return t
		}
	}

	// Format 2: {"task": {...}}
	if t, ok := raw["task"].(map[string]interface{}); ok {
		log.Printf("[TaskMaster MCP] Found task in task format")
		return t
	}

	// Format 3: {"result": {"task": {...}}}
	if result, ok := raw["result"].(map[string]interface{}); ok {
		if t, ok := result["task"].(map[string]interface{}); ok {
			log.Printf("[TaskMaster MCP] Found task in result.task format")
			return t
		}
	}

	// Format 4: direct task object (has "title" key at top level)
	if _, hasTitle := raw["title"]; hasTitle {
		log.Printf("[TaskMaster MCP] Found task at top level (has title)")
		return raw
	}

	// Format 5: look for any nested map that has "title"
	for k, v := range raw {
		if nested, ok := v.(map[string]interface{}); ok {
			if _, hasTitle := nested["title"]; hasTitle {
				log.Printf("[TaskMaster MCP] Found task nested under key %q", k)
				return nested
			}
		}
	}

	return nil
}

// taskFromMap extracts a Task from a generic map, handling numeric IDs etc.
func taskFromMap(m map[string]interface{}) *Task {
	task := &Task{}
	task.ID = mapString(m, "id")
	task.Title = mapString(m, "title")
	task.Description = mapString(m, "description")
	task.Status = mapString(m, "status")
	task.Priority = mapString(m, "priority")
	task.Details = mapString(m, "details")
	task.CreatedAt = mapString(m, "createdAt")

	if tags, ok := m["tags"].([]interface{}); ok {
		for _, t := range tags {
			if s, ok := t.(string); ok {
				task.Tags = append(task.Tags, s)
			}
		}
	}

	if deps, ok := m["dependencies"].([]interface{}); ok {
		for _, d := range deps {
			task.Dependencies = append(task.Dependencies, fmt.Sprintf("%v", d))
		}
	}

	if complexity, ok := m["complexity"].(float64); ok {
		c := int(complexity)
		task.Complexity = &c
	}

	if subtasks, ok := m["subtasks"].([]interface{}); ok {
		for _, st := range subtasks {
			if stMap, ok := st.(map[string]interface{}); ok {
				sub := Subtask{
					ID:          mapString(stMap, "id"),
					Title:       mapString(stMap, "title"),
					Description: mapString(stMap, "description"),
					Status:      mapString(stMap, "status"),
					Details:     mapString(stMap, "details"),
				}
				task.Subtasks = append(task.Subtasks, sub)
			}
		}
	}

	return task
}

// mapString extracts a string from a map, converting numbers to string if needed
func mapString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int(val)) {
			return fmt.Sprintf("%d", int(val))
		}
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
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

	if result.IsError {
		return nil, fmt.Errorf("next_task failed: %s", GetToolResultText(result))
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

	result, err := tm.client.CallToolWithTimeout("add_task", args, aiTimeout)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("add_task failed: %s", GetToolResultText(result))
	}

	text := GetToolResultText(result)

	// Try parsing as {"data": {"task": {...}}}
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
		return nil, fmt.Errorf("failed to parse add_task response: %w", err)
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

	result, err := tm.client.CallToolWithTimeout("add_task", args, aiTimeout)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("add_task failed: %s", GetToolResultText(result))
	}

	text := GetToolResultText(result)

	// Try parsing as {"data": {"task": {...}}}
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
		return nil, fmt.Errorf("failed to parse add_task response: %w", err)
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

	result, err := tm.client.CallToolWithTimeout("update_task", args, aiTimeout)
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

	result, err := tm.client.CallToolWithTimeout("update_subtask", args, aiTimeout)
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

	result, err := tm.client.CallToolWithTimeout("expand_task", args, aiTimeout)
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

	result, err := tm.client.CallToolWithTimeout("expand_all", args, aiTimeout)
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

	result, err := tm.client.CallToolWithTimeout("analyze_project_complexity", args, aiTimeout)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("analyze_project_complexity failed: %s", GetToolResultText(result))
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

	if result.IsError {
		return nil, fmt.Errorf("complexity_report failed: %s", GetToolResultText(result))
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

	if result.IsError {
		return "", fmt.Errorf("validate_dependencies failed: %s", GetToolResultText(result))
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
		return nil, fmt.Errorf("failed to parse add_subtask response: %w", err)
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

// UpdateTaskDirect updates a task with direct field values.
// Uses update_task with a structured prompt describing the changes.
func (tm *TaskMaster) UpdateTaskDirect(taskID, title, description, details, priority string) error {
	// Build a prompt describing the exact changes
	var parts []string
	if title != "" {
		parts = append(parts, fmt.Sprintf("Set the title to: %s", title))
	}
	if description != "" {
		parts = append(parts, fmt.Sprintf("Set the description to: %s", description))
	}
	if details != "" {
		parts = append(parts, fmt.Sprintf("Set the details to: %s", details))
	}
	if priority != "" {
		parts = append(parts, fmt.Sprintf("Set the priority to: %s", priority))
	}

	if len(parts) == 0 {
		return nil // Nothing to update
	}

	prompt := "Update this task with the following changes:\n"
	for _, p := range parts {
		prompt += "- " + p + "\n"
	}

	args := map[string]interface{}{
		"id":          taskID,
		"prompt":      prompt,
		"projectRoot": tm.projectRoot,
	}

	result, err := tm.client.CallToolWithTimeout("update_task", args, aiTimeout)
	if err != nil {
		return err
	}

	if result.IsError {
		return fmt.Errorf("update_task failed: %s", GetToolResultText(result))
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
