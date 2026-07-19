package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"asmgr-desktop/mcp"
	"asmgr-desktop/session"
	"asmgr-desktop/session/filters"
	"asmgr-desktop/updater"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds the application state
type App struct {
	ctx              context.Context
	storage          *session.Storage
	historyIndex     *session.HistoryIndex
	ptys             map[string]*ptySession
	ptyMu            sync.RWMutex
	termServer       *TerminalServer
	dictation        *DictationService
	activityStats    *ActivityStatsRecorder
	previewCancel    context.CancelFunc
	previewWG        sync.WaitGroup
	lastTypingSignal int64 // unix nano timestamp of last typing signal
	// projectLocked is true when THIS instance owns the active project's lock.
	// otherInstancePID is the PID of the instance that owns it instead (0 if
	// none). Terminal attaches are refused unless projectLocked, so a second
	// GUI on the same project can't fight over its tmux sessions.
	projectLocked    bool
	otherInstancePID int
}

// ptySession represents an active PTY connection
type ptySession struct {
	ptmx     *os.File
	cmd      *exec.Cmd
	session  *session.Instance
	windowID int
	cancel   context.CancelFunc
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		ptys: make(map[string]*ptySession),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Enable verbose status detection logs only in dev builds
	session.DebugLogging = isDevMode

	// Initialize storage
	storage, err := session.NewStorage()
	if err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to initialize storage: %v", err))
		return
	}
	a.storage = storage

	// Single-instance-per-project guard. Two GUIs on the same project attach
	// to the same tmux sessions and rip each other's ptys out
	// ("read /dev/ptmx: input/output error"), silently killing tabs. Try to
	// claim the active project's lock; if another live instance holds it,
	// record the holder's PID so the frontend can warn instead of stomping
	// the tmux state. We DON'T abort startup — the UI is still usable for
	// read-only browsing — but terminal attaches are gated on a.projectLocked.
	if lockErr := a.storage.LockProject(a.storage.GetActiveProjectID()); lockErr != nil {
		var locked *session.ErrProjectLocked
		if errors.As(lockErr, &locked) {
			a.otherInstancePID = locked.PID
			log.Printf("[lock] project already open in pid %d — terminal attaches disabled to protect its tmux sessions", locked.PID)
		} else {
			log.Printf("[lock] could not acquire project lock: %v", lockErr)
		}
	} else {
		a.projectLocked = true
	}

	statsRecorder, err := NewActivityStatsRecorder()
	if err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to initialize activity statistics: %v", err))
	} else {
		a.activityStats = statsRecorder
	}

	// Start WebSocket terminal server for low-latency terminal I/O
	a.termServer = NewTerminalServer(storage, 9753)
	a.termServer.typingSignal = &a.lastTypingSignal
	a.termServer.attachAllowed = &a.projectLocked
	if err := a.termServer.Start(); err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to start terminal server: %v", err))
	}

	// Attention notifications (desktop/ntfy when an agent starts waiting).
	// Backend-side so it keeps working while the window is unfocused.
	a.startAttentionWatcher()

	// Set dictation callbacks (instance created in main.go)
	a.dictation.SetTerminalServer(a.termServer)
	a.dictation.SetStateChangeCallback(func(listening bool) {
		runtime.EventsEmit(ctx, "dictation:state", listening)
	})
	a.dictation.SetErrorCallback(func(title, message string) {
		runtime.EventsEmit(ctx, "dictation:error", map[string]string{
			"title":   title,
			"message": message,
		})
	})
	// Throttle voice level events to ~10Hz for smooth UI without flooding Wails events
	var lastVoiceEmit time.Time
	a.dictation.SetVoiceLevelCallback(func(level float64) {
		now := time.Now()
		if now.Sub(lastVoiceEmit) < 80*time.Millisecond {
			return
		}
		lastVoiceEmit = now
		runtime.EventsEmit(ctx, "dictation:voiceLevel", level)
	})
	a.dictation.SetInterimTextCallback(func(text string) {
		runtime.EventsEmit(ctx, "dictation:interimText", text)
	})
	a.dictation.SetFieldTextCallback(func(text string) {
		runtime.EventsEmit(ctx, "dictation:fieldText", text)
	})
	a.dictation.SetFieldDeleteCallback(func(count int) {
		runtime.EventsEmit(ctx, "dictation:fieldDelete", count)
	})

	// Clean up orphaned GUI tmux sessions from previous runs
	go a.cleanupOrphanedGUISessions()

	// Start preview polling in background
	previewCtx, previewCancel := context.WithCancel(ctx)
	a.previewCancel = previewCancel
	a.previewWG.Add(1)
	go func() {
		defer a.previewWG.Done()
		a.startPreviewPolling(previewCtx)
	}()

	// Pull remote agent filters periodically. Hard-coded URL + host
	// allowlist; refuses to run until session/filters/remote.go has a
	// real RemoteFiltersURL set, so this is a no-op for now.
	filters.StartRemoteUpdater(ctx)
}

// IsDevMode returns whether the app is running in dev mode
func (a *App) IsDevMode() bool {
	return isDevMode
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// Release the project lock only if WE hold it — a second instance that
	// failed to acquire it must not delete the real owner's lock file.
	if a.projectLocked {
		a.storage.UnlockProject()
	}
	if a.previewCancel != nil {
		a.previewCancel()
		a.previewWG.Wait()
	}
	if a.activityStats != nil {
		if err := a.activityStats.Close(); err != nil {
			log.Printf("[statistics] failed to flush: %v", err)
		}
	}

	// Clean up all GUI linked tmux sessions
	a.cleanupAllGUISessions()

	// Close all PTY sessions
	a.ptyMu.Lock()
	for id, ps := range a.ptys {
		if ps.cancel != nil {
			ps.cancel()
		}
		if ps.ptmx != nil {
			ps.ptmx.Close()
		}
		if ps.cmd != nil && ps.cmd.Process != nil {
			_ = ps.cmd.Wait()
		}
		delete(a.ptys, id)
	}
	a.ptyMu.Unlock()

	// Shutdown dictation service
	if a.dictation != nil {
		a.dictation.Shutdown()
	}

	// Stop and reap every cached MCP child. Copy the values out before Stop so
	// no potentially blocking process shutdown runs while the global lock is held.
	taskMasterMu.Lock()
	taskMasters := make([]*mcp.TaskMaster, 0, len(taskMasterCache))
	for path, tm := range taskMasterCache {
		taskMasters = append(taskMasters, tm)
		delete(taskMasterCache, path)
	}
	taskMasterMu.Unlock()
	for _, tm := range taskMasters {
		_ = tm.Stop()
	}
}

// cleanupOrphanedGUISessions removes GUI linked tmux sessions that belong to
// sessions that are no longer running (e.g. from a previous app crash).
func (a *App) cleanupOrphanedGUISessions() {
	// Only the project owner may sweep mirrors. A second instance's "running"
	// set is built from ITS view of storage and would classify the owner's
	// live mirrors as orphaned, killing them and dropping the owner's
	// terminals. Non-owners created no mirrors, so there is nothing to reap.
	if !a.projectLocked {
		return
	}

	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil || len(out) == 0 {
		return
	}

	// Build two sets from THIS project's storage: every session we own
	// (mine) and which of those are running. A mirror is reaped only when its
	// base session is ours AND not running — a mirror whose base belongs to
	// another PROJECT (loaded from a different sessions.json) is invisible
	// here and must be left alone, or closing one project would kill another
	// project's live terminals.
	instances, _, _, _ := a.storage.LoadAllWithSettings()
	mine := make(map[string]bool)
	running := make(map[string]bool)
	for _, inst := range instances {
		mine[inst.ID] = true
		if inst.Status == session.StatusRunning {
			running[inst.ID] = true
		}
	}

	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.Contains(name, "_gui_") {
			continue
		}
		// Extract base session name (everything before first _gui_)
		// Session IDs are asm_<agent>_<name>_<timestamp>, and GUI sessions
		// are <sessionID>_gui_<windowIdx>_<timestamp>, so first match is safe
		idx := strings.Index(name, "_gui_")
		if idx <= 0 {
			continue
		}
		baseName := name[:idx]
		if mine[baseName] && !running[baseName] {
			exec.Command("tmux", "kill-session", "-t", name).Run()
		}
	}
}

// cleanupAllGUISessions removes THIS project's GUI linked tmux sessions on
// app shutdown.
func (a *App) cleanupAllGUISessions() {
	// Only the project owner sweeps, and only its OWN mirrors. Two guards:
	//  1. a non-owner created no mirrors (attaches refused) — nothing to do;
	//  2. even as owner, kill only mirrors whose base session is in THIS
	//     project's storage, so closing one project's window can't kill a
	//     different project's live terminals (the _gui_ namespace is shared
	//     across all projects on the machine).
	if !a.projectLocked {
		return
	}

	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil || len(out) == 0 {
		return
	}

	instances, _, _, _ := a.storage.LoadAllWithSettings()
	mine := make(map[string]bool)
	for _, inst := range instances {
		mine[inst.ID] = true
	}

	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		idx := strings.Index(name, "_gui_")
		if idx <= 0 {
			continue
		}
		if mine[name[:idx]] {
			exec.Command("tmux", "kill-session", "-t", name).Run()
		}
	}
}

// ============================================================================
// Project Management
// ============================================================================

// ProjectInfo represents project data for frontend
type ProjectInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsLocked bool   `json:"isLocked"`
}

// GetProjects returns all projects
func (a *App) GetProjects() ([]ProjectInfo, error) {
	projectsData, err := a.storage.LoadProjects()
	if err != nil {
		return nil, err
	}

	result := make([]ProjectInfo, len(projectsData.Projects))
	for i, p := range projectsData.Projects {
		locked, _ := a.storage.IsProjectLocked(p.ID)
		result[i] = ProjectInfo{
			ID:       p.ID,
			Name:     p.Name,
			IsLocked: locked,
		}
	}
	return result, nil
}

// SelectProject switches to a project, moving the single-instance lock with
// it: release the old project's lock and claim the new one. If the target is
// already open elsewhere, the switch still happens (so the user can view it)
// but this instance stays unlocked and terminal attaches remain disabled.
func (a *App) SelectProject(id string) error {
	if err := a.storage.SetActiveProject(id); err != nil {
		return err
	}
	a.otherInstancePID = 0
	if err := a.storage.LockProject(id); err != nil {
		a.projectLocked = false
		var locked *session.ErrProjectLocked
		if errors.As(err, &locked) {
			a.otherInstancePID = locked.PID
			log.Printf("[lock] switched to project %q which is open in pid %d — terminal attaches disabled", id, locked.PID)
		}
	} else {
		a.projectLocked = true
	}
	return nil
}

