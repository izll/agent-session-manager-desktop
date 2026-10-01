package main

import (
	"fmt"
	"log"

	"asmgr-desktop/session"
)

// ProjectTasksScope addresses the active project's own task list in the task
// API, in place of a session ID.
//
// The project's list is the same kind of list as a session's — same file
// format, same TaskManager — so rather than a second copy of every task
// endpoint, the existing ones accept this as the list to act on. It cannot be
// a session ID: those are generated, and never start with "@".
//
// The frontend has the same constant (PROJECT_TASKS_SCOPE in
// utils/projectScope.ts); a test keeps the two equal.
const ProjectTasksScope = "@project"

// taskOwner is the session a newly created task is tied to. A task created in
// the project's list belongs to no session.
func taskOwner(scope string) string {
	if scope == ProjectTasksScope {
		return ""
	}
	return scope
}

// withoutSessionAssignment drops any session or tab from an edit to a task in
// the project's list. Giving a project task to a session is a move into that
// session's list (MoveTaskToSession), not a field on it: a project task that
// named a session would be in one list and shown as belonging to another.
func withoutSessionAssignment(updates map[string]interface{}) map[string]interface{} {
	_, hasSession := updates["sessionId"]
	_, hasTab := updates["tabId"]
	if !hasSession && !hasTab {
		return updates
	}
	cleaned := make(map[string]interface{}, len(updates))
	for key, value := range updates {
		if key != "sessionId" && key != "tabId" {
			cleaned[key] = value
		}
	}
	return cleaned
}

// MoveTaskToSession moves a task from the project's list into a session's,
// optionally assigning it to one of that session's tabs.
//
// See session.TransferTask for what travels with it: everything but its
// dependencies, which name tasks in the list it left.
//
// The session's list here is the app's own. With Task Master switched on, the
// session panel shows Task Master's list instead — a different file — and a
// task moved into the app's list would seem to vanish. That is refused rather
// than done invisibly.
func (a *App) MoveTaskToSession(taskID, targetSessionID, tabID, expectedProjectID string) (*TaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	if a.taskMasterEnabled() {
		return nil, fmt.Errorf("error.projectTaskMoveTaskMaster")
	}
	if targetSessionID == "" || targetSessionID == ProjectTasksScope {
		return nil, fmt.Errorf("a session to move the task to is required")
	}
	from, err := a.getTaskManager(ProjectTasksScope)
	if err != nil {
		return nil, err
	}
	to, err := a.getTaskManager(targetSessionID)
	if err != nil {
		return nil, err
	}
	moved, err := session.TransferTask(from, to, taskID, targetSessionID, tabID)
	if err != nil {
		return nil, err
	}
	info := convertTask(*moved)
	return &info, nil
}

// MoveTaskToProject moves a task out of a session's list into the project's.
// It no longer belongs to the session or any of its tabs.
func (a *App) MoveTaskToProject(sessionID, taskID, expectedProjectID string) (*TaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	if sessionID == "" || sessionID == ProjectTasksScope {
		return nil, fmt.Errorf("the task is already in the project's list")
	}
	from, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}
	to, err := a.getTaskManager(ProjectTasksScope)
	if err != nil {
		return nil, err
	}
	moved, err := session.TransferTask(from, to, taskID, "", "")
	if err != nil {
		return nil, err
	}
	info := convertTask(*moved)
	return &info, nil
}

// SendProjectTaskToAgent types a project task into a session's agent, the way
// SendTaskToAgent does for a session's own task.
//
// The session, and optionally its tab, are chosen at send time: a project task
// belongs to none of them. An empty or unknown tab sends to the session's
// active window. The task stays in the project's list, unchanged — sending
// asks an agent to work on it; moving it is a separate, deliberate act.
func (a *App) SendProjectTaskToAgent(taskID, targetSessionID, tabID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(ProjectTasksScope)
	if err != nil {
		return err
	}
	prompt, err := tm.FormatTaskForAgent(taskID)
	if err != nil {
		return err
	}
	inst, err := a.storage.GetInstance(targetSessionID)
	if err != nil {
		return err
	}
	log.Printf("[TaskManager] SendProjectTaskToAgent taskID=%s session=%s tab=%q", taskID, targetSessionID, tabID)
	return inst.SendTaskToAgent(prompt, tabID)
}

// GetProjectNotePages returns the pages of the active project's own note.
func (a *App) GetProjectNotePages() ([]session.NotePage, error) {
	pages, err := a.storage.ProjectNotePages()
	if err != nil {
		return nil, err
	}
	return nonNilNotePages(pages), nil
}

// SetProjectNotePages replaces the active project's own note, all of its
// pages at once.
func (a *App) SetProjectNotePages(pages []session.NotePage, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.SetProjectNotePages(pages)
}
