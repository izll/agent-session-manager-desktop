package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"asmgr-desktop/session"
)

// Moving tabs between the sessions of the active project: one tab into
// another session, one tab out into a session of its own, a whole session
// into another. The multiplexer side is session.MoveTab, SplitTab and
// MergeSession; this is everything around it — the servers the windows are
// on, saving both sessions as one change, and the things that name a tab
// elsewhere (tasks, the running record, the quick-jump list).

// TabMoveResult says where the moved tab, or the merged session, is now.
type TabMoveResult struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	// WindowIdx is the moved tab's window in that session; for a merge, the
	// window the merged session's own agent is in now.
	WindowIdx int `json:"windowIdx"`
	// TabsMoved counts the tabs that moved — one, or all of a merged session's.
	TabsMoved int `json:"tabsMoved"`
	// TasksMoved counts the tasks that went with them.
	TasksMoved int `json:"tasksMoved"`
}

// TabMoveRefusals says, for every other session of the project, why the tab
// at windowIdx could not be moved into it — a translation key — or "" where it
// can. Asked once when a tab starts being dragged, so each session can say so
// while the tab is over it.
func (a *App) TabMoveRefusals(sourceID string, windowIdx int) (map[string]string, error) {
	return a.moveRefusals(sourceID, func(src, dst *session.Instance) string {
		return src.MoveRefusal(windowIdx, dst)
	})
}

// SessionMergeRefusals is TabMoveRefusals for merging a whole session.
func (a *App) SessionMergeRefusals(sourceID string) (map[string]string, error) {
	return a.moveRefusals(sourceID, func(src, dst *session.Instance) string {
		return src.MergeRefusal(dst)
	})
}

func (a *App) moveRefusals(sourceID string, refusal func(src, dst *session.Instance) string) (map[string]string, error) {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return nil, err
	}
	var src *session.Instance
	for _, inst := range instances {
		if inst.ID == sourceID {
			src = inst
		}
	}
	if src == nil {
		return nil, fmt.Errorf("instance not found")
	}
	refusals := make(map[string]string, len(instances))
	for _, inst := range instances {
		if inst.ID != sourceID {
			refusals[inst.ID] = refusal(src, inst)
		}
	}
	return refusals, nil
}