// LockStatusInfo tells the frontend whether this instance owns the active
// project (and if not, which PID does) so it can warn the user.
type LockStatusInfo struct {
	Locked           bool `json:"locked"`
	OtherInstancePID int  `json:"otherInstancePid"`
}

// GetLockStatus reports whether this instance owns the active project's lock.
func (a *App) GetLockStatus() LockStatusInfo {
	return LockStatusInfo{Locked: a.projectLocked, OtherInstancePID: a.otherInstancePID}
}

// CreateProject creates a new project
func (a *App) CreateProject(name string) (*ProjectInfo, error) {
	project, err := a.storage.AddProject(name)
	if err != nil {
		return nil, err
	}
	return &ProjectInfo{
		ID:       project.ID,
		Name:     project.Name,
		IsLocked: false,
	}, nil
}

// DeleteProject deletes a project
func (a *App) DeleteProject(id string) error {
	return a.storage.RemoveProject(id)
}

// GetActiveProjectID returns current project ID
func (a *App) GetActiveProjectID() string {
	return a.storage.GetActiveProjectID()
}

// BrowseDirectory opens a native directory picker dialog
func (a *App) BrowseDirectory(defaultPath string) (string, error) {
	options := runtime.OpenDialogOptions{
		Title:            "Select Project Directory",
		DefaultDirectory: defaultPath,
	}
	return runtime.OpenDirectoryDialog(a.ctx, options)
}

// GetProjectSessions returns sessions from a specific project (for import dialog)
func (a *App) GetProjectSessions(projectID string) ([]SessionInfo, error) {
	// Use atomic project-switching load to avoid race conditions
	instances, _, err := a.storage.LoadAllForProject(projectID)
	if err != nil {
		return nil, err
	}

	// Convert to SessionInfo
	result := make([]SessionInfo, len(instances))
	for i, inst := range instances {
		result[i] = SessionInfo{
			ID:       inst.ID,
			Name:     inst.Name,
			Path:     inst.Path,
			Status:   string(inst.Status),
			Agent:    string(inst.Agent),
			Color:    inst.Color,
			BgColor:  inst.BgColor,
			GroupID:  inst.GroupID,
			Notes:    inst.Notes,
			Favorite: inst.Favorite,
		}
	}

	return result, nil
}

// ImportSessions imports selected sessions from another project
func (a *App) ImportSessions(sourceProjectID string, sessionIDs []string) (int, error) {
	// Load source project data atomically
	sourceInstances, sourceGroups, err := a.storage.LoadAllForProject(sourceProjectID)
	if err != nil {
		return 0, err
	}

	// Filter to only selected sessions
	selectedInstances := make([]*session.Instance, 0)
	selectedGroupIDs := make(map[string]bool)
	for _, inst := range sourceInstances {
		for _, id := range sessionIDs {
			if inst.ID == id {
				selectedInstances = append(selectedInstances, inst)
				if inst.GroupID != "" {
					selectedGroupIDs[inst.GroupID] = true
				}
				break
			}
		}
	}

	// Get groups that are needed
	selectedGroups := make([]*session.Group, 0)
	for _, g := range sourceGroups {
		if selectedGroupIDs[g.ID] {
			selectedGroups = append(selectedGroups, g)
		}
	}

	// Load current project's data
	currentInstances, currentGroups, err := a.storage.LoadAll()
	if err != nil {
		return 0, err
	}

	// Merge groups (avoid duplicates by name)
	for _, g := range selectedGroups {
		exists := false
		var existingGroupID string
		for _, cg := range currentGroups {
			if cg.Name == g.Name {
				exists = true
				existingGroupID = cg.ID
				break
			}
		}
		if !exists {
			currentGroups = append(currentGroups, g)
		} else {
			// Update instances to use existing group ID
			for _, inst := range selectedInstances {
				if inst.GroupID == g.ID {
					inst.GroupID = existingGroupID
				}
			}
		}
	}

	// Merge instances (check for ID conflicts)
	existingIDs := make(map[string]bool)
	for _, inst := range currentInstances {
		existingIDs[inst.ID] = true
	}

	importCount := 0
	for _, inst := range selectedInstances {
		if !existingIDs[inst.ID] {
			currentInstances = append(currentInstances, inst)
			importCount++
		}
	}

	// Save merged data
	if err := a.storage.SaveWithGroups(currentInstances, currentGroups); err != nil {
		return 0, err
	}

	return importCount, nil
}

// ============================================================================
// Session Management
// ============================================================================

// SessionInfo represents session data for frontend
type SessionInfo struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	Path               string                   `json:"path"`
	Status             string                   `json:"status"`
	Agent              string                   `json:"agent"`
	Color              string                   `json:"color"`
	BgColor            string                   `json:"bgColor"`
	FullRowColor       bool                     `json:"fullRowColor"`
	GroupID            string                   `json:"groupId"`
	AutoYes            bool                     `json:"autoYes"`
	HideStatusLine     bool                     `json:"hideStatusLine"`
	Notes              string                   `json:"notes"`
	Favorite           bool                     `json:"favorite"`
	ResumeSessionID    string                   `json:"resumeSessionId"`
	FollowedWindows    []session.FollowedWindow `json:"followedWindows"`
	MainWindowStopped  bool                     `json:"mainWindowStopped"`
	TabOrder           []int                    `json:"tabOrder"`
	ExtraArgs          string                   `json:"extraArgs"`
	TabTextColor       string                   `json:"tabTextColor"`
	TabBackgroundColor string                   `json:"tabBackgroundColor"`
}

// GetSessions returns all sessions
func (a *App) GetSessions() ([]SessionInfo, error) {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return nil, err
	}

	result := make([]SessionInfo, len(instances))
	for i, inst := range instances {
		// Update status from tmux
		inst.UpdateStatus()
		result[i] = a.instanceToSessionInfo(inst)
	}
	return result, nil
}

// GetProjectGitSummaries returns an isolated Git snapshot for every session in
// the requested project. Using an explicit project ID avoids cross-project
// snapshots when the user switches projects while a refresh is in flight.
func (a *App) GetProjectGitSummaries(projectID string) ([]ProjectGitSummary, error) {
	instances, _, err := a.storage.LoadAllForProject(projectID)
	if err != nil {
		return nil, err
	}
	return collectProjectGitSummaries(a.ctx, instances), nil
}

func (a *App) instanceToSessionInfo(inst *session.Instance) SessionInfo {
	mainStopped := inst.MainWindowStopped
	// Auto-detect dead main pane from tmux (handles pre-existing sessions)
	if inst.Status == session.StatusRunning && !mainStopped {
		if inst.IsMainWindowDead() {
			mainStopped = true
			inst.MainWindowStopped = true
		}
	}
	return SessionInfo{
		ID:                 inst.ID,
		Name:               inst.Name,
		Path:               inst.Path,
		Status:             string(inst.Status),
		Agent:              string(inst.Agent),
		Color:              inst.Color,
		BgColor:            inst.BgColor,
		FullRowColor:       inst.FullRowColor,
		GroupID:            inst.GroupID,
		AutoYes:            inst.AutoYes,
		HideStatusLine:     inst.HideStatusLine,
		Notes:              inst.Notes,
		Favorite:           inst.Favorite,
		ResumeSessionID:    inst.ResumeSessionID,
		FollowedWindows:    inst.FollowedWindows,
		MainWindowStopped:  mainStopped,
		TabOrder:           inst.GetTabOrder(),
		ExtraArgs:          inst.ExtraArgs,
		TabTextColor:       inst.TabTextColor,
		TabBackgroundColor: inst.TabBackgroundColor,
	}
}

// CreateSession creates a new session
func (a *App) CreateSession(name, path string, agent string, autoYes bool, extraArgs string) (*SessionInfo, error) {
	inst, err := session.NewInstance(name, path, autoYes, session.AgentType(agent), extraArgs)
	if err != nil {
		return nil, err
	}
	if err := a.storage.AddInstance(inst); err != nil {
		return nil, err
	}
	info := a.instanceToSessionInfo(inst)
	return &info, nil
}

// StartSession starts a session
func (a *App) StartSession(id string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}

	// StartSession is called for NEW sessions (no resume).
	// Clear any saved ResumeSessionID so it doesn't accidentally resume.
	// StartWithResume("") will generate a fresh --session-id if supported.
	log.Printf("[StartSession] id=%s agent=%s clearing old ResumeSessionID=%q", id, inst.Agent, inst.ResumeSessionID)
	inst.ResumeSessionID = ""

	if err := inst.Start(); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// StartSessionWithResume starts a session with resume.
// If the supplied resume ID no longer exists on disk (Claude/Codex deleted
// the conversation file, moved machine, etc.), we drop it and start fresh
// instead of letting the CLI boot into a "No conversation found" error.
func (a *App) StartSessionWithResume(id, resumeID string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	log.Printf("[StartSessionWithResume] id=%s agent=%s resumeID=%q", id, inst.Agent, resumeID)

	// Hard reject any resume ID that isn't a safe shape, regardless of agent
	// (ResumeIDExists only file-checks Claude/Codex and returns true for
	// others). This stops a crafted ID from reaching the tmux command line.
	if resumeID != "" && !session.IsSafeResumeID(resumeID) {
		log.Printf("[StartSessionWithResume] rejected unsafe resumeID=%q — starting fresh", resumeID)
		resumeID = ""
		if !session.IsSafeResumeID(inst.ResumeSessionID) {
			inst.ResumeSessionID = ""
		}
	}

	if resumeID != "" && !session.ResumeIDExists(inst.Agent, resumeID) {
		log.Printf("[StartSessionWithResume] resume ID %q no longer exists for agent=%s — starting fresh", resumeID, inst.Agent)
		// Wipe persisted ID too — next start should also be clean.
		if inst.ResumeSessionID == resumeID {
			inst.ResumeSessionID = ""
		}
		resumeID = ""
	}

	if err := inst.StartWithResume(resumeID); err != nil {
		return err
	}
	inst.ResumeSessionID = resumeID
	return a.storage.UpdateInstance(inst)
}

