package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// What was running, kept so a restart can put it back.
//
// A reboot never stops anything: the multiplexer is killed with the machine,
// and the next launch finds every session stopped with nothing to say which of
// them the user had been working in. Nor can the stored session say it — its
// status is whatever the last save wrote, and the per-tab Stopped marks only
// cover tabs the user stopped by hand, not an agent that exited on its own.
//
// So the sidebar poll, which already looks at every running session, writes
// down what it saw: per session, which tabs had a live process. An explicit
// stop takes the session (or the tab) out again. The record lives beside the
// project's sessions.json in a file of its own, so the 1 Hz bookkeeping never
// competes with — or rolls back — an edit to the sessions themselves.

// runningSnapshotFile is the file name, per project.
const runningSnapshotFile = "running.json"

// maxRunningSnapshotBytes bounds a read; the file lists IDs, and even a
// thousand sessions with a dozen tabs each stays far below this.
const maxRunningSnapshotBytes = 4 << 20

// RunningRecord is one session as the poll last saw it running.
type RunningRecord struct {
	// Tabs lists the tabs that had a live process: MainTabID for the
	// session's own window, a FollowedWindow's ID for each of its tabs. A tab
	// the user had stopped, or whose agent had exited, is not in it.
	Tabs []string `json:"tabs"`
	// Multiplexer identifies the multiplexer server the session ran in (see
	// MultiplexerIdentity). A later launch that finds the same server still
	// running knows the session was ended on its own rather than lost with the
	// machine; empty when the server could not say.
	Multiplexer string `json:"multiplexer,omitempty"`
	// SeenAt is when the poll last saw it running.
	SeenAt time.Time `json:"seen_at"`
}

// Has reports whether a tab was among the running ones.
func (r RunningRecord) Has(tabID string) bool {
	return slices.Contains(r.Tabs, tabID)
}

// RunningSnapshot is a project's record of what was running.
type RunningSnapshot struct {
	Sessions map[string]RunningRecord `json:"sessions"`
}

// sameRecord compares two records by what matters for a restart. SeenAt is
// left out on purpose: rewriting the file every poll just to move a timestamp
// would put a disk write under every tick.
func sameRecord(a, b RunningRecord) bool {
	return a.Multiplexer == b.Multiplexer && slices.Equal(a.Tabs, b.Tabs)
}

// runningSnapshotPath is the file for one project. The default project keeps
// its sessions in the config directory itself, and so does this.
func (s *Storage) runningSnapshotPath(projectID string) (string, error) {
	if !validProjectID(projectID) {
		return "", fmt.Errorf("invalid project ID")
	}
	if projectID == "" {
		return filepath.Join(s.configDir, runningSnapshotFile), nil
	}
	return filepath.Join(s.configDir, "projects", projectID, runningSnapshotFile), nil
}

// LoadRunningSnapshot reads a project's record. A missing file is an empty
// record — the first launch, or a project nothing has run in yet.
func (s *Storage) LoadRunningSnapshot(projectID string) (RunningSnapshot, error) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	return s.loadRunningSnapshotLocked(projectID)
}

func (s *Storage) loadRunningSnapshotLocked(projectID string) (RunningSnapshot, error) {
	empty := RunningSnapshot{Sessions: map[string]RunningRecord{}}
	path, err := s.runningSnapshotPath(projectID)
	if err != nil {
		return empty, err
	}
	data, err := readFileAtMost(path, maxRunningSnapshotBytes)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var snapshot RunningSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		// A torn or hand-edited file must not stop the app from starting. The
		// worst this loses is one offer to reopen; the next poll rewrites it.
		return empty, nil
	}
	if snapshot.Sessions == nil {
		snapshot.Sessions = map[string]RunningRecord{}
	}
	return snapshot, nil
}

// UpdateRunningSnapshot changes a project's record under its lock. The file is
// written only when edit reports a change.
func (s *Storage) UpdateRunningSnapshot(projectID string, edit func(*RunningSnapshot) bool) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	snapshot, err := s.loadRunningSnapshotLocked(projectID)
	if err != nil {
		return err
	}
	if !edit(&snapshot) {
		return nil
	}
	path, err := s.runningSnapshotPath(projectID)
	if err != nil {
		return err
	}
	if len(snapshot.Sessions) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// RecordRunning stores what the poll saw running. A session with no live tab