// MoveTabToSession moves a tab into another session of the project. A running
// tab keeps running; see session.MoveTab.
func (a *App) MoveTabToSession(sourceID string, windowIdx int, targetID, expectedProjectID string) (*TabMoveResult, error) {
	connections, err := a.connectionsForTabMove(sourceID, windowIdx)
	if err != nil {
		return nil, err
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()

	src, dst, err := a.tabMoveSessions(sourceID, targetID)
	if err != nil {
		return nil, err
	}
	routeTabMove(dst, connections)

	move, err := session.MoveTab(src, dst, windowIdx)
	if err != nil {
		return nil, err
	}
	moves := []session.TabMove{move}
	if err := a.storage.CommitTabMove(session.TabMoveCommit{
		Updated:   []*session.Instance{src, dst},
		QuickJump: retargetQuickJump(src.ID, dst.ID, moves, false),
	}); err != nil {
		return nil, errors.Join(err, session.UndoTabMoves(moves))
	}

	a.carryRunningRecord(src.ID, dst.ID, moves, false)
	tasks := a.carryTasks(src, dst, moves, false)
	return &TabMoveResult{SessionID: dst.ID, SessionName: dst.Name, WindowIdx: move.Index,
		TabsMoved: 1, TasksMoved: tasks}, nil
}

// MoveTabToNewSession makes a tab a session of its own, placed right after
// the session it leaves and in the same group. An empty name takes the tab's.
func (a *App) MoveTabToNewSession(sourceID string, windowIdx int, name, expectedProjectID string) (*TabMoveResult, error) {
	connections, err := a.connectionsForTabMove(sourceID, windowIdx)
	if err != nil {
		return nil, err
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()

	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return nil, err
	}
	src, err := a.storage.GetInstance(sourceID)
	if err != nil {
		return nil, err
	}
	fw := src.GetFollowedWindowRecord(windowIdx)
	if fw == nil {
		// Let SplitTab say which: the session's own window, or nothing.
		fw = &session.FollowedWindow{}
	}
	if name == "" {
		name = fw.Name
	}
	if name == "" {
		name = string(fw.Agent)
	}
	taken := make(map[string]bool, len(instances))
	for _, inst := range instances {
		taken[inst.ID] = true
	}
	id := session.UniqueSessionID(name, fw.Agent, taken)
	if machine := fw.RunsOn(src.ServerID); machine != "" {
		if connection := connections[machine]; connection != nil {
			session.SetExecutor(id, connection.executor)
		}
	}

	inst, move, err := session.SplitTab(src, windowIdx, id, name)
	if err != nil {
		return nil, err
	}
	moves := []session.TabMove{move}
	if err := a.storage.CommitTabMove(session.TabMoveCommit{
		Updated:    []*session.Instance{src},
		Added:      inst,
		AddedAfter: src.ID,
		QuickJump:  retargetQuickJump(src.ID, inst.ID, moves, false),
	}); err != nil {
		return nil, errors.Join(err, session.UndoTabMoves(moves))
	}

	a.carryRunningRecord(src.ID, inst.ID, moves, false)
	tasks := a.carryTasks(src, inst, moves, false)
	return &TabMoveResult{SessionID: inst.ID, SessionName: inst.Name, WindowIdx: move.Index,
		TabsMoved: 1, TasksMoved: tasks}, nil
}

// MergeSessionInto moves every tab of a session, its own window included,
// into another session, and removes the session it emptied.
//
// Removed outright rather than put in the trash: everything it held now lives
// on in the target — its tabs, its own agent as a tab, its notes as pages of
// the target's note, its tasks in the target's list. A restorable copy would
// be a second session claiming the same tabs and conversations.
//
// A failure part way keeps what was moved: both sessions are saved as they
// are, the source with the tabs that did not move, and the error says why.
func (a *App) MergeSessionInto(sourceID, targetID, expectedProjectID string) (*TabMoveResult, error) {
	connections, err := a.connectionsForMerge(sourceID)
	if err != nil {
		return nil, err
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()

	src, dst, err := a.tabMoveSessions(sourceID, targetID)
	if err != nil {
		return nil, err
	}
	routeTabMove(dst, connections)

	moves, mergeErr := session.MergeSession(src, dst)
	if len(moves) == 0 && mergeErr != nil {
		return nil, mergeErr
	}
	commit := session.TabMoveCommit{
		Updated:   []*session.Instance{dst},
		QuickJump: retargetQuickJump(src.ID, dst.ID, moves, mergeErr == nil),
	}
	if mergeErr == nil {
		commit.Removed = src.ID
	} else {
		commit.Updated = append(commit.Updated, src)
	}
	if err := a.storage.CommitTabMove(commit); err != nil {
		// Not undone: the merged session's own multiplexer session ended when
		// its last window left, so there is nothing to put the windows back
		// into. They run on in the target; the store is what failed.
		log.Printf("[tab-move] merge of %s into %s could not be saved: %v", src.ID, dst.ID, err)
		return nil, errors.Join(err, mergeErr)
	}

	a.carryRunningRecord(src.ID, dst.ID, moves, mergeErr == nil)
	tasks := a.carryTasks(src, dst, moves, mergeErr == nil)
	if mergeErr != nil {
		return nil, mergeErr
	}
	result := &TabMoveResult{SessionID: dst.ID, SessionName: dst.Name, TabsMoved: len(moves), TasksMoved: tasks}
	for _, move := range moves {
		if move.FromTabID == session.MainTabID {
			result.WindowIdx = move.Index
		}
	}
	return result, nil
}

// tabMoveSessions loads the two sessions of a move, refusing one into itself.
func (a *App) tabMoveSessions(sourceID, targetID string) (*session.Instance, *session.Instance, error) {
	if sourceID == targetID {
		return nil, nil, fmt.Errorf("error.tabMoveSameSession")
	}
	src, err := a.storage.GetInstance(sourceID)
	if err != nil {
		return nil, nil, err
	}
	dst, err := a.storage.GetInstance(targetID)
	if err != nil {
		return nil, nil, err
	}
	return src, dst, nil
}

// connectionsForTabMove opens the connection to the server a tab runs on, if
// it runs on one.
//
// Before the project lock, as CreateTabOnServer does: dialling can take as
// long as the server takes to answer, and every other mutation waits on that
// lock.
func (a *App) connectionsForTabMove(sourceID string, windowIdx int) (map[string]*serverConnection, error) {
	src, err := a.storage.GetInstance(sourceID)
	if err != nil {
		return nil, err
	}
	machine := src.ServerID
	if fw := src.GetFollowedWindowRecord(windowIdx); fw != nil {
		machine = fw.RunsOn(src.ServerID)
	}
	return a.connectionsTo([]string{machine})
}

// connectionsForMerge opens the connections to every server a session's tabs
// run on.
func (a *App) connectionsForMerge(sourceID string) (map[string]*serverConnection, error) {
	src, err := a.storage.GetInstance(sourceID)
	if err != nil {
		return nil, err
	}
	machines := []string{src.ServerID}
	for _, fw := range src.FollowedWindows {
		machines = append(machines, fw.RunsOn(src.ServerID))
	}
	return a.connectionsTo(machines)
}

func (a *App) connectionsTo(machines []string) (map[string]*serverConnection, error) {
	connections := map[string]*serverConnection{}
	for _, machine := range machines {
		if machine == "" || connections[machine] != nil {
			continue
		}
		connection, err := a.connectionFor(machine)
		if err != nil {
			return nil, err
		}
		connections[machine] = connection
	}
	return connections, nil
}

// routeTabMove points the target at the servers the moving tabs run on, for a
// tab that stays on a server the target does not run on itself.
func routeTabMove(dst *session.Instance, connections map[string]*serverConnection) {
	for machine, connection := range connections {
		if machine != dst.ServerID {
			session.SetTabExecutor(dst.ID, machine, connection.executor)
		}
	}
}

func retargetQuickJump(from, to string, moves []session.TabMove, merged bool) func([]session.QuickJumpEntry) []session.QuickJumpEntry {
	return func(entries []session.QuickJumpEntry) []session.QuickJumpEntry {
		return session.RetargetQuickJump(entries, from, to, moves, merged)
	}
}

// carryRunningRecord moves the moved tabs' running marks to where they are
// now. The stops are noted first, so a poll already under way does not put the
// tabs back under the session they left.
func (a *App) carryRunningRecord(from, to string, moves []session.TabMove, merged bool) {
	now := time.Now()
	if merged {
		a.runningStops.note(from, "", now)
	}
	for _, move := range moves {
		a.runningStops.note(from, move.FromTabID, now)
	}
	projectID := a.storage.GetActiveProjectID()
	if err := a.storage.UpdateRunningSnapshot(projectID, func(snapshot *session.RunningSnapshot) bool {
		return snapshot.CarryTabs(from, to, moves, merged)
	}); err != nil {
		log.Printf("[tab-move] could not update the running record: %v", err)
	}
}

// carryTasks moves the tasks assigned to the moved tabs into the target's
// list, still assigned to them; for a merge, every task of the merged session.
// The rest of the source's tasks stay where they are.
//
// Not with Task Master: its list is a file of its own that the app does not
// move tasks between (see MoveTaskToSession). Those tasks keep naming the
// source session and read as unassigned.
//
// A task that cannot be moved is logged and left; the tab has moved either
// way, and a task in the wrong list is recoverable where a half-move is not.
func (a *App) carryTasks(src, dst *session.Instance, moves []session.TabMove, merged bool) int {
	if a.taskMasterEnabled() {
		return 0
	}
	tabs := make(map[string]string, len(moves))
	for _, move := range moves {
		tabs[move.FromTabID] = move.TabID
	}
	from, err := cachedTaskManager(src.Path)
	if err != nil {
		log.Printf("[tab-move] tasks of %s not moved: %v", src.ID, err)
		return 0
	}
	to, err := cachedTaskManager(dst.Path)
	if err != nil {
		log.Printf("[tab-move] tasks of %s not moved: %v", src.ID, err)
		return 0
	}
	if err := from.Load(); err != nil {
		log.Printf("[tab-move] tasks of %s not moved: %v", src.ID, err)
		return 0
	}
	moved := 0
	for _, task := range from.GetTasks() {
		if task.SessionID != src.ID {
			continue
		}
		tab, assigned := tabs[task.TabID]
		if !assigned && !merged {
			continue
		}
		if _, err := session.TransferTask(from, to, task.ID, dst.ID, tab); err != nil {
			log.Printf("[tab-move] task %s of %s not moved: %v", task.ID, src.ID, err)
			continue
		}
		moved++
	}
	return moved
}
