package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// A finished task is never late.
//
// The deadline stopped mattering when the work was done; showing a completed
// task as overdue for the rest of time makes the whole "late" marker useless.
func TestCompletedTasksAreNeverOverdue(t *testing.T) {
	past := time.Now().Add(-48 * time.Hour)
	now := time.Now()

	done := Task{Status: TaskStatusDone, DueAt: &past}
	if done.Overdue(now) {
		t.Error("a task finished after its deadline is not overdue, it is finished")
	}

	open := Task{Status: TaskStatusInProgress, DueAt: &past}
	if !open.Overdue(now) {
		t.Error("unfinished work past its deadline is overdue")
	}
}

func TestTaskMutationsRollBackWhenSaveFails(t *testing.T) {
	dir := t.TempDir()
	manager := NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	task, err := manager.CreateTask("before", "description", TaskPriorityMedium, []string{"tag"})
	if err != nil {
		t.Fatal(err)
	}
	subtask, err := manager.AddSubtask(task.ID, "subtask")
	if err != nil {
		t.Fatal(err)
	}

	// Turn the storage directory into a plain file. Every following mutation
	// reaches its in-memory change but must fail before replacing tasks.json.
	taskDir := filepath.Join(dir, ".taskmaster")
	if err := os.RemoveAll(taskDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}

	assertUnchanged := func(name string, mutate func() error) {
		t.Helper()
		before := manager.GetTasks()
		if err := mutate(); err == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
		after := manager.GetTasks()
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("%s leaked a failed mutation into memory\nbefore: %#v\nafter:  %#v", name, before, after)
		}
	}

	assertUnchanged("create", func() error {
		_, err := manager.CreateTask("failed", "", TaskPriorityLow, nil)
		return err
	})
	assertUnchanged("update", func() error {
		return manager.UpdateTask(task.ID, map[string]interface{}{"title": "changed"})
	})
	assertUnchanged("add subtask", func() error {
		_, err := manager.AddSubtask(task.ID, "failed")
		return err
	})
	assertUnchanged("toggle subtask", func() error {
		return manager.ToggleSubtask(task.ID, subtask.ID)
	})
	assertUnchanged("delete subtask", func() error {
		return manager.DeleteSubtask(task.ID, subtask.ID)
	})
	assertUnchanged("delete task", func() error {
		return manager.DeleteTask(task.ID)
	})
}

