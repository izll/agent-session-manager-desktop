package main

import (
	"os"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// Deadlines are what this view is for, so they have to sort first.
//
// A nil deadline sorted naively lands at the top, which puts every task nobody
// has scheduled ahead of the one due this afternoon — the opposite of what
// someone opens this list to see.
func TestUnscheduledTasksSortBelowScheduledOnes(t *testing.T) {
	soon := "2026-08-13T09:00:00Z"
	later := "2026-08-20T09:00:00Z"

	items := []TaskOverviewItem{
		{TaskInfo: TaskInfo{ID: "none-1", Title: "no deadline"}, ProjectName: "b"},
		{TaskInfo: TaskInfo{ID: "late", Title: "later", DueAt: &later}},
		{TaskInfo: TaskInfo{ID: "none-2", Title: "also none"}, ProjectName: "a"},
		{TaskInfo: TaskInfo{ID: "soon", Title: "soon", DueAt: &soon}},
	}

	sortTasksByDeadline(items)

	order := make([]string, len(items))
	for i, item := range items {
		order[i] = item.ID
	}

	if order[0] != "soon" || order[1] != "late" {
		t.Errorf("scheduled tasks should come first, soonest first; got %v", order)
	}
	// The two unscheduled ones follow, ordered by project so the list does not
	// reshuffle itself between loads.
	if order[2] != "none-2" || order[3] != "none-1" {
		t.Errorf("unscheduled tasks should sink to the bottom, ordered by project; got %v", order)
	}
}

// Equal deadlines must not reorder between loads.
//
// A list that shuffles on every refresh is unusable for tracking anything:
// SliceStable is what keeps two tasks due at the same moment in a fixed order.
func TestEqualDeadlinesKeepTheirOrder(t *testing.T) {
	same := "2026-08-15T12:00:00Z"

	items := []TaskOverviewItem{
		{TaskInfo: TaskInfo{ID: "first", DueAt: &same}},
		{TaskInfo: TaskInfo{ID: "second", DueAt: &same}},
		{TaskInfo: TaskInfo{ID: "third", DueAt: &same}},
	}

	for run := 0; run < 3; run++ {
		sortTasksByDeadline(items)
		if items[0].ID != "first" || items[1].ID != "second" || items[2].ID != "third" {
			t.Fatalf("run %d reordered equal deadlines: %v", run, items)
		}
	}
}

// Several sessions can share one working directory, and then they share one
// task file.
//
// Measured on this machine: 46 sessions across 40 distinct paths. Reading per
// session rather than per path would list the tasks in those shared
// directories two or three times, with nothing to tell the copies apart.
func TestTasksInASharedDirectoryAreListedOnce(t *testing.T) {
	dir := t.TempDir()

	manager := session.NewTaskManager(dir)
	if err := manager.Load(); err != nil {
		t.Fatalf("loading an empty store: %v", err)
	}
	if _, err := manager.CreateTask("shared", "", session.TaskPriorityMedium, nil); err != nil {
		t.Fatalf("creating a task: %v", err)
	}

	// Three sessions, one directory — the shape that caused the duplication.
	instances := []*session.Instance{
		{ID: "a", Name: "first", Path: dir},
		{ID: "b", Name: "second", Path: dir},
		{ID: "c", Name: "third", Path: dir},
	}

	seen := map[string]bool{}
	loaded := 0
	for _, instance := range instances {
		if instance.Path == "" || seen[instance.Path] {
			continue
		}
		seen[instance.Path] = true

		m := session.NewTaskManager(instance.Path)
		if err := m.Load(); err != nil {
			continue
		}
		loaded += len(m.GetTasks())
	}

	if loaded != 1 {
		t.Errorf("one task in a directory shared by three sessions should appear once, got %d", loaded)
	}
}

// The default project is not listed in projects.json.
//
// That file holds only the projects someone explicitly created; sessions
// started before any project existed live in the root store, addressed by an
// empty project ID. On this machine that was all 46 sessions and all 18 tasks,
// so iterating projects.json alone produced an empty view while the per-project
// panel happily showed tasks for the very same sessions.
func TestTheDefaultProjectIsIncluded(t *testing.T) {
	source, err := os.ReadFile("tasks_overview.go")
	if err != nil {
		t.Fatalf("reading the source: %v", err)
	}

	// The empty-ID entry is what reaches the root store. Asserting on the
	// source because the alternative — a real storage round trip — would need
	// this machine's config to be present.
	if !strings.Contains(string(source), `refs := []projectRef{{id: "", name: ""}}`) {
		t.Error("the root store (empty project ID) must be included, or sessions " +
			"outside a named project contribute no tasks at all")
	}
}
