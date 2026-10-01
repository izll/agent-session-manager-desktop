package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Moving tabs between sessions.
//
// Three things are built on one operation, taking a tab out of one session and
// giving it to another:
//
//   - MoveTab: a tab goes into another session of the same project.
//   - SplitTab: a tab becomes a session of its own.
//   - MergeSession: every tab of a session, its own window included, goes
//     into another session, and the session is left empty.
//
// A running tab keeps running. Its window is handed over with move-window,
// which re-parents the window object inside the multiplexer: the pane, the
// process in it and its scrollback are untouched, and nothing is restarted.
// That is only possible inside ONE multiplexer, so a tab never changes the
// machine it runs on — see TabMoveRefusal. A tab with no process (stopped, or
// in a stopped session) moves as its record alone.
//
// The record goes with it whole: its ID, name, agent, conversation, notes,
// colours, palette, worktree and directory. Anything elsewhere that names the
// tab by its ID (a task assigned to it, the running record) is rewritten by the
// caller from the IDs reported here; the window index does change, and is
// reported too.

// TabMove says where a tab ended up.
type TabMove struct {
	// FromIndex is the window index the tab had in the session it left.
	FromIndex int
	// FromTabID is the ID it had there: its own, or MainTabID for the session's
	// own window.
	FromTabID string
	// TabID is the ID it has now: unchanged for a tab, MainTabID for a tab that
	// became a session's own window, a new ID for a session's own window that
	// became a tab.
	TabID string
	// Index is its window index in its new session.
	Index int
	// Live reports that the window itself moved, with whatever ran in it.
	Live bool

	undo func() error
}

// Undo puts a moved window back where it came from, for a caller whose save
// failed after the multiplexer had already moved it. A record move has nothing
// to undo in the multiplexer.
func (m TabMove) Undo() error {
	if m.undo == nil {
		return nil
	}
	return m.undo()
}