// left is taken out: there is nothing of it to bring back.
func (snapshot *RunningSnapshot) RecordRunning(sessionID string, record RunningRecord) bool {
	if len(record.Tabs) == 0 {
		return snapshot.Forget(sessionID)
	}
	if current, ok := snapshot.Sessions[sessionID]; ok && sameRecord(current, record) {
		return false
	}
	snapshot.Sessions[sessionID] = record
	return true
}

// Forget takes a session out of the record: it was stopped on purpose, or no
// longer exists.
func (snapshot *RunningSnapshot) Forget(sessionID string) bool {
	if _, ok := snapshot.Sessions[sessionID]; !ok {
		return false
	}
	delete(snapshot.Sessions, sessionID)
	return true
}

// ForgetTab takes one tab out of a session's record, after the user stopped it.
func (snapshot *RunningSnapshot) ForgetTab(sessionID, tabID string) bool {
	record, ok := snapshot.Sessions[sessionID]
	if !ok || !record.Has(tabID) {
		return false
	}
	var tabs []string
	for _, id := range record.Tabs {
		if id != tabID {
			tabs = append(tabs, id)
		}
	}
	record.Tabs = tabs
	if len(tabs) == 0 {
		delete(snapshot.Sessions, sessionID)
	} else {
		snapshot.Sessions[sessionID] = record
	}
	return true
}

// ── Telling a restart from an ordinary relaunch ─────────────────────────────

// Interruption is what became of a session recorded as running.
type Interruption int

const (
	// StillRunning: the session is alive. Closing and reopening the app does
	// not touch the multiplexer, so there is nothing to bring back.
	StillRunning Interruption = iota
	// Interrupted: the session is gone and so is the multiplexer it ran in —
	// a reboot, or the server killed. This is what is offered for reopening.
	Interrupted
	// EndedOnItsOwn: the session is gone but the multiplexer that ran it is
	// still the same one. Something ended this session alone (a kill-session,
	// another tool), and a restart prompt would be offering something the
	// machine never lost. The record is dropped.
	EndedOnItsOwn
	// Undecided: the session's machine could not be asked — a server not
	// connected yet. Kept for a later look, never offered on a guess.
	Undecided
)

// JudgeInterruption decides what a recorded session's absence means.
//
// recorded is the multiplexer identity stored with it, current the one the
// same machine reports now (empty when no server is running there, or it
// cannot say). reachable is false only for a server whose connection is not
// open; this computer is always reachable.
func JudgeInterruption(alive, reachable bool, recorded, current string) Interruption {
	switch {
	case alive:
		return StillRunning
	case !reachable:
		return Undecided
	case recorded != "" && current == recorded:
		return EndedOnItsOwn
	default:
		return Interrupted
	}
}

// multiplexerIdentityFormat names the server process and when it started. The
// pid alone could be reused after a reboot; with the start time it cannot.
// (tmux before 3.2 has no start_time and prints it empty, which still leaves
// the pid.)
const multiplexerIdentityFormat = "#{pid} #{start_time}"

// MultiplexerIdentityContext asks the multiplexer on the session's machine who
// it is. Empty when no server is running there or the answer is unusable — a
// multiplexer that does not expand the format echoes it back.
func (i *Instance) MultiplexerIdentityContext(ctx context.Context) string {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.exec().Output(commandCtx, "display-message", "-p", multiplexerIdentityFormat)
	if err != nil {
		return ""
	}
	return parseMultiplexerIdentity(output)
}

func parseMultiplexerIdentity(output []byte) string {
	// The first field has to be the pid: an error message, or a multiplexer
	// that echoes the format back unexpanded, is not an identity.
	identity := strings.TrimSpace(string(output))
	fields := strings.Fields(identity)
	if len(fields) == 0 || strings.Trim(fields[0], "0123456789") != "" {
		return ""
	}
	return identity
}

