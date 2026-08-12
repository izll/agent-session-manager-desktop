package main

import (
	"sort"
	"time"

	"asmgr-desktop/session"
)

// TaskOverviewItem is one task in the all-projects view, carrying the context
// the per-project panel gets from its surroundings.
//
// The task itself does not know which project it came from — tasks are stored
// per project, one file each — so the project name and path are attached here.
// Without them the view could show a deadline but not where the work lives.
type TaskOverviewItem struct {
	TaskInfo

	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	ProjectPath string `json:"projectPath"`

	// SessionName is the session the task belongs to, resolved for display.
	// Empty when the task is project-wide or its session no longer exists —
	// a deleted session must not hide the task that outlived it.
	SessionName string `json:"sessionName,omitempty"`

	// SessionColor is the colour the user gave that session, so a group heading
	// here is recognisable as the same session shown in the sidebar. May be a
	// gradient definition rather than a plain colour, which the frontend
	// already knows how to render.
	SessionColor string `json:"sessionColor,omitempty"`

	// Overdue is computed here rather than in the frontend so "late" means the
	// same thing everywhere: past the deadline and not done. A finished task is
	// never late, however long it sat there.
	Overdue bool `json:"overdue"`
}

// GetAllTasks returns every task from every project, newest deadline first.
//
// Reads each project's task file directly rather than going through the
// per-session task manager: the manager is scoped to the active project, and
// this view exists precisely to see past that.
//
// Projects whose task file is missing or unreadable are skipped rather than
// failing the whole call. Most projects have no tasks at all, and one unusable
// file must not empty the view.
func (a *App) GetAllTasks() ([]TaskOverviewItem, error) {
	projectsData, err := a.storage.LoadProjects()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var items []TaskOverviewItem

	// Tasks live under the SESSION's working directory, not the project's:
	// a project record has no path of its own (it is a grouping, not a place on
	// disk), and getTaskManager resolves a session's path to find its task
	// file. Reading per project instead found nothing at all — the projects
	// here have an empty path, so every lookup was skipped and the view came up
	// blank while the panel showed tasks for the same sessions.
	//
	// Several sessions can share one working directory, and then they share one
	// task file too. Loaded once per distinct path so those tasks are not
	// listed twice.
	// The default project is not in projects.json — that file lists only the
	// ones explicitly created. Sessions started before any project existed live
	// in the root store, and on this machine that is all 46 of them, which is
	// why iterating projects.json alone found nothing at all.
	//
	// Represented by the empty project ID, which is what the storage layer uses
	// for the root store.
	type projectRef struct {
		id   string
		name string
	}
	// Name left empty: the default project's label is a translated string that
	// lives in the frontend, so naming it here would hard-code one language.
	refs := []projectRef{{id: "", name: ""}}
	for _, project := range projectsData.Projects {
		if project != nil {
			refs = append(refs, projectRef{id: project.ID, name: project.Name})
		}
	}

	for _, project := range refs {
		instances, _, err := a.storage.LoadAllForProject(project.id)
		if err != nil {
			continue
		}

		names := map[string]string{}
		colors := map[string]string{}
		for _, instance := range instances {
			if instance != nil {
				names[instance.ID] = instance.Name
				colors[instance.ID] = instance.Color
			}
		}

		seen := map[string]bool{}
		for _, instance := range instances {
			if instance == nil || instance.Path == "" || seen[instance.Path] {
				continue
			}
			seen[instance.Path] = true

			manager := session.NewTaskManager(instance.Path)
			if err := manager.Load(); err != nil {
				continue
			}

			for _, task := range manager.GetTasks() {
				item := TaskOverviewItem{
					TaskInfo:    convertTask(task),
					ProjectID:   project.id,
					ProjectName: project.name,
					ProjectPath: instance.Path,
					Overdue:     task.Overdue(now),
				}

				// Which session to show, and which to jump to.
				//
				// Tasks created before the session field existed have none, and
				// so would show an empty column — which is most of them right
				// now. The task file lives in a session's working directory,
				// so that session is the honest answer even when the task never
				// recorded one. Only used for display and navigation; it is not
				// written back, because guessing an assignment is not the same
				// as the user making one.
				item.SessionID = task.SessionID
				if item.SessionID == "" {
					item.SessionID = instance.ID
				}
				item.SessionName = names[item.SessionID]
				item.SessionColor = colors[item.SessionID]

				items = append(items, item)
			}
		}
	}

	sortTasksByDeadline(items)
	return items, nil
}

// sortTasksByDeadline orders tasks the way someone chasing deadlines reads
// them: soonest first, and tasks with no deadline last rather than first.
//
// A nil deadline sorted naively lands at the top, which puts every task nobody
// has scheduled ahead of the one due this afternoon.
func sortTasksByDeadline(items []TaskOverviewItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].DueAt, items[j].DueAt

		switch {
		case left == nil && right == nil:
			// Neither is scheduled: keep them together, ordered by project so
			// the list does not reshuffle between loads.
			return items[i].ProjectName < items[j].ProjectName
		case left == nil:
			return false // unscheduled sinks below anything with a deadline
		case right == nil:
			return true
		default:
			return *left < *right // RFC 3339 sorts correctly as text
		}
	})
}