// UndoTabMoves undoes several moves, latest first.
func UndoTabMoves(moves []TabMove) error {
	var firstErr error
	for at := len(moves) - 1; at >= 0; at-- {
		if err := moves[at].Undo(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Refusals, as translation keys.
const (
	errTabMoveMainTab      = "error.tabMoveMainTab"
	errTabMoveSameSession  = "error.tabMoveSameSession"
	errTabMoveLocalToSrv   = "error.tabMoveLocalTabToServer"
	errTabMoveRunningLocal = "error.tabMoveRunningUnsupported"
	errTabMoveNotFound     = "error.windowNotFound"
	errTabSplitOnlyTab     = "error.tabSplitOnlyTab"
)

// TabMoveRefusal says why a tab cannot go into dst, or "" when it can.
//
// machine is where the tab runs (FollowedWindow.RunsOn); live is whether its
// window would have to move with a process in it.
//
// A tab keeps its machine. A tab on this computer can therefore join only a
// session that runs here: a session on a server has no way to hold a tab that
// runs elsewhere than on a server ("" in a tab's record means "where the
// session is"). A tab on a server can join any session — one on that server,
// where it becomes an ordinary tab, or any other, where it stays a server tab,
// the way a tab is opened on a server from a local session.
func TabMoveRefusal(machine string, live bool, dst *Instance) string {
	if machine == "" && dst.ServerID != "" {
		return errTabMoveLocalToSrv
	}
	if live && machine == "" && !RunningTabMoveSupported() {
		return errTabMoveRunningLocal
	}
	return ""
}

// MoveRefusal says why the tab at windowIdx cannot go into dst, or "".
//
// What the UI asks while a tab is dragged, so a session that will refuse it
// says so before the drop rather than after. Answered from the records alone:
// it runs on every dragover-to-be, and nothing here is worth a multiplexer
// round trip — the move itself checks again.
//
// The own window of a session that has no other tab is the whole session:
// moving it is merging the session (see IsOnlyTab), and it is refused where
// the merge would be.
func (i *Instance) MoveRefusal(windowIdx int, dst *Instance) string {
	if dst == nil || dst.ID == i.ID {
		return errTabMoveSameSession
	}
	at := i.followedAt(windowIdx)
	if at < 0 {
		if len(i.FollowedWindows) == 0 {
			return i.MergeRefusal(dst)
		}
		return errTabMoveMainTab
	}
	fw := i.FollowedWindows[at]
	live := i.Status == StatusRunning && !fw.Stopped
	return TabMoveRefusal(fw.RunsOn(i.ServerID), live, dst)
}

// SplitRefusal says why the tab at windowIdx cannot become a session of its
// own (SplitTab), or "" when it can.
//
// The rules are a move's into a session on the tab's own machine, which is
// where the new session runs. The session's own window is refused: it is a
// session already — of its own when it is the only tab, which says so.
func (i *Instance) SplitRefusal(windowIdx int) string {
	at := i.followedAt(windowIdx)
	if at < 0 {
		if len(i.FollowedWindows) == 0 {
			return errTabSplitOnlyTab
		}
		return errTabMoveMainTab
	}
	fw := i.FollowedWindows[at]
	machine := fw.RunsOn(i.ServerID)
	live := i.Status == StatusRunning && !fw.Stopped
	return TabMoveRefusal(machine, live, &Instance{ServerID: machine})
}

// IsOnlyTab reports whether windowIdx is the own window of a session that has
// no other tab — the tab that, moved, takes the whole session with it.
func (i *Instance) IsOnlyTab(windowIdx int) bool {
	return len(i.FollowedWindows) == 0 && i.isOwnWindow(windowIdx)
}

// MergeRefusal says why this whole session cannot go into dst, or "".
func (i *Instance) MergeRefusal(dst *Instance) string {
	if dst == nil || dst.ID == i.ID {
		return errTabMoveSameSession
	}
	running := i.Status == StatusRunning
	if refusal := TabMoveRefusal(i.ServerID, running && !i.MainWindowStopped, dst); refusal != "" {
		return refusal
	}
	for _, fw := range i.FollowedWindows {
		if refusal := TabMoveRefusal(fw.RunsOn(i.ServerID), running && !fw.Stopped, dst); refusal != "" {
			return refusal
		}
	}
	return ""
}

// followedAt is the position in FollowedWindows of the tab at windowIdx, or -1
// for the session's own window and for an index that names nothing.
func (i *Instance) followedAt(windowIdx int) int {
	for at := range i.FollowedWindows {
		if i.FollowedWindows[at].Index == windowIdx {
			return at
		}
	}
	return -1
}

// GetFollowedWindowRecord is the stored tab at windowIdx, or nil for the
// session's own window and for an index that names nothing. Asked of the
// record alone, unlike GetFollowedWindow, which asks the multiplexer which
// window is the session's own.
func (i *Instance) GetFollowedWindowRecord(windowIdx int) *FollowedWindow {
	if at := i.followedAt(windowIdx); at >= 0 {
		return &i.FollowedWindows[at]
	}
	return nil
}

// MoveTab moves the tab at windowIdx from src into dst.
//
// The session's own window is refused: without it the session is not a
// session. Moving all of it is MergeSession.
//
// dst must be routed to the tab's machine beforehand when that is a server
// other than dst's own (SetTabExecutor), as for a new tab there.
func MoveTab(src, dst *Instance, windowIdx int) (TabMove, error) {
	if dst == nil || src.ID == dst.ID {
		return TabMove{}, errors.New(errTabMoveSameSession)
	}
	at := src.followedAt(windowIdx)
	if at < 0 {
		if src.isOwnWindow(windowIdx) {
			return TabMove{}, errors.New(errTabMoveMainTab)
		}
		return TabMove{}, errors.New(errTabMoveNotFound)
	}
	fw := src.FollowedWindows[at]
	move, adopted, err := carryTab(src, dst, fw)
	if err != nil {
		return TabMove{}, err
	}
	src.dropFollowed(fw.ID, fw.Index)
	dst.adopt(adopted, move.Live)
	return move, nil
}

// isOwnWindow reports whether windowIdx is the session's own window, asked of
// the multiplexer while it runs. A stopped session's tab bar shows its own
// window as 0.
func (i *Instance) isOwnWindow(windowIdx int) bool {
	if i.Status != StatusRunning {
		return windowIdx == 0
	}
	mainIdx, ok := i.getMainWindowIndex()
	return ok && mainIdx == windowIdx
}

// carryTab moves one tab's window (or, without one, makes room for its record)
// into dst and returns the record as dst will hold it. Neither session's
// record is changed: the caller does that once the move has worked.
func carryTab(src, dst *Instance, fw FollowedWindow) (TabMove, FollowedWindow, error) {
	machine := fw.RunsOn(src.ServerID)
	move := TabMove{FromIndex: fw.Index, FromTabID: fw.ID, TabID: fw.ID}
	if move.FromTabID == "" {
		move.FromTabID = MainTabID
	}

	if machine == "" && dst.ServerID != "" {
		return TabMove{}, FollowedWindow{}, errors.New(errTabMoveLocalToSrv)
	}
	live, err := windowGoesWithTab(src, fw, machine, dst.Status == StatusRunning)
	if err != nil {
		return TabMove{}, FollowedWindow{}, err
	}

	adopted := fw
	if machine == dst.ServerID {
		adopted.ServerID = ""
	} else {
		adopted.ServerID = machine
	}
	// The tab worked in its session's directory, and that is where it goes on
	// working: a restart in the new session must not move it into another
	// project's files.
	if adopted.WorkDir == "" && src.Path != dst.Path {
		adopted.WorkDir = src.Path
	}

	if live {
		index, err := moveWindowInto(src, dst, machine, fw.Index, tabDirFor(src, fw))
		if err != nil {
			return TabMove{}, FollowedWindow{}, err
		}
		move.Index, move.Live = index, true
		move.undo = undoMove(src, dst, machine, fw.Index, index)
		adopted.Index = index
		return move, adopted, nil
	}

	// No process to keep: the placeholder the stopped tab left behind goes, and
	// the record moves alone.
	src.discardWindow(machine, fw.Index)
	index, parked, err := dst.slotForStoppedTab(machine, adopted)
	if err != nil {
		return TabMove{}, FollowedWindow{}, err
	}
	if parked {
		move.undo = func() error {
			return dst.tmuxRun("kill-window", "-t", fmt.Sprintf("%s:%d", dst.TmuxSessionName(), index))
		}
	}
	move.Index = index
	adopted.Index = index
	// A stopped session's tabs all start with it; a running one's stopped tab
	// waits to be started, as it did where it came from. A tab on a server is
	// not started with its session at all, and one with no window there is
	// stopped whatever its session is doing — unmarked, it would count as
	// running (see LiveTabsContext).
	adopted.Stopped = dst.Status == StatusRunning || machine != dst.ServerID
	return move, adopted, nil
}

// tabDirFor is the directory a tab works in.
func tabDirFor(src *Instance, fw FollowedWindow) string {
	if fw.WorkDir != "" {
		return fw.WorkDir
	}
	return src.Path
}

// windowGoesWithTab decides whether a tab's window moves with it, or the tab
// moves as its record and the window it leaves behind is discarded.
//
// The window goes whenever there is something in it worth keeping and the
// multiplexer can move it: a running process always, and the last output of
// an agent that exited on its own when the host session is running anyway.
// A stopped tab's window holds nothing — a parked "exit 0" — and is not worth
// starting a session for.
//
// Only a running process that cannot be moved is refused: anything else can
// still move as a record.
func windowGoesWithTab(src *Instance, fw FollowedWindow, machine string, hostRunning bool) (bool, error) {
	if fw.Stopped || src.Status != StatusRunning || !src.windowListedOn(machine, fw.Index) {
		return false, nil
	}
	alive := src.paneAlive(machine, fw.Index)
	if machine == "" && !RunningTabMoveSupported() {
		if alive {
			return false, errors.New(errTabMoveRunningLocal)
		}
		return false, nil
	}
	return alive || hostRunning, nil
}

// paneAlive reports whether the pane of a window known to exist has a process
// in it. Asked only of a window that is listed: display-message falls back to
// the current window when the one named is gone.
func (i *Instance) paneAlive(machine string, windowIdx int) bool {
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	out, err := i.tmuxOutputOn(machine, "display-message", "-p", "-t", target, "#{pane_dead}")
	return err == nil && strings.TrimSpace(string(out)) == "0"
}

// windowListedOn asks one machine whether this session has a window at
// windowIdx there.
func (i *Instance) windowListedOn(machine string, windowIdx int) bool {
	out, err := i.tmuxOutputOn(machine, "list-windows", "-t", i.TmuxSessionName(), "-F", "#{window_index}")
	return err == nil && tmuxWindowIndexListed(out, windowIdx)
}

// discardWindow removes what is left of a stopped tab's window, with the
// views of it. Nothing at all is fine: a stopped session has no windows.
func (i *Instance) discardWindow(machine string, windowIdx int) {
	if i.Status != StatusRunning || !i.windowListedOn(machine, windowIdx) {
		return
	}
	i.killWindowViews(machine, windowIdx)
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	if err := i.tmuxRunOn(machine, "kill-window", "-t", target); err != nil {
		log.Printf("[tab-move] %s: could not remove the stopped window %d: %v", i.ID, windowIdx, err)
	}
}

// killWindowViews ends the sessions that exist only to show one window: the
// local mirrors the terminal attaches through, and a server's view.
//
// They have to go when the window leaves. A view is linked to the window
// object, not to an index, so it would go on showing the moved tab under the
// old session's name — and the next tab given this index would find the old
// mirror first and be captured from the wrong window. Killing a view only
// unlinks the window from it; the window and its process stay.
func (i *Instance) killWindowViews(machine string, windowIdx int) {
	sessionName := i.TmuxSessionName()
	out, err := i.tmuxOutputOn(machine, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return
	}
	mirrorPrefix := fmt.Sprintf("%s_gui_%d_", sessionName, windowIdx)
	view := remoteViewName(sessionName, windowIdx)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, mirrorPrefix) || name == view {
			_ = i.tmuxRunOn(machine, "kill-session", "-t", name)
		}
	}
	captureTargetCache.Delete(fmt.Sprintf("%s:%d", sessionName, windowIdx))
}

// moveWindowInto hands src's window at fromIdx to dst on machine and returns
// the index it was given there.
//
// The index is chosen the way a new tab's is: by the multiplexer, the lowest
// free one, for a tab on dst's own machine; from the server's band for a tab
// that stays on a server dst does not run on (see nextRemoteWindowIndex).
func moveWindowInto(src, dst *Instance, machine string, fromIdx int, workDir string) (int, error) {
	if machine == dst.ServerID {
		if err := dst.ensureRunningForTab(); err != nil {
			return -1, err
		}
	} else if err := dst.ensureRemoteSessionFor(machine, workDir); err != nil {
		return -1, err
	}

	srcName, dstName := src.TmuxSessionName(), dst.TmuxSessionName()
	from := fmt.Sprintf("%s:%d", srcName, fromIdx)
	windowID, err := src.windowID(machine, from)
	if err != nil {
		return -1, err
	}

	target := dstName + ":"
	if machine != dst.ServerID {
		target = fmt.Sprintf("%s:%d", dstName, dst.freeBandIndex(machine))
	}

	src.killWindowViews(machine, fromIdx)
	if err := src.tmuxRunOn(machine, "move-window", "-d", "-s", from, "-t", target); err != nil {
		return -1, fmt.Errorf("error.tabMoveFailed|%v", err)
	}
	index, ok := dst.windowIndexOf(machine, windowID)
	if !ok {
		return -1, fmt.Errorf("error.tabMoveFailed|%s", "the moved window was not found")
	}
	moved := fmt.Sprintf("%s:%d", dstName, index)
	// The window keeps its name and its process; these two are what a tab
	// window of ours is created with, and a window that was a session's own
	// may not have had automatic-rename turned off.
	_ = dst.tmuxRunOn(machine, "set-option", "-w", "-t", moved, "remain-on-exit", "on")
	_ = dst.tmuxRunOn(machine, "set-option", "-w", "-t", moved, "automatic-rename", "off")
	return index, nil
}

// undoMove returns a moved window to the index it came from.
func undoMove(src, dst *Instance, machine string, fromIdx, toIdx int) func() error {
	return func() error {
		from := fmt.Sprintf("%s:%d", dst.TmuxSessionName(), toIdx)
		back := fmt.Sprintf("%s:%d", src.TmuxSessionName(), fromIdx)
		return src.tmuxRunOn(machine, "move-window", "-d", "-s", from, "-t", back)
	}
}

// windowID is the multiplexer's own ID (@N) of a window. It stays with the
// window through a move, which is how the window is found again on the other
// side: move-window does not say where it put it.
func (i *Instance) windowID(machine, target string) (string, error) {
	out, err := i.tmuxOutputOn(machine, "display-message", "-p", "-t", target, "#{window_id}")
	id := strings.TrimSpace(string(out))
	if err != nil || !strings.HasPrefix(id, "@") {
		return "", fmt.Errorf("error.tabMoveFailed|%s", "the window could not be identified")
	}
	return id, nil
}

// windowIndexOf finds where in this session a window (by its @ID) is.
func (i *Instance) windowIndexOf(machine, windowID string) (int, bool) {
	out, err := i.tmuxOutputOn(machine, "list-windows", "-t", i.TmuxSessionName(),
		"-F", "#{window_id}\t#{window_index}")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 || parts[0] != windowID {
			continue
		}
		var index int
		if _, err := fmt.Sscanf(parts[1], "%d", &index); err == nil {
			return index, true
		}
	}
	return 0, false
}