// MachineReachable reports whether the machine the session runs on can be
// asked anything: always for this computer, and for a server only once its
// connection is open.
func (i *Instance) MachineReachable() bool {
	_, unreachable := i.exec().(unreachableExecutor)
	return !unreachable
}

// ── Which tabs are running ─────────────────────────────────────────────────

// LiveTabsContext lists the tabs of a running session that have a live
// process right now, by tab ID (MainTabID for the session's own window).
//
// One listing of the session's own machine answers for the main window and
// every tab there: a dead pane is a stopped tab or an agent that exited. A tab
// on another machine is not asked — that would be a round trip per server per
// poll — and counts as running unless the user stopped it; its window lives on
// that server and a local restart does not touch it anyway.
//
// ok is false when the listing failed, so the caller keeps what it had rather
// than recording "nothing running" for a session it simply could not read.
func (i *Instance) LiveTabsContext(ctx context.Context) ([]string, bool) {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.tmuxOutputContext(commandCtx, "list-windows", "-t", i.TmuxSessionName(),
		"-F", "#{window_index}\t#{@asmgr_main}\t#{pane_dead}")
	if err != nil {
		return nil, false
	}
	return liveTabsFromListing(output, i.FollowedWindows, i.ServerID)
}

// liveTabsFromListing is LiveTabsContext's reading of the listing, apart so it
// can be tested without a multiplexer. Each line is index, main marker, dead.
func liveTabsFromListing(output []byte, followed []FollowedWindow, sessionServerID string) ([]string, bool) {
	dead := map[int]bool{}
	var identify strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		var index int
		if _, err := fmt.Sscanf(parts[0], "%d", &index); err != nil {
			continue
		}
		dead[index] = strings.TrimSpace(parts[2]) == "1"
		identify.WriteString(parts[0] + "\t" + parts[1] + "\n")
	}
	if len(dead) == 0 {
		return nil, false
	}

	// Only the tabs on this machine can be told apart from the main window by
	// the listing; the others are not in it.
	var here []FollowedWindow
	for _, window := range followed {
		if window.sameMachine(sessionServerID, sessionServerID) {
			here = append(here, window)
		}
	}

	var live []string
	if mainIdx, ok := identifyMainWindowIndex([]byte(identify.String()), here); ok {
		if isDead, listed := dead[mainIdx]; listed && !isDead {
			live = append(live, MainTabID)
		}
	}
	for _, window := range followed {
		if !window.sameMachine(sessionServerID, sessionServerID) {
			if !window.Stopped {
				live = append(live, window.ID)
			}
			continue
		}
		if isDead, listed := dead[window.Index]; listed && !isDead && !window.Stopped {
			live = append(live, window.ID)
		}
	}
	return live, true
}

// ── Bringing it back ─────────────────────────────────────────────────────────

// StartWithRunningTabs starts a stopped session with only the tabs that were
// running brought back running; the rest come back the way a tab the user
// stopped does, as a parked placeholder that can be started later.
//
// Otherwise exactly a start with resume: the session's own conversation is
// continued from resumeID (or the stored one), and each tab from its own. A tab
// on a server was never this computer's to lose; it is started there only if
// it was running and is not any more.
func (i *Instance) StartWithRunningTabs(resumeID string, record RunningRecord) error {
	for at := range i.FollowedWindows {
		window := &i.FollowedWindows[at]
		if window.sameMachine(i.ServerID, i.ServerID) {
			window.Stopped = !record.Has(window.ID)
		}
	}
	if err := i.startWithResume(resumeID, allWindows); err != nil {
		return err
	}
	if !record.Has(MainTabID) {
		if err := i.stopMainWindowAfterStart(); err != nil {
			log.Printf("[StartWithRunningTabs] session=%s could not park the main window: %v", i.ID, err)
		}
	}
	for _, window := range append([]FollowedWindow(nil), i.FollowedWindows...) {
		if window.sameMachine(i.ServerID, i.ServerID) || !record.Has(window.ID) {
			continue
		}
		if err := i.startServerTabIfIdle(window.Index); err != nil {
			log.Printf("[StartWithRunningTabs] session=%s tab %q on its server did not start: %v", i.ID, window.Name, err)
		}
	}
	return nil
}
