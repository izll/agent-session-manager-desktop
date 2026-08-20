package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreTaskPreservesTaskMasterIDsAndUnknownContextData(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	before := `{
  "master": {
    "tasks": [{"id":"dependent","title":"dependent","dependencies":["restored"]}],
    "metadata": {"keep":"yes"}
  },
  "unrelated": {"value": 42}
}`
	if err := os.WriteFile(path, []byte(before), 0644); err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "restored", Title: "original", Description: "description", Details: "details",
		Status: "in-progress", Priority: "high", Tags: []string{"tag"},
		Dependencies: []string{"prerequisite"}, CreatedAt: "2026-08-20T12:00:00Z",
		UpdatedAt: "2026-08-20T12:30:00Z", CompletedAt: "2026-08-20T13:00:00Z",
		DueAt: "2026-08-21T12:00:00Z", SessionID: "session-one",
		Subtasks: []Subtask{{ID: "restored.1", Title: "sub", Description: "sub description", Status: "done", Details: "sub details", CreatedAt: "2026-08-20T12:05:00Z"}},
	}
	if err := NewTaskMaster(root).RestoreTask(task); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Master struct {
			Tasks    []Task                 `json:"tasks"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"master"`
		Unrelated map[string]interface{} `json:"unrelated"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("restore left invalid tasks.json: %v", err)
	}
	if len(document.Master.Tasks) != 2 {
		t.Fatalf("tasks after restore = %d, want 2", len(document.Master.Tasks))
	}
	got := document.Master.Tasks[1]
	if got.ID != task.ID || got.UpdatedAt != task.UpdatedAt || got.CompletedAt != task.CompletedAt || got.DueAt != task.DueAt || got.SessionID != task.SessionID || len(got.Subtasks) != 1 || got.Subtasks[0].ID != task.Subtasks[0].ID || got.Subtasks[0].Description != task.Subtasks[0].Description || got.Subtasks[0].CreatedAt != task.Subtasks[0].CreatedAt || got.Dependencies[0] != "prerequisite" {
		t.Fatalf("restored Task Master snapshot changed: %+v", got)
	}
	if document.Master.Metadata["keep"] != "yes" || document.Unrelated["value"] != float64(42) {
		t.Fatalf("restore dropped unrelated data: %+v %+v", document.Master.Metadata, document.Unrelated)
	}
	if err := NewTaskMaster(root).RestoreTask(task); err == nil {
		t.Fatal("restoring the same Task Master ID twice should fail")
	}
}

func TestRestoreTaskKeepsNumericTaskMasterIDTypes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[{"id":9,"title":"existing","subtasks":[]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "3", Title: "restored", Dependencies: []string{"9"},
		Subtasks: []Subtask{{ID: "1", Title: "sub", Status: "done"}},
	}
	if err := NewTaskMaster(root).RestoreTask(task); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	tasks := document["master"].(map[string]interface{})["tasks"].([]interface{})
	restored := tasks[1].(map[string]interface{})
	if _, ok := restored["id"].(json.Number); !ok {
		t.Fatalf("restored task id changed JSON type: %T", restored["id"])
	}
	if _, ok := restored["dependencies"].([]interface{})[0].(json.Number); !ok {
		t.Fatalf("restored dependency changed JSON type: %T", restored["dependencies"].([]interface{})[0])
	}
	subtask := restored["subtasks"].([]interface{})[0].(map[string]interface{})
	if _, ok := subtask["id"].(json.Number); !ok {
		t.Fatalf("restored subtask id changed JSON type: %T", subtask["id"])
	}
}

func TestRestoreSubtaskPreservesSnapshotAndIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[{"id":3,"title":"parent","subtasks":[{"id":1,"title":"kept"}]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	subtask := Subtask{ID: "2", Title: "restored", Description: "description", Details: "details", Status: "done", CreatedAt: "2026-08-20T12:00:00Z"}
	manager := NewTaskMaster(root)
	if err := manager.RestoreSubtask("3", subtask); err != nil {
		t.Fatal(err)
	}
	if err := manager.RestoreSubtask("3", subtask); err == nil {
		t.Fatal("restoring the same subtask twice should fail")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document map[string]interface{}
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	tasks := document["master"].(map[string]interface{})["tasks"].([]interface{})
	restored := tasks[0].(map[string]interface{})["subtasks"].([]interface{})[1].(map[string]interface{})
	if id, ok := restored["id"].(json.Number); !ok || id.String() != "2" || restored["status"] != "done" || restored["details"] != "details" || restored["createdAt"] != subtask.CreatedAt {
		t.Fatalf("restored subtask changed: %#v", restored)
	}
}

func TestTaskFromMapKeepsOptionalSnapshotFields(t *testing.T) {
	task := taskFromMap(map[string]interface{}{
		"id": 3.0, "title": "task", "updatedAt": "updated", "completedAt": "completed",
		"dueAt": "due", "sessionId": "session", "subtasks": []interface{}{
			map[string]interface{}{"id": 1.0, "title": "sub", "createdAt": "created"},
		},
	})
	if task.UpdatedAt != "updated" || task.CompletedAt != "completed" || task.DueAt != "due" || task.SessionID != "session" || len(task.Subtasks) != 1 || task.Subtasks[0].CreatedAt != "created" {
		t.Fatalf("optional fields were dropped: %+v", task)
	}
}