// freeBandIndex is nextRemoteWindowIndex, also stepping over a window the
// server already has there — a leftover no record names any more.
func (i *Instance) freeBandIndex(machine string) int {
	index := i.nextRemoteWindowIndex(machine)
	for i.windowListedOn(machine, index) {
		index++
	}
	return index
}

// slotForStoppedTab gives a tab arriving without a window its index in this
// session.
//
// A running session gets a parked window for it, made the way a stopped tab's
// window is made when the session starts. The index has to be held by a real
// window: one held only by a record is handed to the next new tab by the
// multiplexer, and claimWindowIndex then drops this record as stale.
//
// parked reports that a window was made for it.
func (i *Instance) slotForStoppedTab(machine string, fw FollowedWindow) (index int, parked bool, err error) {
	if machine != i.ServerID {
		// A server tab is not rebuilt with its session; its index comes from
		// the band, which no local window ever takes.
		return i.nextRemoteWindowIndex(machine), false, nil
	}
	if i.Status != StatusRunning {
		// Renumbered when the session starts. Only has to be unique in the
		// record until then.
		highest := 0
		for _, window := range i.FollowedWindows {
			if window.sameMachine(i.ServerID, i.ServerID) && window.Index > highest {
				highest = window.Index
			}
		}
		return highest + 1, false, nil
	}

	sessionName := i.TmuxSessionName()
	dir := fw.WorkDir
	if dir == "" {
		dir = i.Path
	}
	out, err := i.tmuxOutput(newTmuxWindowArgs(sessionName, dir, fw.Name, true, nil)...)
	if err != nil {
		return -1, false, fmt.Errorf("error.tabMoveFailed|%v", err)
	}
	index, err = parseTmuxWindowIndex(out)
	if err != nil {
		return -1, false, fmt.Errorf("error.tabMoveFailed|%v", err)
	}
	target := fmt.Sprintf("%s:%d", sessionName, index)
	_ = i.tmuxRun("set-option", "-w", "-t", target, "remain-on-exit", "on")
	_ = i.tmuxRun("set-option", "-w", "-t", target, "automatic-rename", "off")
	_ = i.tmuxRun(respawnPaneArgs(nil, target, "exit", "0")...)
	return index, true, nil
}

