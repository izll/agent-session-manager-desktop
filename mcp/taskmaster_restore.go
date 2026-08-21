package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

func decodeTaskMasterObject(raw []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded map[string]interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return decoded, nil
}

// RestoreTask writes a deleted Task Master task back with its original task
// and subtask IDs. Task Master's create tools always allocate a new ID, which
// would leave reverse dependencies pointing at a task that no longer exists.
func (tm *TaskMaster) RestoreTask(task Task) error {
	if task.ID == "" {
		return fmt.Errorf("restored task has no ID")
	}
	path := filepath.Join(tm.projectRoot, ".taskmaster", "tasks", "tasks.json")
	return MutateTaskMasterFile(path, func(root map[string]interface{}) error {
		context, err := taskMasterRestoreContext(root)
		if err != nil {
			return err
		}
		tasks, ok := context["tasks"].([]interface{})
		if !ok {
			return fmt.Errorf("tasks.json context has no tasks array")
		}
		for _, value := range tasks {
			stored, ok := value.(map[string]interface{})
			if ok && taskMasterID(stored["id"]) == task.ID {
				return fmt.Errorf("task already exists: %s", task.ID)
			}
		}
		restored, rawIdentity, err := taskMasterRestoreObject(task.RawJSON, task.ID, task)
		if err != nil {
			return fmt.Errorf("failed to prepare restored task: %w", err)
		}
		if !rawIdentity {
			coerceTaskMasterIDs(restored,
				taskMasterUsesNumericIDs(tasks, task.ID),
				taskMasterUsesNumericSubtaskIDs(tasks, firstSubtaskID(task.Subtasks)))
		}
		context["tasks"] = append(tasks, restored)
		return nil
	})
}

// RestoreSubtask appends a deleted subtask to its original parent without
// asking Task Master to allocate a replacement ID.
func (tm *TaskMaster) RestoreSubtask(taskID string, subtask Subtask) error {
	if taskID == "" || subtask.ID == "" {
		return fmt.Errorf("restored subtask has no parent or ID")
	}
	path := filepath.Join(tm.projectRoot, ".taskmaster", "tasks", "tasks.json")
	return MutateTaskMasterFile(path, func(root map[string]interface{}) error {
		context, err := taskMasterRestoreContext(root)
		if err != nil {
			return err
		}
		tasks, ok := context["tasks"].([]interface{})
		if !ok {
			return fmt.Errorf("tasks.json context has no tasks array")
		}
		for _, value := range tasks {
			stored, ok := value.(map[string]interface{})
			if !ok || taskMasterID(stored["id"]) != taskID {
				continue
			}
			subtasks, _ := stored["subtasks"].([]interface{})
			for _, rawSubtask := range subtasks {
				candidate, ok := rawSubtask.(map[string]interface{})
				if ok && taskMasterID(candidate["id"]) == subtask.ID {
					return fmt.Errorf("subtask already exists: %s", subtask.ID)
				}
			}
			restored, rawIdentity, err := taskMasterRestoreObject(subtask.RawJSON, subtask.ID, subtask)
			if err != nil {
				return fmt.Errorf("failed to prepare restored subtask: %w", err)
			}
			if !rawIdentity && taskMasterSubtasksUseNumericIDs(subtasks, subtask.ID) {
				if id, ok := numericJSONID(subtask.ID); ok {
					restored["id"] = id
				}
			}
			stored["subtasks"] = append(subtasks, restored)
			return nil
		}
		return fmt.Errorf("task not found: %s", taskID)
	})
}

func taskMasterUsesNumericIDs(tasks []interface{}, fallbackID string) bool {
	for _, value := range tasks {
		stored, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		switch stored["id"].(type) {
		case json.Number, float64:
			return true
		case string:
			return false
		}
	}
	_, ok := numericJSONID(fallbackID)
	return ok
}

func numericJSONID(id string) (json.Number, bool) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return "", false
	}
	return json.Number(id), true
}

func coerceTaskMasterIDs(task map[string]interface{}, numericTasks, numericSubtasks bool) {
	if numericTasks {
		if id, ok := task["id"].(string); ok {
			if number, valid := numericJSONID(id); valid {
				task["id"] = number
			}
		}
		if dependencies, ok := task["dependencies"].([]interface{}); ok {
			for i, raw := range dependencies {
				if id, ok := raw.(string); ok {
					if number, valid := numericJSONID(id); valid {
						dependencies[i] = number
					}
				}
			}
		}
	}
	if !numericSubtasks {
		return
	}
	if subtasks, ok := task["subtasks"].([]interface{}); ok {
		for _, raw := range subtasks {
			stored, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := stored["id"].(string); ok {
				if number, valid := numericJSONID(id); valid {
					stored["id"] = number
				}
			}
		}
	}
}

func taskMasterUsesNumericSubtaskIDs(tasks []interface{}, fallbackID string) bool {
	for _, value := range tasks {
		stored, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		subtasks, _ := stored["subtasks"].([]interface{})
		if len(subtasks) != 0 {
			return taskMasterSubtasksUseNumericIDs(subtasks, fallbackID)
		}
	}
	_, ok := numericJSONID(fallbackID)
	return ok
}

func taskMasterSubtasksUseNumericIDs(subtasks []interface{}, fallbackID string) bool {
	for _, raw := range subtasks {
		stored, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch stored["id"].(type) {
		case json.Number, float64:
			return true
		case string:
			return false
		}
	}
	_, ok := numericJSONID(fallbackID)
	return ok
}

func firstSubtaskID(subtasks []Subtask) string {
	if len(subtasks) == 0 {
		return ""
	}
	return subtasks[0].ID
}

func taskMasterRestoreObject(rawJSON, expectedID string, known interface{}) (map[string]interface{}, bool, error) {
	if rawJSON != "" {
		restored, err := decodeTaskMasterObject([]byte(rawJSON))
		if err != nil {
			return nil, false, fmt.Errorf("invalid raw provider snapshot: %w", err)
		}
		if taskMasterID(restored["id"]) != expectedID {
			return nil, false, fmt.Errorf("raw provider snapshot ID does not match %s", expectedID)
		}
		return restored, true, nil
	}
	encoded, err := json.Marshal(known)
	if err != nil {
		return nil, false, err
	}
	restored, err := decodeTaskMasterObject(encoded)
	if err != nil {
		return nil, false, err
	}
	return restored, false, nil
}

func taskMasterRestoreContext(root map[string]interface{}) (map[string]interface{}, error) {
	if master, ok := root["master"].(map[string]interface{}); ok {
		if _, ok := master["tasks"].([]interface{}); ok {
			return master, nil
		}
	}
	var found map[string]interface{}
	for _, value := range root {
		context, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := context["tasks"].([]interface{}); !ok {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("tasks.json has multiple contexts but no master context")
		}
		found = context
	}
	if found == nil {
		return nil, fmt.Errorf("tasks.json has no task context")
	}
	return found, nil
}

func taskMasterID(value interface{}) string {
	switch id := value.(type) {
	case string:
		return id
	case json.Number:
		return id.String()
	default:
		return fmt.Sprint(id)
	}
}

// AtomicReplaceTaskMasterFile durably replaces Task Master's canonical JSON.
// Direct editing APIs share this with Undo so none of them truncate the live
// file before a complete replacement is ready.
func AtomicReplaceTaskMasterFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tasks-restore-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	mode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
