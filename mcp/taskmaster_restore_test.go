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

func TestRestoreTaskRejectsTrailingProviderSnapshotWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"master":{"tasks":[]},"keep":true}`)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "restored", RawJSON: `{"id":"restored","title":"first"} {"id":"other"}`}
	if err := NewTaskMaster(root).RestoreTask(task); err == nil {
		t.Fatal("restore accepted a second trailing provider object")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed restore changed Task Master storage")
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

func TestRestoreSubtaskUsesSubtaskIDSchemaNotParentTaskSchema(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Current Task Master data uses string task IDs and numeric subtask IDs.
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[{"id":"3","subtasks":[{"id":1,"title":"kept"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewTaskMaster(root).RestoreSubtask("3", Subtask{ID: "2", Title: "restored"}); err != nil {
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
	restored := tasks[0].(map[string]interface{})["subtasks"].([]interface{})[1].(map[string]interface{})
	if id, ok := restored["id"].(json.Number); !ok || id.String() != "2" {
		t.Fatalf("mixed-schema subtask ID changed type: %#v (%T)", restored["id"], restored["id"])
	}
}

func TestRestoreTaskRawSnapshotPreservesKnownAndUnknownProviderFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[{"id":"other","subtasks":[{"id":1}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rawSnapshot := `{"id":"restored","title":"original","testStrategy":"keep task strategy","futureField":{"nested":true},"subtasks":[{"id":2,"title":"sub","dependencies":[1],"parentId":"restored","testStrategy":"keep sub strategy","futureSub":"keep"}]}`
	task := Task{ID: "restored", Title: "original", RawJSON: rawSnapshot}
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
	if restored["testStrategy"] != "keep task strategy" || restored["futureField"].(map[string]interface{})["nested"] != true {
		t.Fatalf("raw task fields were lost: %#v", restored)
	}
	subtask := restored["subtasks"].([]interface{})[0].(map[string]interface{})
	if _, ok := subtask["id"].(json.Number); !ok || subtask["futureSub"] != "keep" || subtask["testStrategy"] != "keep sub strategy" || subtask["parentId"] != "restored" {
		t.Fatalf("raw subtask fields or ID type were lost: %#v", subtask)
	}
}

func TestTaskFromMapKeepsOptionalSnapshotFields(t *testing.T) {
	task := taskFromMap(map[string]interface{}{
		"id": 3.0, "title": "task", "updatedAt": "updated", "completedAt": "completed",
		"dueAt": "due", "sessionId": "session", "testStrategy": "task tests", "unknown": "raw", "subtasks": []interface{}{
			map[string]interface{}{"id": 1.0, "title": "sub", "createdAt": "created", "dependencies": []interface{}{2.0}, "parentId": "3", "testStrategy": "sub tests", "unknownSub": true},
		},
	})
	if task.UpdatedAt != "updated" || task.CompletedAt != "completed" || task.DueAt != "due" || task.SessionID != "session" || task.TestStrategy != "task tests" || task.RawJSON == "" || len(task.Subtasks) != 1 || task.Subtasks[0].CreatedAt != "created" || task.Subtasks[0].ParentID != "3" || task.Subtasks[0].TestStrategy != "sub tests" || len(task.Subtasks[0].Dependencies) != 1 || task.Subtasks[0].RawJSON == "" {
		t.Fatalf("optional fields were dropped: %+v", task)
	}
}

func TestTaskRawJSONPreservesLargeProviderNumbers(t *testing.T) {
	const source = `{"id":"large","title":"task","futureNumber":9007199254740993,"dependencies":[9007199254740993]}`
	var task Task
	if err := json.Unmarshal([]byte(source), &task); err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	decoder := json.NewDecoder(bytes.NewBufferString(task.RawJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if got := raw["futureNumber"].(json.Number).String(); got != "9007199254740993" {
		t.Fatalf("large unknown number = %s", got)
	}
	if len(task.Dependencies) != 1 || task.Dependencies[0] != "9007199254740993" {
		t.Fatalf("large dependency changed: %#v", task.Dependencies)
	}
}