// adopt takes a moved tab into this session's record.
func (i *Instance) adopt(fw FollowedWindow, fromMultiplexer bool) {
	if fromMultiplexer {
		// The index was the multiplexer's to give; a record still claiming
		// it outlived its window.
		i.claimWindowIndex(fw.Index, fw.RunsOn(i.ServerID))
	}
	i.FollowedWindows = append(i.FollowedWindows, fw)
	if len(i.TabOrder) > 0 {
		i.TabOrder = append(i.TabOrder, fw.Index)
	}
}

// dropFollowed takes a tab that has left out of this session's record.
func (i *Instance) dropFollowed(tabID string, windowIdx int) {
	kept := i.FollowedWindows[:0]
	for _, fw := range i.FollowedWindows {
		if fw.ID != tabID {
			kept = append(kept, fw)
		}
	}
	i.FollowedWindows = kept
	if len(i.TabOrder) > 0 {
		order := make([]int, 0, len(i.TabOrder))
		for _, index := range i.TabOrder {
			if index != windowIdx {
				order = append(order, index)
			}
		}
		i.TabOrder = order
	}
}

// ensureRunningForTab makes sure this session has its multiplexer session for
// a running tab to be moved into.
//
// A stopped session is brought up the way "start only this tab" brings one
// up, but without starting anything: its own window comes up parked, its tabs
// as stopped placeholders, and the only thing running in it afterwards is the
// tab that is moving in. Starting the session's agent just to stop it again —
// what StartOnlyWindow does — would run it for no reason, and an agent that
// resumes a conversation is not something to run for no reason.
func (i *Instance) ensureRunningForTab() error {
	// The stored status can be behind the multiplexer in either direction.
	i.UpdateStatus()
	if i.Status == StatusRunning {
		return nil
	}
	if !i.IsRemote() {
		if err := CheckMultiplexer(); err != nil {
			return err
		}
	}
	unlock := lockSessionStart(i.TmuxSessionName())
	defer unlock()

	sessionName := i.TmuxSessionName()
	if i.tmuxRun("has-session", "-t", sessionName) != nil {
		if err := i.newParkedSession(i.Path); err != nil {
			return err
		}
	}
	mainIdx, ok := i.soleWindowIndex()
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", sessionName)
	}
	mainTarget := fmt.Sprintf("%s:%d", sessionName, mainIdx)
	if PerWindowOptionsSupported() || i.IsRemote() {
		_ = i.tmuxRun("set-option", "-w", "-t", mainTarget, "@asmgr_main", "1")
	}
	_ = i.tmuxRun("rename-window", "-t", mainTarget, i.WindowName())

	i.Status = StatusRunning
	i.UpdatedAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	if err := i.parkMainWindowContext(ctx, sessionName, mainIdx); err != nil {
		return err
	}
	i.restoreFollowedWindows(noTabRunning)
	return nil
}

