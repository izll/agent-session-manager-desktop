package session

import "testing"

// A task can be given to one tab of its session and taken back off it, and an
// edit that does not mention the tab leaves the assignment alone.
func TestTaskTabAssignment(t *testing.T) {
	manager := NewTaskManager(t.TempDir())
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	task, err := manager.CreateTaskForSession("review", "", TaskPriorityMedium, nil, "s1")
	if err != nil {
		t.Fatal(err)
	}
	tabOf := func() string {
		t.Helper()
		got, err := manager.GetTask(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.TabID
	}

	if err := manager.UpdateTask(task.ID, map[string]interface{}{"tabId": "codex"}); err != nil {
		t.Fatal(err)
	}
	if got := tabOf(); got != "codex" {
		t.Fatalf("assigned tab = %q", got)
	}

	if err := manager.UpdateTask(task.ID, map[string]interface{}{"title": "renamed"}); err != nil {
		t.Fatal(err)
	}
	if got := tabOf(); got != "codex" {
		t.Errorf("an edit that did not touch the tab changed it to %q", got)
	}

	// Re-sending the same session is not a move.
	if err := manager.UpdateTask(task.ID, map[string]interface{}{"sessionId": "s1"}); err != nil {
		t.Fatal(err)
	}
	if got := tabOf(); got != "codex" {
		t.Errorf("confirming the session dropped the tab: %q", got)
	}

	if err := manager.UpdateTask(task.ID, map[string]interface{}{"tabId": ""}); err != nil {
		t.Fatal(err)
	}
	if got := tabOf(); got != "" {
		t.Errorf("clearing left the tab %q", got)
	}
}

// A tab ID means nothing outside its session, so moving the task to another
// session — or off every session — drops it, unless the same edit assigns a
// tab there.
func TestMovingATaskToAnotherSessionDropsItsTab(t *testing.T) {
	manager := NewTaskManager(t.TempDir())
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	task, err := manager.CreateTaskForSession("review", "", TaskPriorityMedium, nil, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateTask(task.ID, map[string]interface{}{"tabId": "codex"}); err != nil {
		t.Fatal(err)
	}

	if err := manager.UpdateTask(task.ID, map[string]interface{}{"sessionId": ""}); err != nil {
		t.Fatal(err)
	}
	got, _ := manager.GetTask(task.ID)
	if got.TabID != "" {
		t.Errorf("a project-wide task kept the tab %q", got.TabID)
	}

	if err := manager.UpdateTask(task.ID, map[string]interface{}{"sessionId": "s2", "tabId": MainTabID}); err != nil {
		t.Fatal(err)
	}
	got, _ = manager.GetTask(task.ID)
	if got.SessionID != "s2" || got.TabID != MainTabID {
		t.Errorf("moving with a new tab: session %q tab %q", got.SessionID, got.TabID)
	}
}
