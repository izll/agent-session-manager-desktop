package session

import (
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