// noTabRunning asks restoreFollowedWindows to bring every tab back parked: it
// is no window's index, so no tab is "the one to start".
const noTabRunning = -2

// newParkedSession creates this session's multiplexer session holding one
// shell, set up like every session of ours.
func (i *Instance) newParkedSession(dir string) error {
	sessionName := i.TmuxSessionName()
	_ = i.tmuxRun("set-option", "-g", "history-limit", scrollbackLines)
	args := []string{"new-session", "-d", "-s", sessionName, "-c", dir}
	markStarted(sessionName)
	if i.IsRemote() {
		if err := i.tmuxRun(args...); err != nil {
			return fmt.Errorf("failed to create tmux session on %s: %w", i.exec().Describe(), err)
		}
	} else {
		cmd := TmuxCommand(args...)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}
	for attempt := 0; attempt < 20; attempt++ {
		if i.tmuxRun("has-session", "-t", sessionName) == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	i.configureSessionOptions(sessionName)
	return nil
}

// soleWindowIndex is the index of this session's only window, asked of the
// machine it runs on.
func (i *Instance) soleWindowIndex() (int, bool) {
	out, err := i.tmuxOutput("list-windows", "-t", i.TmuxSessionName(), "-F", "#{window_index}")
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		return 0, false
	}
	var index int
	if _, err := fmt.Sscanf(lines[0], "%d", &index); err != nil {
		return 0, false
	}
	return index, true
}