// StopSession stops a session
func (a *App) StopSession(id string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.Stop(); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// RestartTab restarts a stopped tab (dead pane) in a session
func (a *App) RestartTab(id string, windowIdx int) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.RestartWindow(windowIdx); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// RestartTabWithResume restarts a stopped tab with a specific resume session ID
func (a *App) RestartTabWithResume(id string, windowIdx int, resumeId string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}

	// Validate the resume ID exists for whichever agent owns this tab.
	if resumeId != "" {
		tabAgent := inst.Agent
		if windowIdx != 0 {
			for _, fw := range inst.FollowedWindows {
				if fw.Index == windowIdx {
					tabAgent = fw.Agent
					break
				}
			}
		}
		if !session.ResumeIDExists(tabAgent, resumeId) {
			log.Printf("[RestartTabWithResume] resume ID %q no longer exists for agent=%s — starting fresh", resumeId, tabAgent)
			resumeId = ""
			// Also clear any persisted ID for this tab so future starts don't try again.
			if windowIdx == 0 {
				if inst.ResumeSessionID == "" || inst.ResumeSessionID != "" {
					inst.ResumeSessionID = ""
				}
			} else {
				for i := range inst.FollowedWindows {
					if inst.FollowedWindows[i].Index == windowIdx {
						inst.FollowedWindows[i].ResumeSessionID = ""
						break
					}
				}
			}
		}
	}

	if err := inst.RestartWindowWithResume(windowIdx, resumeId); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// StopTab stops a specific tab (tmux window) in a session
func (a *App) StopTab(id string, windowIdx int) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.StopWindow(windowIdx); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// DeleteTab deletes a tab (followed window) from a session
func (a *App) DeleteTab(id string, windowIdx int) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.DeleteWindow(windowIdx); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// DeleteSession deletes a session
func (a *App) DeleteSession(id string) error {
	return a.storage.RemoveInstance(id)
}

// RenameSession renames a session
func (a *App) RenameSession(id, name string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Name = name
	return a.storage.UpdateInstance(inst)
}

// ToggleFavorite toggles favorite status
func (a *App) ToggleFavorite(id string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Favorite = !inst.Favorite
	return a.storage.UpdateInstance(inst)
}

// CycleYoloMode cycles the permission mode of a RUNNING Claude window by sending
// Shift+Tab (tmux key "BTab") to its pane — exactly what pressing Shift+Tab in
// the terminal does (default → auto mode → bypass → ...). This keeps the YOLO
// button consistent with the live indicator (which reads the pane), with no
// session restart. Falls back to the stored-flag toggle (+restart) when the
// session isn't running or isn't Claude, so YOLO can still be preset offline.
func (a *App) CycleYoloMode(id string, windowIdx int) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	// Determine the agent of the targeted window.
	agent := inst.Agent
	if windowIdx > 0 {
		for _, fw := range inst.FollowedWindows {
			if fw.Index == windowIdx {
				agent = fw.Agent
				break
			}
		}
	}
	if inst.IsAlive() && agent == session.AgentClaude {
		return inst.SendKeysToWindow(windowIdx, "BTab") // Shift+Tab
	}
	// Not running / not Claude: preset via the stored flag (restarts if alive).
	return a.ToggleAutoYes(id)
}

// ToggleAutoYes toggles YOLO mode and restarts the session if running
func (a *App) ToggleAutoYes(id string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.AutoYes = !inst.AutoYes
	if err := a.storage.UpdateInstance(inst); err != nil {
		return err
	}

	// Restart session if it's running so the flag takes effect
	if inst.IsAlive() {
		// Capture the current Claude session ID before stopping,
		// so we can resume the same conversation after restart.
		resumeID := inst.ResumeSessionID
		if resumeID == "" && inst.Agent == session.AgentClaude {
			// Try to get the session ID from the running Claude process args
			resumeID = getClaudeSessionIDFromTmux(inst.TmuxSessionName())
		}

		log.Printf("[ToggleAutoYes] session=%s resumeID=%s", id, resumeID)

		if err := inst.Stop(); err != nil {
			return fmt.Errorf("failed to stop session for YOLO toggle: %w", err)
		}
		// Brief pause for tmux cleanup
		time.Sleep(500 * time.Millisecond)
		if err := inst.StartWithResume(resumeID); err != nil {
			return fmt.Errorf("failed to restart session after YOLO toggle: %w", err)
		}
		if resumeID != "" {
			inst.ResumeSessionID = resumeID
		}
		if err := a.storage.UpdateInstance(inst); err != nil {
			return err
		}
		// Notify frontend to reconnect terminal
		runtime.EventsEmit(a.ctx, "session:restarted", id)
		return nil
	}
	return nil
}

// getClaudeSessionIDFromTmux extracts the --resume or --session-id from the Claude process
// running in the given tmux session's main window (window 0).
func getClaudeSessionIDFromTmux(tmuxSession string) string {
	return getClaudeSessionIDFromTmuxWindow(tmuxSession, 0)
}

// getClaudeSessionIDFromTmuxWindow extracts the --resume or --session-id from the Claude process
// running in the given tmux session window by reading /proc/PID/cmdline.
func getClaudeSessionIDFromTmuxWindow(tmuxSession string, windowIdx int) string {
	// Get the PID of the process in the tmux pane
	target := fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
	out, err := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_pid}").Output()
	if err != nil {
		return ""
	}
	panePID := strings.TrimSpace(string(out))
	if panePID == "" {
		return ""
	}

	// The pane PID is the shell; find child processes (the actual claude process)
	childOut, err := exec.Command("pgrep", "-P", panePID).Output()
	if err != nil {
		return ""
	}

	// Check each child process for --resume or --session-id flag
	for _, pidStr := range strings.Fields(string(childOut)) {
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%s/cmdline", pidStr))
		if err != nil {
			continue
		}
		// cmdline is null-separated
		args := strings.Split(string(cmdline), "\x00")
		for i, arg := range args {
			if (arg == "--resume" || arg == "--session-id") && i+1 < len(args) && args[i+1] != "" {
				candidate := args[i+1]
				// This value comes from the argv of a process running INSIDE
				// the agent's pane — it is not trusted input. Reject anything
				// that isn't a safe ID shape so a hostile agent can't smuggle
				// shell metacharacters into a later respawn-pane command.
				if !session.IsSafeResumeID(candidate) {
					log.Printf("[getClaudeSessionIDFromTmux] PID %s %s value rejected (unsafe shape): %q", pidStr, arg, candidate)
					continue
				}
				log.Printf("[getClaudeSessionIDFromTmux] found session ID from PID %s (flag %s): %s", pidStr, arg, candidate)
				return candidate
			}
		}
	}

	return ""
}

// SetSessionColor sets session colors
func (a *App) SetSessionColor(id, color, bgColor string, fullRow bool) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Color = color
	inst.BgColor = bgColor
	inst.FullRowColor = fullRow
	return a.storage.UpdateInstance(inst)
}

// SetSessionNotes sets session notes
func (a *App) SetSessionNotes(id string, notes string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Notes = notes
	return a.storage.UpdateInstance(inst)
}

// AssignToGroup assigns session to group
func (a *App) AssignToGroup(sessionID, groupID string) error {
	return a.storage.SetInstanceGroup(sessionID, groupID)
}

// ReorderSession moves a session up or down in the list
func (a *App) ReorderSession(sessionID string, direction int) error {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return err
	}

	// Find current index
	currentIdx := -1
	for i, inst := range instances {
		if inst.ID == sessionID {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		return fmt.Errorf("error.sessionNotFound")
	}

	// Calculate new index
	newIdx := currentIdx + direction
	if newIdx < 0 || newIdx >= len(instances) {
		return nil // Can't move further
	}

	// Swap
	instances[currentIdx], instances[newIdx] = instances[newIdx], instances[currentIdx]

	// Save
	groups, err := a.storage.GetGroups()
	if err != nil {
		return err
	}
	return a.storage.SaveWithGroups(instances, groups)
}

// MoveSessionToIndex moves a session to a specific index in the list
func (a *App) MoveSessionToIndex(sessionID string, targetIndex int) error {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return err
	}

	// Find current index
	currentIdx := -1
	for i, inst := range instances {
		if inst.ID == sessionID {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		return fmt.Errorf("error.sessionNotFound")
	}

	// Clamp target index
	if targetIndex < 0 {
		targetIndex = 0
	}
	if targetIndex >= len(instances) {
		targetIndex = len(instances) - 1
	}

	// No change needed
	if currentIdx == targetIndex {
		return nil
	}

	// Remove from current position
	item := instances[currentIdx]
	instances = append(instances[:currentIdx], instances[currentIdx+1:]...)

	// Insert at new position
	instances = append(instances[:targetIndex], append([]*session.Instance{item}, instances[targetIndex:]...)...)

	// Save
	groups, err := a.storage.GetGroups()
	if err != nil {
		return err
	}
	return a.storage.SaveWithGroups(instances, groups)
}

// SendPrompt sends text to session
func (a *App) SendPrompt(id string, text string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	return inst.SendPrompt(text)
}

// ============================================================================
// Fork Session (Claude only)
// ============================================================================

// ForkResult contains fork operation result
type ForkResult struct {
	SessionID string `json:"sessionId"`
}

// ForkSession forks a Claude session
func (a *App) ForkSession(id string) (*ForkResult, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	if inst.Agent != session.AgentClaude {
		return nil, fmt.Errorf("error.forkClaudeOnly")
	}

	sessionID, err := inst.ForkSession()
	if err != nil {
		return nil, err
	}

	return &ForkResult{SessionID: sessionID}, nil
}

// ForkToNewTab forks to a new tab
func (a *App) ForkToNewTab(id, name, sessionID string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.NewForkedTab(name, sessionID); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// ForkToNewSession creates a new session from forked Claude conversation
func (a *App) ForkToNewSession(id, name, sessionID string) (*SessionInfo, error) {
	origInst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	// Create new session with same settings
	newInst, err := session.NewInstance(name, origInst.Path, origInst.AutoYes, session.AgentClaude, origInst.ExtraArgs)
	if err != nil {
		return nil, err
	}

	// Copy settings from original
	newInst.GroupID = origInst.GroupID
	newInst.Color = origInst.Color
	newInst.BgColor = origInst.BgColor
	newInst.FullRowColor = origInst.FullRowColor
	newInst.Notes = fmt.Sprintf("Forked from: %s", origInst.Name)
	newInst.ResumeSessionID = sessionID

	// Save new session
	if err := a.storage.AddInstance(newInst); err != nil {
		return nil, err
	}

	// Start the forked session with resume
	if err := newInst.StartWithResume(sessionID); err != nil {
		// Don't fail if start fails, session is still created
		runtime.LogWarning(a.ctx, fmt.Sprintf("Failed to auto-start forked session: %v", err))
	}
	a.storage.UpdateInstance(newInst)

	info := a.instanceToSessionInfo(newInst)
	return &info, nil
}

// ============================================================================
// Groups
// ============================================================================

// GroupInfo represents group data for frontend
type GroupInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Collapsed    bool   `json:"collapsed"`
	Color        string `json:"color"`
	BgColor      string `json:"bgColor"`
	FullRowColor bool   `json:"fullRowColor"`
}

