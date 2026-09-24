package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Assigning a tab ties the task to the session the tab belongs to; an update
// that assigns no tab is passed on as it came.
func TestAssigningATabImpliesTheSession(t *testing.T) {
	got := tabAssignmentImpliesSession("s1", map[string]interface{}{"tabId": "codex"})
	if got["sessionId"] != "s1" || got["tabId"] != "codex" {
		t.Errorf("assigning a tab: %#v", got)
	}

	for _, updates := range []map[string]interface{}{
		{"title": "x"},
		{"tabId": ""},
		{"sessionId": "", "tabId": ""},
	} {
		if got := tabAssignmentImpliesSession("s1", updates); !reflect.DeepEqual(got, updates) {
			t.Errorf("%#v became %#v", updates, got)
		}
	}
}

// The task store is shared by every session on one directory. A tab belongs
// to one of them — and every session has a "main" — so a task assigned in
// another session is sent from this one as if unassigned.
func TestATaskIsSentToItsTabOnlyFromItsOwnSession(t *testing.T) {
	if got := assignedTabIn("s1", "s1", "main"); got != "main" {
		t.Errorf("own session: %q", got)
	}
	if got := assignedTabIn("s2", "s1", "main"); got != "" {
		t.Errorf("another session's main tab was targeted: %q", got)
	}
	if got := assignedTabIn("s1", "", "codex"); got != "" {
		t.Errorf("a project-wide task was targeted at %q", got)
	}
}

// The Task Master edit carries the tab the same way it carries the session:
// written when chosen, removed when cleared.
func TestTaskMasterDirectEditCarriesTheTab(t *testing.T) {
	tasksFile := filepath.Join(t.TempDir(), "tasks.json")
	original := map[string]interface{}{
		"master": map[string]interface{}{
			"tasks": []interface{}{map[string]interface{}{"id": 7, "title": "old"}},
		},
	}
	data, _ := json.Marshal(original)
	if err := os.WriteFile(tasksFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	readTask := func() map[string]interface{} {
		t.Helper()
		out, err := os.ReadFile(tasksFile)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]struct {
			Tasks []map[string]interface{} `json:"tasks"`
		}
		if err := json.Unmarshal(out, &root); err != nil {
			t.Fatal(err)
		}
		return root["master"].Tasks[0]
	}

	if err := updateTaskMasterFileDirect(tasksFile, "7", "t", "", "", "high", "", "s1", "codex"); err != nil {
		t.Fatal(err)
	}
	if task := readTask(); task["tabId"] != "codex" || task["sessionId"] != "s1" {
		t.Fatalf("the tab was not written: %#v", task)
	}

	if err := updateTaskMasterFileDirect(tasksFile, "7", "t", "", "", "high", "", "s1", ""); err != nil {
		t.Fatal(err)
	}
	if task := readTask(); task["tabId"] != nil {
		t.Errorf("a cleared tab remained: %#v", task)
	}
}