// SplitTab makes the tab at windowIdx a session of its own, named name, with
// the ID id.
//
// The tab becomes the new session's own window: its agent, conversation,
// directory, colours, palette and worktree become the session's, and its note
// the note of the session's own tab. A running tab keeps running — its window
// is moved into a multiplexer session made for it — and the new session runs;
// a tab with no process gives a stopped session.
//
// The new session runs where the tab ran. Its group is the source session's.
// When the new session is on a server, its route must be registered
// beforehand (SetExecutor), as for any session there.
func SplitTab(src *Instance, windowIdx int, id, name string) (*Instance, TabMove, error) {
	at := src.followedAt(windowIdx)
	if at < 0 {
		if src.isOwnWindow(windowIdx) {
			return nil, TabMove{}, errors.New(errTabMoveMainTab)
		}
		return nil, TabMove{}, errors.New(errTabMoveNotFound)
	}
	fw := src.FollowedWindows[at]
	machine := fw.RunsOn(src.ServerID)
	// A tab whose agent exited on its own gives a stopped session, not a
	// running one around a dead pane.
	live, err := windowGoesWithTab(src, fw, machine, false)
	if err != nil {
		return nil, TabMove{}, err
	}

	now := time.Now()
	inst := &Instance{
		ID:                 id,
		Name:               name,
		Path:               tabDirFor(src, fw),
		Status:             StatusStopped,
		CreatedAt:          now,
		UpdatedAt:          now,
		ServerID:           machine,
		GroupID:            src.GroupID,
		Agent:              fw.Agent,
		CustomCommand:      fw.CustomCommand,
		ExtraArgs:          fw.ExtraArgs,
		AutoYes:            fw.AutoYes,
		ResumeSessionID:    fw.ResumeSessionID,
		HideStatusLine:     fw.HideStatusLine,
		MainWindowName:     fw.Name,
		TerminalTheme:      fw.TerminalTheme,
		TerminalFontSize:   fw.TerminalFontSize,
		HideViewBar:        fw.HideViewBar,
		HideStatusBar:      fw.HideStatusBar,
		TabTextColor:       fw.TextColor,
		TabBackgroundColor: fw.BackgroundColor,
		WorktreeDir:        fw.WorktreeDir,
		WorktreeBranch:     fw.WorktreeBranch,
		WorktreeRepoRoot:   fw.WorktreeRepoRoot,
	}
	inst.MainTabNote().SetPages(fw.Note().Pages())

	move := TabMove{FromIndex: fw.Index, FromTabID: fw.ID, TabID: MainTabID}
	if live {
		index, err := moveWindowIntoNewSession(src, inst, machine, fw.Index)
		if err != nil {
			return nil, TabMove{}, err
		}
		move.Index, move.Live = index, true
		move.undo = undoMove(src, inst, machine, fw.Index, index)
	} else {
		src.discardWindow(machine, fw.Index)
	}
	src.dropFollowed(fw.ID, fw.Index)
	return inst, move, nil
}