// GetGroups returns all groups
func (a *App) GetGroups() ([]GroupInfo, error) {
	groups, err := a.storage.GetGroups()
	if err != nil {
		return nil, err
	}

	result := make([]GroupInfo, len(groups))
	for i, g := range groups {
		result[i] = GroupInfo{
			ID:           g.ID,
			Name:         g.Name,
			Collapsed:    g.Collapsed,
			Color:        g.Color,
			BgColor:      g.BgColor,
			FullRowColor: g.FullRowColor,
		}
	}
	return result, nil
}

// CreateGroup creates a new group
func (a *App) CreateGroup(name string) (*GroupInfo, error) {
	group, err := a.storage.AddGroup(name)
	if err != nil {
		return nil, err
	}
	return &GroupInfo{
		ID:   group.ID,
		Name: group.Name,
	}, nil
}

// DeleteGroup deletes a group
func (a *App) DeleteGroup(id string) error {
	return a.storage.RemoveGroup(id)
}

// RenameGroup renames a group
func (a *App) RenameGroup(id, name string) error {
	return a.storage.RenameGroup(id, name)
}

// ToggleGroupCollapse toggles group collapsed state
func (a *App) ToggleGroupCollapse(id string) error {
	return a.storage.ToggleGroupCollapsed(id)
}

// SetGroupColor sets group colors
func (a *App) SetGroupColor(id, color, bgColor string, fullRow bool) error {
	groups, err := a.storage.GetGroups()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.ID == id {
			g.Color = color
			g.BgColor = bgColor
			g.FullRowColor = fullRow
			instances, _, err := a.storage.LoadAll()
			if err != nil {
				return err
			}
			return a.storage.SaveWithGroups(instances, groups)
		}
	}
	return fmt.Errorf("error.groupNotFound")
}

// ============================================================================
// Tabs (Multi-window support)
// ============================================================================

// CreateTab creates a new tab in session
// CreateTab creates a new tab and returns the new tmux window index so the
// frontend can switch to (and focus) it immediately.
func (a *App) CreateTab(sessionID string, isAgent bool, agent string, name string, extraArgs string, workDir string) (int, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return -1, err
	}

	newIdx := -1
	if isAgent {
		agentType := session.AgentType(agent)
		idx, err := inst.NewAgentWindow(name, agentType, "", extraArgs, workDir)
		if err != nil {
			return -1, err
		}
		newIdx = idx
	} else {
		if err := inst.NewWindowWithName(name, workDir); err != nil {
			return -1, err
		}
		// tmux new-window selects the created window, so this is its index.
		newIdx = inst.GetCurrentWindowIndex()
	}
	return newIdx, a.storage.UpdateInstance(inst)
}

// CloseTab closes a tab
func (a *App) CloseTab(sessionID string, windowIdx int) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if err := inst.CloseWindow(windowIdx); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// RenameTab renames a tab
func (a *App) RenameTab(sessionID string, windowIdx int, name string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	// Select window and rename
	if err := inst.SelectWindow(windowIdx); err != nil {
		return err
	}
	if err := inst.RenameCurrentWindow(name); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// ReorderTab reorders a tab within a session's display order.
// fromPos and toPos are indices into the tab display order (0-based, including main window).
func (a *App) ReorderTab(sessionID string, fromPos, toPos int) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if err := inst.ReorderTabs(fromPos, toPos); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// GetTabOrder returns the custom tab display order for a session.
func (a *App) GetTabOrder(sessionID string) ([]int, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, err
	}
	return inst.GetTabOrder(), nil
}

// SetTabNotes sets tab notes
func (a *App) SetTabNotes(sessionID string, windowIdx int, notes string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	// Window 0 uses session notes
	if windowIdx == 0 {
		inst.Notes = notes
		return a.storage.UpdateInstance(inst)
	}
	for i := range inst.FollowedWindows {
		if inst.FollowedWindows[i].Index == windowIdx {
			inst.FollowedWindows[i].Notes = notes
			return a.storage.UpdateInstance(inst)
		}
	}
	return fmt.Errorf("error.windowNotFound")
}

// SetTabColor sets the optional text and background colors for a tab.
// Empty values clear an override; textColor also supports "auto".
func (a *App) SetTabColor(sessionID string, windowIdx int, textColor, backgroundColor string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if err := inst.SetTabColors(windowIdx, textColor, backgroundColor); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// GetTabNotes gets tab notes
func (a *App) GetTabNotes(sessionID string, windowIdx int) (string, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return "", err
	}
	// Window 0 uses session notes
	if windowIdx == 0 {
		return inst.Notes, nil
	}
	for _, fw := range inst.FollowedWindows {
		if fw.Index == windowIdx {
			return fw.Notes, nil
		}
	}
	return "", nil
}

// GetWindowList returns list of windows
func (a *App) GetWindowList(sessionID string) ([]session.WindowInfo, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, err
	}
	return inst.GetWindowList(), nil
}

// GetWindowAutoYes returns YOLO state for a specific window
func (a *App) GetWindowAutoYes(sessionID string, windowIdx int) (bool, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return false, err
	}
	// Window 0 uses session AutoYes
	if windowIdx == 0 {
		return inst.AutoYes, nil
	}
	for _, fw := range inst.FollowedWindows {
		if fw.Index == windowIdx {
			return fw.AutoYes, nil
		}
	}
	return false, nil
}

// SetWindowAutoYes sets YOLO state for a specific window
func (a *App) SetWindowAutoYes(sessionID string, windowIdx int, enabled bool) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	// Window 0 uses session AutoYes
	if windowIdx == 0 {
		inst.AutoYes = enabled
		return a.storage.UpdateInstance(inst)
	}
	for i := range inst.FollowedWindows {
		if inst.FollowedWindows[i].Index == windowIdx {
			inst.FollowedWindows[i].AutoYes = enabled
			return a.storage.UpdateInstance(inst)
		}
	}
	return fmt.Errorf("error.windowNotFound")
}

// GetExtraArgs returns the extra CLI arguments for a session window
func (a *App) GetExtraArgs(sessionID string, windowIdx int) (string, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return "", err
	}
	if windowIdx == 0 {
		return inst.ExtraArgs, nil
	}
	for _, fw := range inst.FollowedWindows {
		if fw.Index == windowIdx {
			return fw.ExtraArgs, nil
		}
	}
	return "", nil
}

// SetExtraArgs sets the extra CLI arguments for a session window
func (a *App) SetExtraArgs(sessionID string, windowIdx int, extraArgs string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	log.Printf("[SetExtraArgs] sessionID=%s windowIdx=%d newExtraArgs=%q", sessionID, windowIdx, extraArgs)
	if windowIdx == 0 {
		inst.ExtraArgs = extraArgs
		return a.storage.UpdateInstance(inst)
	}
	for i := range inst.FollowedWindows {
		if inst.FollowedWindows[i].Index == windowIdx {
			oldVal := inst.FollowedWindows[i].ExtraArgs
			inst.FollowedWindows[i].ExtraArgs = extraArgs
			log.Printf("[SetExtraArgs] tab %d: old=%q -> new=%q", windowIdx, oldVal, extraArgs)
			return a.storage.UpdateInstance(inst)
		}
	}
	return fmt.Errorf("error.windowNotFound")
}

// ============================================================================
// Preview & Activity
// ============================================================================

// PreviewData contains preview info
type PreviewData struct {
	Content  string `json:"content"`
	Activity string `json:"activity"`
}

// GetPreview returns session preview content
func (a *App) GetPreview(id string, lines int) (*PreviewData, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	content, _ := inst.GetPreview(lines)
	activity := inst.DetectActivity()

	activityStr := "idle"
	switch activity {
	case session.ActivityBusy:
		activityStr = "busy"
	case session.ActivityWaiting:
		activityStr = "waiting"
	}

	return &PreviewData{
		Content:  content,
		Activity: activityStr,
	}, nil
}

// GetLastLine returns status line for a session
func (a *App) GetLastLine(id string) (string, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return "", err
	}
	return inst.GetLastLine(), nil
}

// TabStatusInfo contains per-tab status information for multi-agent sessions
type TabStatusInfo struct {
	WindowIdx   int    `json:"windowIdx"`
	Agent       string `json:"agent"`
	Name        string `json:"name"`
	Activity    string `json:"activity"`
	StatusLine  string `json:"statusLine"`
	SpinnerText string `json:"spinnerText"`
	// Yolo: live bypass-permissions (real YOLO) state read from the pane status
	// bar, so the sidebar follows a Shift+Tab toggle inside Claude, not just the
	// stored launch flag. "auto mode" is NOT yolo and reports false here.
	Yolo bool `json:"yolo"`
	// HideStatusLine: per-tab user preference — the session list omits this
	// tab's status line row when set.
	HideStatusLine bool `json:"hideStatusLine"`
}

// SidebarUpdate contains combined activity and status line data
type SidebarUpdate struct {
	Activities   map[string]string          `json:"activities"`
	StatusLines  map[string]string          `json:"statusLines"`
	SpinnerTexts map[string]string          `json:"spinnerTexts"`
	TabStatuses  map[string][]TabStatusInfo `json:"tabStatuses"`
	observations []activityObservation
	projectID    string
}