func TestTaskManagerSerializesConcurrentCreatesAndWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	manager := NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}

	const count = 40
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.CreateTask("task", "", TaskPriorityMedium, []string{"tag"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(manager.GetTasks()); got != count {
		t.Fatalf("stored %d tasks, want %d", got, count)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".taskmaster", "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var store TaskStore
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("concurrent save left invalid JSON: %v", err)
	}
	if len(store.Tasks) != count {
		t.Fatalf("disk has %d tasks, want %d", len(store.Tasks), count)
	}
}

func TestTaskManagerReturnsDeepCopies(t *testing.T) {
	due := time.Now().Add(time.Hour)
	manager := &TaskManager{store: &TaskStore{Tasks: []Task{{
		ID: "one", Tags: []string{"original"}, Subtasks: []Subtask{{ID: "sub", Title: "original"}},
		Dependencies: []string{"dep"}, DueAt: &due,
	}}}}

	tasks := manager.GetTasks()
	tasks[0].Tags[0] = "changed"
	tasks[0].Subtasks[0].Title = "changed"
	tasks[0].Dependencies[0] = "changed"
	*tasks[0].DueAt = time.Time{}
	got, err := manager.GetTask("one")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tags[0] != "original" || got.Subtasks[0].Title != "original" || got.Dependencies[0] != "dep" || got.DueAt.IsZero() {
		t.Fatalf("caller mutated manager-owned task through returned value: %+v", got)
	}
}

func TestRestoreTaskPreservesIdentityAndReverseDependencies(t *testing.T) {
	dir := t.TempDir()
	manager := NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	dependent := Task{
		ID: "dependent", Title: "dependent", Status: TaskStatusBacklog,
		Priority: TaskPriorityMedium, Dependencies: []string{"restored"}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	manager.store.Tasks = append(manager.store.Tasks, dependent)
	if err := manager.Save(); err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(24 * time.Hour).Round(0)
	restored := Task{
		ID: "restored", Title: "original", Description: "description", Details: "details",
		Status: TaskStatusInProgress, Priority: TaskPriorityHigh, Tags: []string{"tag"},
		Dependencies: []string{"prerequisite"}, Subtasks: []Subtask{{ID: "sub-original", Title: "sub", Description: "sub description", Details: "sub details", Status: TaskStatusDone, Done: true}},
		CreatedAt: time.Now().Add(-time.Hour).Round(0), UpdatedAt: time.Now().Round(0), DueAt: &due,
	}
	if err := manager.RestoreTask(restored); err != nil {
		t.Fatal(err)
	}

	reloaded := NewTaskManager(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.GetTask("restored")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != restored.ID || got.Details != restored.Details || len(got.Subtasks) != 1 || got.Subtasks[0].ID != "sub-original" || got.Subtasks[0].Description != "sub description" || got.Subtasks[0].Details != "sub details" || got.Subtasks[0].Status != TaskStatusDone || got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Fatalf("restored snapshot changed: %+v", got)
	}
	dep, err := reloaded.GetTask("dependent")
	if err != nil {
		t.Fatal(err)
	}
	if len(dep.Dependencies) != 1 || dep.Dependencies[0] != restored.ID {
		t.Fatalf("reverse dependency no longer resolves to restored ID: %+v", dep.Dependencies)
	}
	if err := reloaded.RestoreTask(restored); err == nil {
		t.Fatal("restoring the same ID twice should not overwrite the existing task")
	}
}

func TestSubtaskEditRoundTripPreservesDescriptionDetailsAndStatus(t *testing.T) {
	dir := t.TempDir()
	manager := NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	task, err := manager.CreateTask("task", "", TaskPriorityMedium, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateTask(task.ID, map[string]interface{}{
		"subtasks": []interface{}{map[string]interface{}{
			"id": "sub", "title": "subtask", "description": "description",
			"details": "details", "status": "in-progress", "createdAt": "2026-08-20T12:00:00Z",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	reloaded := NewTaskManager(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Subtasks) != 1 || got.Subtasks[0].Description != "description" || got.Subtasks[0].Details != "details" || got.Subtasks[0].Status != TaskStatusInProgress || got.Subtasks[0].Done {
		t.Fatalf("subtask fields did not round-trip: %+v", got.Subtasks)
	}
	if err := reloaded.ToggleSubtask(task.ID, "sub"); err != nil {
		t.Fatal(err)
	}
	toggled, err := reloaded.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !toggled.Subtasks[0].Done || toggled.Subtasks[0].Status != TaskStatusDone || toggled.Subtasks[0].Description != "description" || toggled.Subtasks[0].Details != "details" {
		t.Fatalf("toggle lost subtask fields or status: %+v", toggled.Subtasks[0])
	}
}

// No deadline means nothing to be late for.
func TestTasksWithoutADeadlineAreNeverOverdue(t *testing.T) {
	task := Task{Status: TaskStatusBacklog}
	if task.Overdue(time.Now()) {
		t.Error("a task with no deadline cannot be overdue")
	}
}

// Deferred counts as unfinished.
//
// It was put off, not dealt with — and closing a session that still has
// deferred work is exactly the case the warning exists for.
func TestDeferredCountsAsUnfinished(t *testing.T) {
	cases := map[TaskStatus]bool{
		TaskStatusBacklog:    true,
		TaskStatusInProgress: true,
		TaskStatusDeferred:   true,
		TaskStatusDone:       false,
	}

	for status, wantUnfinished := range cases {
		if got := (Task{Status: status}).Unfinished(); got != wantUnfinished {
			t.Errorf("status %q: Unfinished() = %v, want %v", status, got, wantUnfinished)
		}
	}
}

// Only tasks assigned to the session count towards its warning.
//
// Project-wide tasks are not one session's business. Counting them would fire
// the warning on every close in a project that has any tasks at all, which
// teaches people to click through it.
func TestOnlyTheSessionsOwnTasksTriggerTheWarning(t *testing.T) {
	manager := &TaskManager{
		projectPath: t.TempDir(),
		store: &TaskStore{Tasks: []Task{
			{ID: "1", SessionID: "session-a", Status: TaskStatusInProgress},
			{ID: "2", SessionID: "session-b", Status: TaskStatusBacklog},
			{ID: "3", SessionID: "", Status: TaskStatusBacklog}, // project-wide
			{ID: "4", SessionID: "session-a", Status: TaskStatusDone},
		}},
	}

	pending := manager.UnfinishedForSession("session-a")
	if len(pending) != 1 || pending[0].ID != "1" {
		t.Errorf("expected only session-a's unfinished task; got %v", pending)
	}

	// A session with nothing outstanding must close without a prompt.
	if got := manager.UnfinishedForSession("session-c"); len(got) != 0 {
		t.Errorf("a session with no tasks should have nothing pending; got %v", got)
	}

	// An empty session ID must not match the project-wide tasks, or every
	// caller passing "" would be told the project's whole backlog is theirs.
	if got := manager.UnfinishedForSession(""); len(got) != 0 {
		t.Errorf("an empty session ID must match nothing; got %v", got)
	}
}

// Editing a task must not silently drop its deadline.
//
// Updates arrive as a map, and every unrelated edit — a title change, a status
// move — goes through the same path. Keyed on presence rather than on type, so
// only a caller that actually sends "dueAt" can change it.
func TestUnrelatedEditsKeepTheDeadline(t *testing.T) {
	due := time.Date(2026, 8, 20, 14, 30, 0, 0, time.UTC)
	manager := &TaskManager{
		projectPath: t.TempDir(),
		store: &TaskStore{Tasks: []Task{
			{ID: "1", Title: "before", DueAt: &due, Status: TaskStatusBacklog},
		}},
	}
	if err := manager.Save(); err != nil {
		t.Fatal(err)
	}

	if err := manager.UpdateTask("1", map[string]interface{}{"title": "after"}); err != nil {
		t.Fatalf("updating the title: %v", err)
	}
	if manager.store.Tasks[0].DueAt == nil {
		t.Fatal("changing the title wiped the deadline")
	}
	if !manager.store.Tasks[0].DueAt.Equal(due) {
		t.Errorf("deadline changed to %v, want %v", manager.store.Tasks[0].DueAt, due)
	}

	// An explicit empty string is how the dialog takes a deadline back off.
	if err := manager.UpdateTask("1", map[string]interface{}{"dueAt": ""}); err != nil {
		t.Fatalf("clearing the deadline: %v", err)
	}
	if manager.store.Tasks[0].DueAt != nil {
		t.Error("an empty dueAt should clear the deadline")
	}

	// And setting one back has to survive the round trip.
	if err := manager.UpdateTask("1", map[string]interface{}{"dueAt": "2026-09-01T08:00:00Z"}); err != nil {
		t.Fatalf("setting a deadline: %v", err)
	}
	if manager.store.Tasks[0].DueAt == nil {
		t.Fatal("setting a deadline did not take")
	}
	if got := manager.store.Tasks[0].DueAt.Format(time.RFC3339); got != "2026-09-01T08:00:00Z" {
		t.Errorf("deadline round-tripped as %q", got)
	}
}

func TestInvalidDeadlineUpdateFailsWithoutClearingExistingValue(t *testing.T) {
	due := time.Date(2026, 8, 20, 14, 30, 0, 0, time.UTC)
	manager := &TaskManager{
		projectPath: t.TempDir(),
		store:       &TaskStore{Tasks: []Task{{ID: "1", Title: "before", DueAt: &due, Status: TaskStatusBacklog}}},
	}
	if err := manager.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateTask("1", map[string]interface{}{"title": "after", "dueAt": "not-a-date"}); err == nil {
		t.Fatal("invalid deadline update unexpectedly succeeded")
	}
	got, err := manager.GetTask("1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "before" || got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Fatalf("invalid deadline partially changed task: %+v", got)
	}
}