// moveWindowIntoNewSession creates inst's multiplexer session on machine
// around src's window at fromIdx, which becomes its own window.
//
// A session cannot be created empty, so it is created with a shell that is
// removed once the window has arrived.
func moveWindowIntoNewSession(src, inst *Instance, machine string, fromIdx int) (int, error) {
	unlock := lockSessionStart(inst.TmuxSessionName())
	defer unlock()

	from := fmt.Sprintf("%s:%d", src.TmuxSessionName(), fromIdx)
	windowID, err := src.windowID(machine, from)
	if err != nil {
		return -1, err
	}
	if err := inst.newParkedSession(inst.Path); err != nil {
		return -1, err
	}
	placeholder, ok := inst.soleWindowIndex()
	if !ok {
		_ = inst.tmuxRun("kill-session", "-t", inst.TmuxSessionName())
		return -1, fmt.Errorf("error.tabMoveFailed|%s", "the new session has no window")
	}

	name := inst.TmuxSessionName()
	src.killWindowViews(machine, fromIdx)
	if err := src.tmuxRunOn(machine, "move-window", "-d", "-s", from, "-t", name+":"); err != nil {
		_ = inst.tmuxRun("kill-session", "-t", name)
		return -1, fmt.Errorf("error.tabMoveFailed|%v", err)
	}
	index, ok := inst.windowIndexOf(machine, windowID)
	if !ok {
		return -1, fmt.Errorf("error.tabMoveFailed|%s", "the moved window was not found")
	}
	_ = inst.tmuxRun("kill-window", "-t", fmt.Sprintf("%s:%d", name, placeholder))

	target := fmt.Sprintf("%s:%d", name, index)
	if PerWindowOptionsSupported() || inst.IsRemote() {
		_ = inst.tmuxRun("set-option", "-w", "-t", target, "@asmgr_main", "1")
	}
	_ = inst.tmuxRun("select-window", "-t", target)
	inst.Status = StatusRunning
	inst.MainWindowStopped = !inst.paneAlive(machine, index)
	return index, nil
}