// GetSidebarUpdates returns activity and status line data in one call (single LoadAll)
func (a *App) GetSidebarUpdates() SidebarUpdate {
	result := SidebarUpdate{
		Activities:   make(map[string]string),
		StatusLines:  make(map[string]string),
		SpinnerTexts: make(map[string]string),
		TabStatuses:  make(map[string][]TabStatusInfo),
	}

	projectID, instances, _, err := a.storage.LoadAllWithProjectSnapshot()
	if err != nil {
		return result
	}
	result.projectID = projectID

	// Phase 1: auto-detect + persist session IDs (sequential; touches storage).
	// Phase 2 (below) runs the tmux capture + detection in parallel across
	// sessions so total wall time scales with max-per-session work rather
	// than sum-per-session work.
	type detectJob struct {
		inst *session.Instance
	}
	var jobs []detectJob

	for _, inst := range instances {
		if inst.Status != session.StatusRunning {
			continue
		}

		// Auto-detect and persist Claude session ID from running process
		// so that resume works correctly after app/machine restart
		needSave := false
		if inst.ResumeSessionID == "" && inst.Agent == session.AgentClaude {
			if sid := getClaudeSessionIDFromTmux(inst.TmuxSessionName()); sid != "" {
				inst.ResumeSessionID = sid
				needSave = true
				log.Printf("[SidebarPoll] auto-detected and saved ResumeSessionID=%s for session=%s", sid, inst.ID)
			}
		}

		// Auto-detect Claude session ID for followed windows (tabs)
		for idx := range inst.FollowedWindows {
			fw := &inst.FollowedWindows[idx]
			if fw.ResumeSessionID == "" && fw.Agent == session.AgentClaude {
				if sid := getClaudeSessionIDFromTmuxWindow(inst.TmuxSessionName(), fw.Index); sid != "" {
					fw.ResumeSessionID = sid
					needSave = true
					log.Printf("[SidebarPoll] auto-detected Claude sessionID=%s for tab=%s/%d", sid, inst.ID, fw.Index)
				}
			}
		}

		// Auto-detect Gemini session ID from filesystem
		// Gemini creates session files at ~/.gemini/tmp/<hash>/chats/session-*.json on startup
		// Collect already-assigned Gemini IDs to avoid giving the same ID to multiple tabs
		var geminiExcludeIDs []string
		if inst.Agent == session.AgentGemini && inst.ResumeSessionID != "" {
			geminiExcludeIDs = append(geminiExcludeIDs, inst.ResumeSessionID)
		}
		for _, fw := range inst.FollowedWindows {
			if fw.Agent == session.AgentGemini && fw.ResumeSessionID != "" {
				geminiExcludeIDs = append(geminiExcludeIDs, fw.ResumeSessionID)
			}
		}

		if inst.ResumeSessionID == "" && inst.Agent == session.AgentGemini {
			if sid := session.DetectGeminiSessionID(inst.Path, geminiExcludeIDs...); sid != "" {
				inst.ResumeSessionID = sid
				geminiExcludeIDs = append(geminiExcludeIDs, sid)
				needSave = true
				log.Printf("[SidebarPoll] auto-detected Gemini sessionID=%s for session=%s", sid, inst.ID)
			}
		}

		// Auto-detect Gemini session ID for followed windows (tabs)
		for idx := range inst.FollowedWindows {
			fw := &inst.FollowedWindows[idx]
			if fw.ResumeSessionID == "" && fw.Agent == session.AgentGemini {
				if sid := session.DetectGeminiSessionID(inst.Path, geminiExcludeIDs...); sid != "" {
					fw.ResumeSessionID = sid
					geminiExcludeIDs = append(geminiExcludeIDs, sid)
					needSave = true
					log.Printf("[SidebarPoll] auto-detected Gemini sessionID=%s for tab=%s/%d", sid, inst.ID, fw.Index)
				}
			}
		}

		if needSave {
			if err := a.storage.UpdateInstanceForProject(projectID, inst); err != nil {
				log.Printf("[SidebarPoll] failed to save auto-detected session IDs for session=%s: %v", inst.ID, err)
			}
		}

		jobs = append(jobs, detectJob{inst: inst})
	}

	// Phase 2: run detection in parallel. isSpinnerAnimating() sleeps 60ms
	// between two tmux captures to decide if a spinner is still rotating —
	// that sleep serialised across many agents was the wall-time cost that
	// made 1s ticks infeasible. Doing it per-session concurrently keeps
	// total time roughly at the slowest single session.
	type sessionResult struct {
		instID       string
		activity     string
		statusLine   string
		spinnerText  string
		agentTabs    []TabStatusInfo
		observations []activityObservation
	}
	resultsCh := make(chan sessionResult, len(jobs))

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(inst *session.Instance) {
			defer wg.Done()

			mainAgent := inst.Agent
			if mainAgent == "" {
				mainAgent = session.AgentClaude
			}
			mainWindowIdx := inst.GetMainWindowIndex()

			type windowInfo struct {
				idx      int
				agent    session.AgentType
				name     string
				hideLine bool
			}
			windows := []windowInfo{{idx: mainWindowIdx, agent: mainAgent, name: inst.Name, hideLine: inst.HideStatusLine}}
			for _, fw := range inst.FollowedWindows {
				if fw.Index != mainWindowIdx && !fw.Stopped {
					name := fw.Name
					if name == "" {
						name = string(fw.Agent)
					}
					windows = append(windows, windowInfo{idx: fw.Index, agent: fw.Agent, name: name, hideLine: fw.HideStatusLine})
				}
			}

			var tabStatuses []TabStatusInfo
			validActivityWindows := make(map[int]bool)
			highestActivity := session.ActivityIdle
			bestWindowIdx := 0

			for wi, w := range windows {
				activity, activityValid := inst.DetectActivityForWindowWithValidity(w.idx)
				validActivityWindows[w.idx] = activityValid
				info := inst.GetStatusInfoForWindow(w.idx, w.agent)

				actStr := "idle"
				switch activity {
				case session.ActivityBusy:
					actStr = "busy"
				case session.ActivityWaiting:
					actStr = "waiting"
				}

				line := session.StripANSI(info.StatusLine)
				line = strings.ReplaceAll(line, "\n", " ")
				line = strings.ReplaceAll(line, "\r", "")
				line = strings.TrimSpace(line)
				if len(line) > 100 {
					line = line[:97] + "..."
				}

				tabStatuses = append(tabStatuses, TabStatusInfo{
					WindowIdx:      w.idx,
					Agent:          string(w.agent),
					Name:           w.name,
					Activity:       actStr,
					StatusLine:     line,
					SpinnerText:    info.SpinnerText,
					Yolo:           inst.DetectYoloForWindow(w.idx),
					HideStatusLine: w.hideLine,
				})

				if activity == session.ActivityWaiting {
					highestActivity = session.ActivityWaiting
					bestWindowIdx = wi
				} else if activity == session.ActivityBusy && highestActivity != session.ActivityWaiting {
					highestActivity = session.ActivityBusy
					bestWindowIdx = wi
				}
			}

			activityStr := "idle"
			switch highestActivity {
			case session.ActivityBusy:
				activityStr = "busy"
			case session.ActivityWaiting:
				activityStr = "waiting"
			}

			sr := sessionResult{instID: inst.ID, activity: activityStr}
			if len(tabStatuses) > 0 {
				best := tabStatuses[bestWindowIdx]
				sr.statusLine = best.StatusLine
				sr.spinnerText = best.SpinnerText
			}
			for _, ts := range tabStatuses {
				if ts.Agent != string(session.AgentTerminal) {
					sr.agentTabs = append(sr.agentTabs, ts)
					if validActivityWindows[ts.WindowIdx] {
						sr.observations = append(sr.observations, activityObservation{
							SessionID:   inst.ID,
							SessionName: inst.Name,
							WindowIdx:   ts.WindowIdx,
							TabName:     ts.Name,
							Agent:       ts.Agent,
							Activity:    ts.Activity,
						})
					}
				}
			}
			resultsCh <- sr
		}(job.inst)
	}
	wg.Wait()
	close(resultsCh)

	for sr := range resultsCh {
		result.Activities[sr.instID] = sr.activity
		if sr.statusLine != "" {
			result.StatusLines[sr.instID] = sr.statusLine
		}
		if sr.spinnerText != "" {
			result.SpinnerTexts[sr.instID] = sr.spinnerText
		}
		if len(sr.agentTabs) > 1 {
			result.TabStatuses[sr.instID] = sr.agentTabs
		}
		result.observations = append(result.observations, sr.observations...)
	}

	return result
}

// GetActivities returns activity status for all running sessions
func (a *App) GetActivities() map[string]string {
	return a.GetSidebarUpdates().Activities
}

// GetStatusLines returns the last output line for all running sessions
func (a *App) GetStatusLines() map[string]string {
	return a.GetSidebarUpdates().StatusLines
}

// startPreviewPolling runs a background goroutine that polls sidebar updates
// and emits events to the frontend. This avoids blocking the JS main thread.
func (a *App) startPreviewPolling(ctx context.Context) {
	const typingCooldown = 1500 * time.Millisecond

	// 1s tick feels "live" in the sidebar. GetSidebarUpdates parallelises
	// per-session capture/detect work, so 1Hz scales even with many running
	// agents.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Skip polling while user is actively typing
			lastTyping := atomic.LoadInt64(&a.lastTypingSignal)
			if lastTyping > 0 && time.Since(time.Unix(0, lastTyping)) < typingCooldown {
				continue
			}

			data := a.GetSidebarUpdates()
			// Drop a completed snapshot if the user switched projects while
			// tmux captures were running. The snapshot itself carries the ID
			// captured atomically with its instance list, so A→B→A is safe.
			if data.projectID != a.storage.GetActiveProjectID() {
				continue
			}
			if a.activityStats != nil {
				a.activityStats.Observe(data.projectID, time.Now(), data.observations)
			}
			if isDevMode {
				log.Printf("[SidebarEmit] activities=%v", data.Activities)
			}
			runtime.EventsEmit(a.ctx, "sidebar:update", data)

		case <-ctx.Done():
			return
		}
	}
}

// GetTerminalWSPort returns the WebSocket terminal server port
func (a *App) GetTerminalWSPort() int {
	if a.termServer != nil {
		return a.termServer.GetPort()
	}
	return 9753
}

// GetTerminalWSToken returns the per-launch auth token the frontend must
// include when opening the terminal WebSocket. Empty if the server isn't up.
func (a *App) GetTerminalWSToken() string {
	if a.termServer != nil {
		return a.termServer.AuthToken()
	}
	return ""
}

// ============================================================================
// Terminal (PTY) Integration
// ============================================================================

// AttachSession attaches to a session terminal
func (a *App) AttachSession(id string, windowIdx int) (string, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return "", err
	}

	// Create PTY ID
	ptyID := fmt.Sprintf("%s-%d", id, windowIdx)

	// Serialize check-and-create for a PTY ID. Holding the write lock until the
	// process is registered prevents two concurrent attaches from leaking one
	// of two tmux children under the same map key.
	a.ptyMu.Lock()
	if _, exists := a.ptys[ptyID]; exists {
		a.ptyMu.Unlock()
		return ptyID, nil
	}

	// Get tmux session name
	tmuxSession := inst.TmuxSessionName()

	// Start tmux attach command with PTY
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", fmt.Sprintf("%s:%d", tmuxSession, windowIdx))

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		a.ptyMu.Unlock()
		return "", fmt.Errorf("failed to start PTY: %w", err)
	}

	ps := &ptySession{
		ptmx:     ptmx,
		cmd:      cmd,
		session:  inst,
		windowID: windowIdx,
		cancel:   cancel,
	}

	a.ptys[ptyID] = ps
	a.ptyMu.Unlock()

	// Start reading PTY output
	go a.readPTY(ptyID, ptmx)

	return ptyID, nil
}

// readPTY reads from PTY and emits events with batching for performance
func (a *App) readPTY(ptyID string, ptmx *os.File) {
	buf := make([]byte, 32768) // Larger buffer
	eventName := "pty:output:" + ptyID

	for {
		n, err := ptmx.Read(buf)
		if err != nil {
			if err != io.EOF {
				runtime.LogError(a.ctx, fmt.Sprintf("PTY read error: %v", err))
			}
			break
		}
		if n > 0 {
			// Emit immediately - let frontend batch the rendering
			runtime.EventsEmit(a.ctx, eventName, string(buf[:n]))
		}
	}
	// Cleanup
	a.DetachSession(ptyID)
}

// DetachSession detaches from a session terminal
func (a *App) DetachSession(ptyID string) error {
	a.ptyMu.Lock()
	defer a.ptyMu.Unlock()

	ps, exists := a.ptys[ptyID]
	if !exists {
		return nil
	}

	if ps.cancel != nil {
		ps.cancel()
	}
	if ps.ptmx != nil {
		ps.ptmx.Close()
	}
	// Reap the process to avoid zombies
	if ps.cmd != nil && ps.cmd.Process != nil {
		go func(c *exec.Cmd) { _ = c.Wait() }(ps.cmd)
	}
	delete(a.ptys, ptyID)

	runtime.EventsEmit(a.ctx, "pty:closed:"+ptyID, nil)
	return nil
}

// SendInput sends input to PTY
func (a *App) SendInput(ptyID string, data string) error {
	a.ptyMu.RLock()
	ps, exists := a.ptys[ptyID]
	a.ptyMu.RUnlock()

	if !exists || ps.ptmx == nil {
		return fmt.Errorf("error.ptyNotFound")
	}

	_, err := ps.ptmx.WriteString(data)
	return err
}

// ResizeTerminal resizes PTY and refreshes tmux
func (a *App) ResizeTerminal(ptyID string, cols, rows int) error {
	a.ptyMu.RLock()
	ps, exists := a.ptys[ptyID]
	a.ptyMu.RUnlock()

	if !exists {
		return fmt.Errorf("error.ptyNotFound")
	}

	// Resize PTY
	err := pty.Setsize(ps.ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return err
	}

	// Force tmux to resize window and refresh
	if ps.session != nil {
		sessionName := ps.session.TmuxSessionName()
		target := fmt.Sprintf("%s:%d", sessionName, ps.windowID)
		go func() {
			// Resize the specific window to fit the largest client
			exec.Command("tmux", "resize-window", "-t", target, "-A").Run()
			// Also refresh all clients
			exec.Command("tmux", "refresh-client", "-t", sessionName).Run()
		}()
	}

	return nil
}

// RefreshWindow forces tmux to redraw the pane for the given session window.
// Fixes occasional rendering glitches (garbled characters) by sending Ctrl+L
// to the pane and refreshing all clients attached to the tmux session.
func (a *App) RefreshWindow(sessionID string, windowIdx int) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if !inst.IsAlive() {
		return fmt.Errorf("error.sessionNotRunning")
	}

	sessionName := inst.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Clear the pane's screen buffer and resize to match attached clients.
	// send-keys C-l clears the screen (equivalent to "clear" in most shells/TUIs).
	// Many TUI apps (Claude, Codex, etc.) redraw their UI on SIGWINCH/clear.
	_ = exec.Command("tmux", "send-keys", "-t", target, "C-l").Run()
	_ = exec.Command("tmux", "resize-window", "-t", target, "-A").Run()
	_ = exec.Command("tmux", "refresh-client", "-t", sessionName).Run()

	return nil
}

// ============================================================================
// Diff View
// ============================================================================

// DiffData contains diff info
type DiffData struct {
	Content string `json:"content"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// GetSessionDiff returns git diff since session start
func (a *App) GetSessionDiff(id string) (*DiffData, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	diff := inst.GetSessionDiff()
	return &DiffData{
		Content: diff.Content,
		Added:   diff.Added,
		Removed: diff.Removed,
	}, nil
}

// GetFullDiff returns full uncommitted diff for path
func (a *App) GetFullDiff(id string) (*DiffData, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	diff := inst.GetFullDiff()
	return &DiffData{
		Content: diff.Content,
		Added:   diff.Added,
		Removed: diff.Removed,
	}, nil
}

// ============================================================================
// Global History Search
// ============================================================================

// HistoryEntryInfo represents history entry for frontend
type HistoryEntryInfo struct {
	Agent       string `json:"agent"`
	Content     string `json:"content"`
	SessionFile string `json:"sessionFile"`
	SessionID   string `json:"sessionId"`
	Score       int    `json:"score"`
}

// InitHistorySearch initializes history search index
func (a *App) InitHistorySearch() error {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return err
	}

	index := session.NewHistoryIndex()
	index.SetInstances(instances)
	if err := index.Load(); err != nil {
		return err
	}
	a.historyIndex = index
	return nil
}

// GlobalSearch searches history
func (a *App) GlobalSearch(query string) ([]HistoryEntryInfo, error) {
	if a.historyIndex == nil {
		if err := a.InitHistorySearch(); err != nil {
			return nil, err
		}
	}

	results := a.historyIndex.Search(query)
	infos := make([]HistoryEntryInfo, len(results))
	for i, r := range results {
		infos[i] = HistoryEntryInfo{
			Agent:       string(r.Agent),
			Content:     r.Content,
			SessionFile: r.SessionFile,
			SessionID:   r.SessionID,
			Score:       r.Score,
		}
	}
	return infos, nil
}

// GetHistoryPreview loads conversation preview
func (a *App) GetHistoryPreview(entry HistoryEntryInfo) (string, error) {
	// Create a HistoryEntry and load its conversation
	he := &session.HistoryEntry{
		Agent:       session.AgentType(entry.Agent),
		SessionFile: entry.SessionFile,
		SessionID:   entry.SessionID,
		Content:     entry.Content,
	}
	messages, err := he.LoadConversation()
	if err != nil {
		return "", err
	}

	// Format messages as string
	var result strings.Builder
	for _, msg := range messages {
		result.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}
	return result.String(), nil
}

// ============================================================================
// Resume Sessions
// ============================================================================

// AgentSessionInfo represents an agent session for resume
type AgentSessionInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Path        string `json:"path"`
	Timestamp   string `json:"timestamp"`
}

// GetResumeSessions returns available sessions for resume
func (a *App) GetResumeSessions(agent string, path string) ([]AgentSessionInfo, error) {
	var sessions []session.AgentSession
	var err error

	switch session.AgentType(agent) {
	case session.AgentClaude:
		sessions, err = session.ListAgentSessionsByHistory(path)
	case session.AgentGemini:
		sessions, err = session.ListGeminiSessions(path)
	case session.AgentCodex:
		sessions, err = session.ListCodexSessions(path)
	case session.AgentOpenCode:
		sessions, err = session.ListOpenCodeSessions(path)
	case session.AgentAmazonQ:
		sessions, err = session.ListAmazonQSessions(path)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	result := make([]AgentSessionInfo, len(sessions))
	for i, s := range sessions {
		// Create display name from first prompt
		displayName := s.FirstPrompt
		if len(displayName) > 50 {
			displayName = displayName[:50] + "..."
		}
		if displayName == "" {
			displayName = s.SessionID[:8] + "..."
		}

		result[i] = AgentSessionInfo{
			ID:          s.SessionID,
			DisplayName: displayName,
			Path:        path,
			Timestamp:   s.UpdatedAt.Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

// ============================================================================
// Settings
// ============================================================================

// SettingsInfo represents settings for frontend
type SettingsInfo struct {
	CompactList      bool   `json:"compactList"`
	HideStatusLines  bool   `json:"hideStatusLines"`
	ShowAgentIcons   bool   `json:"showAgentIcons"`
	SplitView        bool   `json:"splitView"`
	MarkedSessionID  string `json:"markedSessionId"`
	Language         string `json:"language"`
	TerminalRenderer string `json:"terminalRenderer"`
	NotifyOnWaiting  bool   `json:"notifyOnWaiting"`
	NotifyDesktop    bool   `json:"notifyDesktop"`
	NotifyNtfy       bool   `json:"notifyNtfy"`
	NtfyURL          string `json:"ntfyUrl"`
}

// GetSettings returns UI settings
func (a *App) GetSettings() (*SettingsInfo, error) {
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = &session.Settings{}
	}

	// Language fallback so the i18n loader doesn't see an empty string.
	lang := settings.Language
	if lang == "" {
		lang = "en"
	}

	// Default the terminal renderer to canvas if unset.
	renderer := settings.TerminalRenderer
	if renderer == "" {
		renderer = "canvas"
	}

	return &SettingsInfo{
		CompactList:      settings.CompactList,
		HideStatusLines:  settings.HideStatusLines,
		ShowAgentIcons:   settings.ShowAgentIcons,
		SplitView:        settings.SplitView,
		MarkedSessionID:  settings.MarkedSessionID,
		Language:         lang,
		TerminalRenderer: renderer,
		NotifyOnWaiting:  settings.NotifyOnWaiting,
		NotifyDesktop:    settings.NotifyDesktop,
		NotifyNtfy:       settings.NotifyNtfy,
		NtfyURL:          settings.NtfyURL,
	}, nil
}

// SaveSettings saves UI settings
func (a *App) SaveSettings(settings SettingsInfo) error {
	return a.storage.SaveSettings(&session.Settings{
		CompactList:      settings.CompactList,
		HideStatusLines:  settings.HideStatusLines,
		ShowAgentIcons:   settings.ShowAgentIcons,
		SplitView:        settings.SplitView,
		MarkedSessionID:  settings.MarkedSessionID,
		Language:         settings.Language,
		TerminalRenderer: settings.TerminalRenderer,
		NotifyOnWaiting:  settings.NotifyOnWaiting,
		NotifyDesktop:    settings.NotifyDesktop,
		NotifyNtfy:       settings.NotifyNtfy,
		NtfyURL:          settings.NtfyURL,
	})
}

// ============================================================================
// Updater
// ============================================================================

// UpdateInfo contains update information
type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
}

// GetVersion returns the current application version (for the UI/about).
func (a *App) GetVersion() string {
	return Version
}

// SetTabStatusLineVisibility stores whether a tab's status line should be
// shown in the session list. windowIdx 0 (the main window) is stored on the
// instance; followed windows carry their own flag.
func (a *App) SetTabStatusLineVisibility(sessionID string, windowIdx int, hide bool) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		inst.HideStatusLine = hide
	} else {
		found := false
		for i := range inst.FollowedWindows {
			if inst.FollowedWindows[i].Index == windowIdx {
				inst.FollowedWindows[i].HideStatusLine = hide
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("window %d not found", windowIdx)
		}
	}
	return a.storage.UpdateInstance(inst)
}

// QuickReplyTab sends one whitelisted answer key to a session window so the
// user can respond to a waiting agent prompt straight from the attention
// inbox, without switching tabs. The whitelist keeps arbitrary key injection
// out of the bound API surface.
func (a *App) QuickReplyTab(sessionID string, windowIdx int, action string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	var keys []string
	switch action {
	case "enter":
		keys = []string{"Enter"}
	case "esc":
		keys = []string{"Escape"}
	case "y":
		keys = []string{"y", "Enter"}
	case "n":
		keys = []string{"n", "Enter"}
	case "1", "2", "3":
		keys = []string{action}
	default:
		return fmt.Errorf("unsupported quick-reply action %q", action)
	}
	for _, k := range keys {
		if err := inst.SendKeysToWindow(windowIdx, k); err != nil {
			return err
		}
	}
	return nil
}

// LogFrontend drops a frontend message into the app's log file. The packaged
// build has no devtools console, so frontend-side failures (terminal pool /
// WebSocket attach errors) were invisible — this makes them diagnosable from
// ~/.config/agent-session-manager-desktop/asmgr-desktop.log.
func (a *App) LogFrontend(msg string) {
	log.Printf("[frontend] %s", msg)
}

// CheckForUpdate checks for updates
func (a *App) CheckForUpdate() *UpdateInfo {
	current := Version
	latest := updater.CheckForUpdate(current)

	return &UpdateInfo{
		Available:      latest != "",
		CurrentVersion: current,
		LatestVersion:  latest,
	}
}

// PerformUpdate downloads and installs update
func (a *App) PerformUpdate(version string) error {
	return updater.DownloadAndInstall(version)
}

// ============================================================================
// Agent Info
// ============================================================================

// AgentInfo represents agent configuration
type AgentInfo struct {
	Type            string `json:"type"`
	Name            string `json:"name"`
	Icon            string `json:"icon"`
	SupportsResume  bool   `json:"supportsResume"`
	SupportsAutoYes bool   `json:"supportsAutoYes"`
	SupportsFork    bool   `json:"supportsFork"`
}

// GetAgents returns available agents
func (a *App) GetAgents() []AgentInfo {
	return []AgentInfo{
		{Type: "claude", Name: "Claude", Icon: "🤖", SupportsResume: true, SupportsAutoYes: true, SupportsFork: true},
		{Type: "gemini", Name: "Gemini", Icon: "💎", SupportsResume: true, SupportsAutoYes: false, SupportsFork: false},
		{Type: "aider", Name: "Aider", Icon: "🔧", SupportsResume: false, SupportsAutoYes: true, SupportsFork: false},
		{Type: "codex", Name: "Codex", Icon: "📦", SupportsResume: true, SupportsAutoYes: true, SupportsFork: false},
		{Type: "amazonq", Name: "Amazon Q", Icon: "🦜", SupportsResume: true, SupportsAutoYes: true, SupportsFork: false},
		{Type: "opencode", Name: "OpenCode", Icon: "💻", SupportsResume: true, SupportsAutoYes: false, SupportsFork: false},
		{Type: "custom", Name: "Custom", Icon: "⚙️", SupportsResume: false, SupportsAutoYes: false, SupportsFork: false},
		{Type: "terminal", Name: "Terminal", Icon: "🖥️", SupportsResume: false, SupportsAutoYes: false, SupportsFork: false},
	}
}

// ============================================================================
// Task Management
// ============================================================================

// TaskInfo represents a task for the frontend
type TaskInfo struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`
	Priority     string        `json:"priority"`
	Tags         []string      `json:"tags"`
	Subtasks     []SubtaskInfo `json:"subtasks"`
	Dependencies []string      `json:"dependencies"`
	CreatedAt    string        `json:"createdAt"`
	UpdatedAt    string        `json:"updatedAt"`
	CompletedAt  *string       `json:"completedAt,omitempty"`
}