// MergeSession moves every tab of src into dst, its own window last, which
// becomes an ordinary tab there. src is left with nothing; removing it is the
// caller's.
//
// Its own window becomes a tab with a new ID, carrying what was the session's:
// agent, conversation, directory, colours and the note of its own tab. The
// session's note — shared by all of its tabs — is added to dst's as pages of
// their own, titled with src's name.
//
// Nothing moves unless everything can: the refusals are checked for every tab
// first. A failure part way (the multiplexer refusing a move) stops there and
// returns the moves made so far, so the caller can save both sessions as they
// now are, or undo them.
func MergeSession(src, dst *Instance) ([]TabMove, error) {
	if refusal := src.MergeRefusal(dst); refusal != "" {
		return nil, fmt.Errorf("%s", refusal)
	}
	// Asked before anything leaves: with tabs gone, "the one window that is
	// not a tab" stops identifying it on a multiplexer without the marker.
	mainIdx := 0
	if src.Status == StatusRunning {
		index, ok := src.getMainWindowIndex()
		if !ok {
			return nil, fmt.Errorf("cannot identify main tmux window for session %s", src.TmuxSessionName())
		}
		mainIdx = index
	}

	// Likewise the servers its tabs are on, for the sessions it holds there.
	servers := src.tabServerIDs()

	var moves []TabMove
	for _, fw := range append([]FollowedWindow(nil), src.FollowedWindows...) {
		move, adopted, err := carryTab(src, dst, fw)
		if err != nil {
			return moves, err
		}
		src.dropFollowed(fw.ID, fw.Index)
		dst.adopt(adopted, move.Live)
		moves = append(moves, move)
	}

	own := src.ownWindowAsTab(mainIdx)
	move, adopted, err := carryTab(src, dst, own)
	if err != nil {
		return moves, err
	}
	move.FromTabID = MainTabID
	if move.Live {
		target := fmt.Sprintf("%s:%d", dst.TmuxSessionName(), move.Index)
		// No longer anyone's own window: the marker would make dst's own
		// window ambiguous, and a session with two marks refuses to say which
		// is its own (identifyMainWindowIndex).
		_ = dst.tmuxRunOn(src.ServerID, "set-option", "-wu", "-t", target, "@asmgr_main")
		_ = dst.tmuxRunOn(src.ServerID, "rename-window", "-t", target, own.Name)
	}
	dst.adopt(adopted, move.Live)
	moves = append(moves, move)

	appendSessionNote(dst, src)
	src.discardEmptySessions(servers)
	return moves, nil
}

// ownWindowAsTab is the session's own window described as a tab, with a new
// ID: the one it had, MainTabID, is every session's.
func (i *Instance) ownWindowAsTab(mainIdx int) FollowedWindow {
	name := i.MainWindowName
	if name == "" {
		name = i.Name
	}
	fw := FollowedWindow{
		ID:               newTabID(),
		Index:            mainIdx,
		Agent:            i.Agent,
		Name:             name,
		CustomCommand:    i.CustomCommand,
		AutoYes:          i.AutoYes,
		ResumeSessionID:  i.ResumeSessionID,
		ExtraArgs:        i.ExtraArgs,
		Stopped:          i.Status == StatusRunning && i.MainWindowStopped,
		TerminalTheme:    i.TerminalTheme,
		TerminalFontSize: i.TerminalFontSize,
		HideViewBar:      i.HideViewBar,
		HideStatusBar:    i.HideStatusBar,
		TextColor:        i.TabTextColor,
		BackgroundColor:  i.TabBackgroundColor,
		HideStatusLine:   i.HideStatusLine,
		WorktreeDir:      i.WorktreeDir,
		WorktreeBranch:   i.WorktreeBranch,
		WorktreeRepoRoot: i.WorktreeRepoRoot,
	}
	fw.Note().SetPages(i.MainTabNote().Pages())
	return fw
}

// appendSessionNote adds src's session note to dst's as pages of its own.
func appendSessionNote(dst, src *Instance) {
	incoming := src.SessionNote().Pages()
	var kept []NotePage
	for _, page := range incoming {
		if strings.TrimSpace(page.Text) == "" {
			continue
		}
		title := src.Name
		if page.Title != "" {
			title = src.Name + " – " + page.Title
		}
		kept = append(kept, NotePage{Title: title, Text: page.Text})
	}
	if len(kept) == 0 {
		return
	}
	pages := dst.SessionNote().Pages()
	if len(pages) == 1 && pages[0].Title == "" && strings.TrimSpace(pages[0].Text) == "" {
		pages = nil
	}
	dst.SessionNote().SetPages(append(pages, kept...))
}

// discardEmptySessions ends what is left of this session in the multiplexers
// once its last tab has gone: the views, and on a server the session that held
// its tabs there, which keeps a placeholder shell of its own. Its own session
// ended by itself when its last window left.
//
// servers are the ones its tabs were on, read before they left.
func (i *Instance) discardEmptySessions(servers []string) {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	sessionName := i.TmuxSessionName()
	machines := append([]string{i.ServerID}, servers...)
	for _, machine := range machines {
		i.killViewSessionsOn(ctx, machine, sessionName)
		if machine != i.ServerID {
			_ = i.execOn(machine).Run(ctx, "kill-session", "-t", sessionName)
		}
	}
	i.Status = StatusStopped
	i.MainWindowStopped = false
	i.FollowedWindows = nil
	i.TabOrder = nil
}