// SubtaskInfo represents a subtask for the frontend
type SubtaskInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"createdAt"`
}

// taskManagerCache caches task managers per project path
var taskManagerCache = make(map[string]*session.TaskManager)
var taskManagerMu sync.RWMutex

// getTaskManager returns or creates a task manager for a session's project path
func (a *App) getTaskManager(sessionID string) (*session.TaskManager, error) {
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	projectPath := sess.Path
	if projectPath == "" {
		return nil, fmt.Errorf("error.sessionNoPath")
	}

	taskManagerMu.RLock()
	if tm, ok := taskManagerCache[projectPath]; ok {
		taskManagerMu.RUnlock()
		return tm, nil
	}
	taskManagerMu.RUnlock()

	taskManagerMu.Lock()
	defer taskManagerMu.Unlock()

	// Double-check after acquiring write lock
	if tm, ok := taskManagerCache[projectPath]; ok {
		return tm, nil
	}

	tm := session.NewTaskManager(projectPath)
	if err := tm.Load(); err != nil {
		return nil, fmt.Errorf("failed to load tasks: %w", err)
	}

	taskManagerCache[projectPath] = tm
	return tm, nil
}

// convertTask converts session.Task to TaskInfo for frontend
func convertTask(t session.Task) TaskInfo {
	subtasks := make([]SubtaskInfo, len(t.Subtasks))
	for i, st := range t.Subtasks {
		subtasks[i] = SubtaskInfo{
			ID:        st.ID,
			Title:     st.Title,
			Done:      st.Done,
			CreatedAt: st.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}

	deps := t.Dependencies
	if deps == nil {
		deps = []string{}
	}

	info := TaskInfo{
		ID:           t.ID,
		Title:        t.Title,
		Description:  t.Description,
		Status:       string(t.Status),
		Priority:     string(t.Priority),
		Tags:         tags,
		Subtasks:     subtasks,
		Dependencies: deps,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.CompletedAt != nil {
		completedStr := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		info.CompletedAt = &completedStr
	}

	return info
}

// GetTasks returns all tasks for a session's project
func (a *App) GetTasks(sessionID string) ([]TaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	tasks := tm.GetTasks()
	result := make([]TaskInfo, len(tasks))
	for i, t := range tasks {
		result[i] = convertTask(t)
	}

	return result, nil
}

// GetTasksByStatus returns tasks filtered by status
func (a *App) GetTasksByStatus(sessionID string, status string) ([]TaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	tasks := tm.GetTasksByStatus(session.TaskStatus(status))
	result := make([]TaskInfo, len(tasks))
	for i, t := range tasks {
		result[i] = convertTask(t)
	}

	return result, nil
}

// CreateTask creates a new task
func (a *App) CreateTask(sessionID, title, description, priority string, tags []string) (*TaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	if tags == nil {
		tags = []string{}
	}

	task, err := tm.CreateTask(title, description, session.TaskPriority(priority), tags)
	if err != nil {
		return nil, err
	}

	info := convertTask(*task)
	return &info, nil
}

// UpdateTask updates an existing task
func (a *App) UpdateTask(sessionID, taskID string, updates map[string]interface{}) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateTask(taskID, updates)
}

// DeleteTask deletes a task
func (a *App) DeleteTask(sessionID, taskID string) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.DeleteTask(taskID)
}

// MoveTask changes the status of a task
func (a *App) MoveTask(sessionID, taskID, newStatus string) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.MoveTask(taskID, session.TaskStatus(newStatus))
}

// AddSubtask adds a subtask to a task
func (a *App) AddSubtask(sessionID, taskID, title string) (*SubtaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	subtask, err := tm.AddSubtask(taskID, title)
	if err != nil {
		return nil, err
	}

	return &SubtaskInfo{
		ID:        subtask.ID,
		Title:     subtask.Title,
		Done:      subtask.Done,
		CreatedAt: subtask.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// ToggleSubtask toggles the done status of a subtask
func (a *App) ToggleSubtask(sessionID, taskID, subtaskID string) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.ToggleSubtask(taskID, subtaskID)
}

// DeleteSubtask removes a subtask
func (a *App) DeleteSubtask(sessionID, taskID, subtaskID string) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.DeleteSubtask(taskID, subtaskID)
}

// GetNextTask returns the next recommended task to work on
func (a *App) GetNextTask(sessionID string) (*TaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	task := tm.GetNextTask()
	if task == nil {
		return nil, nil
	}

	info := convertTask(*task)
	return &info, nil
}

// SendTaskToAgent sends a task as a prompt to the active agent
func (a *App) SendTaskToAgent(sessionID, taskID string) error {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	prompt, err := tm.FormatTaskForAgent(taskID)
	if err != nil {
		return err
	}

	log.Printf("[TaskManager] SendToAgent taskID=%s prompt=%q", taskID, prompt)
	// Send the prompt to the active terminal
	return a.SendPrompt(sessionID, prompt)
}

// ============================================================================
// Task Master MCP Integration
// ============================================================================

// taskMasterCache stores TaskMaster instances per project
var taskMasterCache = make(map[string]*mcp.TaskMaster)
var taskMasterMu sync.RWMutex

// getTaskMasterMCP returns or creates a TaskMaster MCP client for a project
func (a *App) getTaskMasterMCP(sessionID string) (*mcp.TaskMaster, error) {
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	projectPath := sess.Path
	if projectPath == "" {
		return nil, fmt.Errorf("error.sessionNoPath")
	}

	taskMasterMu.RLock()
	tm, ok := taskMasterCache[projectPath]
	taskMasterMu.RUnlock()

	if ok && tm.IsRunning() {
		return tm, nil
	}

	taskMasterMu.Lock()
	defer taskMasterMu.Unlock()

	// Double-check after acquiring write lock
	if tm, ok := taskMasterCache[projectPath]; ok && tm.IsRunning() {
		return tm, nil
	}
	if stale, ok := taskMasterCache[projectPath]; ok {
		_ = stale.Stop()
		delete(taskMasterCache, projectPath)
	}

	// Uses Claude Code provider - no API key required
	tm = mcp.NewTaskMaster(projectPath)
	if err := tm.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Task Master: %w", err)
	}

	taskMasterCache[projectPath] = tm
	return tm, nil
}

// MCPTaskInfo represents a Task Master task for the frontend
type MCPTaskInfo struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Description  string           `json:"description"`
	Status       string           `json:"status"`
	Priority     string           `json:"priority"`
	Tags         []string         `json:"tags"`
	Subtasks     []MCPSubtaskInfo `json:"subtasks"`
	Dependencies []string         `json:"dependencies"`
	Complexity   *int             `json:"complexity,omitempty"`
	Details      string           `json:"details,omitempty"`
	CreatedAt    string           `json:"createdAt,omitempty"`
}

// MCPSubtaskInfo represents a subtask for the frontend
type MCPSubtaskInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Details     string `json:"details,omitempty"`
}

// convertMCPTask converts mcp.Task to MCPTaskInfo
func convertMCPTask(t mcp.Task) MCPTaskInfo {
	subtasks := make([]MCPSubtaskInfo, len(t.Subtasks))
	for i, st := range t.Subtasks {
		subtasks[i] = MCPSubtaskInfo{
			ID:          st.ID,
			Title:       st.Title,
			Description: st.Description,
			Status:      st.Status,
			Details:     st.Details,
		}
	}

	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}

	deps := t.Dependencies
	if deps == nil {
		deps = []string{}
	}

	return MCPTaskInfo{
		ID:           t.ID,
		Title:        t.Title,
		Description:  t.Description,
		Status:       t.Status,
		Priority:     t.Priority,
		Tags:         tags,
		Subtasks:     subtasks,
		Dependencies: deps,
		Complexity:   t.Complexity,
		Details:      t.Details,
		CreatedAt:    t.CreatedAt,
	}
}

// TaskMasterStatus returns the status of Task Master for a session
func (a *App) TaskMasterStatus(sessionID string) map[string]interface{} {
	result := map[string]interface{}{
		"initialized": false,
		"running":     false,
		"error":       nil,
	}

	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		result["error"] = err.Error()
		return result
	}

	result["running"] = tm.IsRunning()
	result["initialized"] = true
	result["tools"] = len(tm.GetTools())

	return result
}

// TaskMasterInit initializes Task Master for a project
func (a *App) TaskMasterInit(sessionID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.InitializeProject(false)
}

// TaskMasterParsePRD parses a PRD file into tasks
func (a *App) TaskMasterParsePRD(sessionID, prdContent string, numTasks int) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	sess, _ := a.storage.GetInstance(sessionID)
	if sess == nil {
		return fmt.Errorf("session not found")
	}

	// Write PRD content to file
	prdPath := sess.Path + "/.taskmaster/docs/prd.md"
	if err := os.MkdirAll(sess.Path+"/.taskmaster/docs", 0755); err != nil {
		return fmt.Errorf("failed to create docs directory: %w", err)
	}

	if err := os.WriteFile(prdPath, []byte(prdContent), 0644); err != nil {
		return fmt.Errorf("failed to write PRD file: %w", err)
	}

	return tm.ParsePRD(prdPath, numTasks, true)
}

// TaskMasterGetTasks returns all tasks from Task Master
func (a *App) TaskMasterGetTasks(sessionID, status string) ([]MCPTaskInfo, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return nil, err
	}

	response, err := tm.GetTasks(status, true)
	if err != nil {
		return nil, err
	}

	result := make([]MCPTaskInfo, len(response.Tasks))
	for i, t := range response.Tasks {
		result[i] = convertMCPTask(t)
	}

	return result, nil
}

// TaskMasterGetTask returns a specific task
func (a *App) TaskMasterGetTask(sessionID, taskID string) (*MCPTaskInfo, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return nil, err
	}

	task, err := tm.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	info := convertMCPTask(*task)
	return &info, nil
}

// TaskMasterNextTask returns the next task to work on
func (a *App) TaskMasterNextTask(sessionID string) (*MCPTaskInfo, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return nil, err
	}

	task, err := tm.NextTask()
	if err != nil {
		return nil, err
	}

	if task == nil {
		return nil, nil
	}

	info := convertMCPTask(*task)
	return &info, nil
}

// TaskMasterSetStatus sets the status of a task
func (a *App) TaskMasterSetStatus(sessionID, taskID, status string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.SetTaskStatus(taskID, status)
}

// TaskMasterAddTask adds a new task
func (a *App) TaskMasterAddTask(sessionID, prompt string, research bool, priority string) (*MCPTaskInfo, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return nil, err
	}

	task, err := tm.AddTask(prompt, research, priority, nil)
	if err != nil {
		return nil, err
	}

	if task == nil {
		// Reload tasks to get the new one
		return nil, nil
	}

	info := convertMCPTask(*task)
	info.CreatedAt = time.Now().Format(time.RFC3339)
	return &info, nil
}

// TaskMasterAddManualTask adds a new task without AI (manual mode)
func (a *App) TaskMasterAddManualTask(sessionID, title, description, details, priority string) (*MCPTaskInfo, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return nil, err
	}

	task, err := tm.AddManualTask(title, description, details, priority, nil)
	if err != nil {
		return nil, err
	}

	if task == nil {
		// Reload tasks to get the new one
		return nil, nil
	}

	info := convertMCPTask(*task)
	info.CreatedAt = time.Now().Format(time.RFC3339)
	return &info, nil
}

// TaskMasterUpdateTask updates a task
func (a *App) TaskMasterUpdateTask(sessionID, taskID, prompt string, research bool) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateTask(taskID, prompt, research)
}

// TaskMasterUpdateSubtask updates a subtask with notes
func (a *App) TaskMasterUpdateSubtask(sessionID, subtaskID, prompt string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateSubtask(subtaskID, prompt)
}

// TaskMasterExpandTask expands a task into subtasks
func (a *App) TaskMasterExpandTask(sessionID, taskID string, research, force bool) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ExpandTask(taskID, research, force, 0)
}

// TaskMasterExpandAll expands all eligible tasks
func (a *App) TaskMasterExpandAll(sessionID string, research bool) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ExpandAllTasks(research, false)
}

// TaskMasterAnalyzeComplexity analyzes task complexity
func (a *App) TaskMasterAnalyzeComplexity(sessionID string, research bool) (string, error) {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return "", err
	}

	report, err := tm.AnalyzeComplexity(research)
	if err != nil {
		return "", err
	}

	return report.Summary, nil
}

// TaskMasterRemoveTask removes a task
func (a *App) TaskMasterRemoveTask(sessionID, taskID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveTask(taskID)
}

// TaskMasterSendToAgent sends a task as a prompt to the agent
func (a *App) TaskMasterSendToAgent(sessionID, taskID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	task, err := tm.GetTask(taskID)
	if err != nil {
		return err
	}

	log.Printf("[TaskMaster] SendToAgent taskID=%s task=%+v", taskID, task)

	prompt := mcp.FormatTaskForPrompt(task)
	log.Printf("[TaskMaster] Prompt to send: %q", prompt)
	return a.SendPrompt(sessionID, prompt)
}

// StopTaskMaster stops the Task Master MCP server for a project
func (a *App) StopTaskMaster(sessionID string) error {
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}

	taskMasterMu.Lock()
	defer taskMasterMu.Unlock()

	if tm, ok := taskMasterCache[sess.Path]; ok {
		tm.Stop()
		delete(taskMasterCache, sess.Path)
	}

	return nil
}

// TaskMasterAddSubtask adds a subtask to a task
func (a *App) TaskMasterAddSubtask(sessionID, taskID, title, description string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	_, err = tm.AddSubtask(taskID, title, description)
	return err
}

// TaskMasterRemoveSubtask removes a specific subtask
func (a *App) TaskMasterRemoveSubtask(sessionID, subtaskID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveSubtask(subtaskID)
}

// TaskMasterClearSubtasks removes all subtasks from a task
func (a *App) TaskMasterClearSubtasks(sessionID, taskID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ClearSubtasks(taskID)
}

// TaskMasterSetSubtaskStatus sets the status of a subtask
func (a *App) TaskMasterSetSubtaskStatus(sessionID, subtaskID, status string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.SetSubtaskStatus(subtaskID, status)
}

// TaskMasterUpdateTaskDirect updates a task with direct field values (no AI).
// Modifies the tasks.json file directly instead of using MCP to avoid slow AI calls.
func (a *App) TaskMasterUpdateTaskDirect(sessionID, taskID, title, description, details, priority string) error {
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	projectPath := sess.Path
	if projectPath == "" {
		return fmt.Errorf("error.sessionNoPath")
	}

	tasksFile := filepath.Join(projectPath, ".taskmaster", "tasks", "tasks.json")

	data, err := os.ReadFile(tasksFile)
	if err != nil {
		return fmt.Errorf("failed to read tasks.json: %w", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to parse tasks.json: %w", err)
	}

	// Find and update the task in each context
	updated := false
	for ctxKey, ctxRaw := range root {
		var ctx struct {
			Tasks    []map[string]interface{} `json:"tasks"`
			Metadata json.RawMessage          `json:"metadata,omitempty"`
		}
		// Try parsing as context with tasks array
		if err := json.Unmarshal(ctxRaw, &ctx); err != nil || ctx.Tasks == nil {
			continue
		}

		for i, task := range ctx.Tasks {
			tid := fmt.Sprintf("%v", task["id"])
			if tid == taskID {
				if title != "" {
					ctx.Tasks[i]["title"] = title
				}
				if description != "" {
					ctx.Tasks[i]["description"] = description
				}
				if details != "" {
					ctx.Tasks[i]["details"] = details
				}
				if priority != "" {
					ctx.Tasks[i]["priority"] = priority
				}
				updated = true
				break
			}
		}

		if updated {
			ctxBytes, err := json.Marshal(ctx)
			if err != nil {
				return fmt.Errorf("failed to marshal context: %w", err)
			}
			root[ctxKey] = ctxBytes
			break
		}
	}

	if !updated {
		return fmt.Errorf("task %s not found in tasks.json", taskID)
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks.json: %w", err)
	}

	if err := os.WriteFile(tasksFile, out, 0644); err != nil {
		return fmt.Errorf("failed to write tasks.json: %w", err)
	}

	return nil
}

// TaskMasterAddDependency adds a dependency to a task
func (a *App) TaskMasterAddDependency(sessionID, taskID, dependsOnID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.AddDependency(taskID, dependsOnID)
}

// TaskMasterRemoveDependency removes a dependency from a task
func (a *App) TaskMasterRemoveDependency(sessionID, taskID, dependsOnID string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveDependency(taskID, dependsOnID)
}
