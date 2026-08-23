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

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds the application state
type App struct {
	ctx                 context.Context
	storage             *session.Storage
	historyIndex        *session.HistoryIndex
	historyMu           sync.Mutex
	portableImportMu    sync.Mutex
	portableImports     map[string]portableImportSnapshot
	portableImportIDs   []string
	ptys                map[string]*ptySession
	ptyMu               sync.RWMutex
	ptyDrainDone        chan struct{}
	projectMu           sync.RWMutex
	projectMutationMu   sync.Mutex
	projectTransitionMu sync.Mutex
	projectGateMu       sync.Mutex
	projectSwitching    bool
	projectShuttingDown bool
	termServer          *TerminalServer
	dictation           *DictationService
	activityStats       *ActivityStatsRecorder
	previewCancel       context.CancelFunc
	previewWG           sync.WaitGroup
	attentionMu         sync.Mutex
	attentionCancel     context.CancelFunc
	attentionWG         sync.WaitGroup
	orphanCleanupMu     sync.Mutex
	orphanCleanupStop   context.CancelFunc
	orphanCleanupWG     sync.WaitGroup
	updateCheckMu       sync.Mutex
	updateCheckStop     context.CancelFunc
	updateCheckWG       sync.WaitGroup
	updateInstallMu     sync.Mutex
	updateInstallCancel context.CancelFunc
	updateInstalling    bool
	updateShuttingDown  bool
	updateCriticalWG    sync.WaitGroup
	tmuxWorkMu          sync.Mutex
	tmuxWorkCtx         context.Context
	tmuxWorkStop        context.CancelFunc
	tmuxWorkWG          sync.WaitGroup
	lastTypingSignal    int64 // unix nano timestamp of last typing signal
	// projectLocked is true when THIS instance owns the active project's lock.
	// otherInstancePID is the PID of the instance that owns it instead (0 if
	// none). Terminal attaches are refused unless projectLocked, so a second
	// GUI on the same project can't fight over its tmux sessions.
	projectLocked    bool
	otherInstancePID int
}

type eventThrottle struct {
	mu       sync.Mutex
	last     time.Time
	interval time.Duration
}

func (t *eventThrottle) allow(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && now.Sub(t.last) < t.interval {
		return false
	}
	t.last = now
	return true
}

// ptySession represents an active PTY connection
type ptySession struct {
	// ptmx is a PTY master on Unix and a pipe pair on Windows — see
	// session.StartTerminal. Only Read/Write/Close are used on it.
	ptmx      session.TerminalStream
	cmd       *exec.Cmd
	session   *session.Instance
	windowID  int
	projectID string
	cancel    context.CancelFunc
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

	// Repair PATH before anything shells out. A GUI launch on macOS inherits an
	// empty PATH — launchctl reports none — so tmux and the agents are invisible
	// to a Finder-started app while the same binary works from a terminal. This
	// is also why the TUI never hit it: a terminal launch inherits a real PATH.
	session.EnsureToolPath()

	// Refresh the activity-detection patterns in the background. Agents reword
	// their prompts on their own schedule, and a changed phrase means the app
	// stops noticing one waiting for an answer; this makes that fixable by
	// editing a file rather than by shipping a release. Off the startup path
	// because it reaches the network, and every failure leaves the patterns
	// already in place.
	go session.RefreshPatterns()

	// Verbose logs in a dev build, or in any build started with --debug /
	// ASMGR_DEBUG=1 — so a user hitting a bug can produce a diagnostic log
	// without installing a different binary.
	session.DebugLogging = isDevMode || debugEnabled
	if debugEnabled {
		log.Printf("[debug] verbose logging enabled (version %s)", Version)
	}

	// Clear what the last update could not delete while it was still running.
	// Nothing holds those files open now, so this is the moment they can go.
	updater.CleanStaleUpdateFiles()

	// Initialize storage
	storage, err := session.NewStorage()
	if err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to initialize storage: %v", err))
		return
	}
	a.storage = storage

	// Apply the active project's process/tmux settings before anything can
	// restart a pane or launch a provider. The same helper runs after every
	// successful project switch; these settings are stored per project even
	// though their live implementation is process/server-wide.
	// Server-wide, so it also covers the mirror sessions the terminal server
	// creates for attaching — those never go through session start-up.
	session.ConfigureClipboardForwarding()

	// Single-instance-per-project guard. Two GUIs on the same project attach
	// to the same tmux sessions and rip each other's ptys out
	// ("read /dev/ptmx: input/output error"), silently killing tabs. Try to
	// claim the active project's lock; if another live instance holds it,
	// record the holder's PID so the frontend can warn instead of stomping
	// the tmux state. We DON'T abort startup — the UI is still usable for
	// read-only browsing — but terminal attaches are gated on a.projectLocked.
	if lockErr := a.storage.LockProjectForUse(a.storage.GetActiveProjectID()); lockErr != nil {
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

	// After the lock, not before it.
	//
	// The mouse-copy half of this is gated on a.projectLocked — only the lock
	// owner may rewrite tmux key tables, which are server-wide and shared with
	// whatever else is attached. Called before the claim, that gate was always
	// shut, so the setting silently never applied at start-up: a binding left
	// by an earlier run stayed in force, and a fresh install kept copy-on-select
	// on for someone who never asked for it. SelectProject has always had this
	// order right; only start-up was inverted.
	a.applyActiveProjectRuntimeSettings()

	statsRecorder, err := NewActivityStatsRecorder()
	if err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to initialize activity statistics: %v", err))
	} else {
		a.activityStats = statsRecorder
	}

	// Start WebSocket terminal server for low-latency terminal I/O
	a.termServer = NewTerminalServer(storage, 9753)
	a.termServer.typingSignal = &a.lastTypingSignal
	a.termServer.beginAttach = a.beginTerminalAttach
	if err := a.termServer.Start(); err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to start terminal server: %v", err))
	}

	// Attention notifications (desktop/ntfy when an agent starts waiting).
	// Backend-side so it keeps working while the window is unfocused.
	if err := desktopNotificationInitialize(ctx); err != nil {
		log.Printf("[notify] native desktop notification initialization failed: %v", err)
	}
	a.startAttentionWatcher(ctx)

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
	voiceEvents := eventThrottle{interval: 80 * time.Millisecond}
	a.dictation.SetVoiceLevelCallback(func(level float64) {
		if !voiceEvents.allow(time.Now()) {
			return
		}
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

	// Clean up orphaned GUI tmux sessions from previous runs. This owns tmux
	// processes and a project read lock, so shutdown must be able to cancel and
	// reap it before releasing project ownership.
	a.startOrphanCleanup(ctx)
	// Resize/redraw maintenance is deliberately asynchronous so terminal resize
	// events do not stall the UI. Give it an application-owned lifecycle: the
	// workers touch tmux and retain project ownership, so shutdown must cancel
	// and reap them before terminal/project teardown.
	a.startTmuxMaintenance(ctx)

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

	a.startAutoCheckForUpdate(ctx)
}

func (a *App) startAutoCheckForUpdate(parent context.Context) {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	if a.updateCheckStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.updateCheckStop = cancel
	a.updateCheckWG.Add(1)
	go func() {
		defer a.updateCheckWG.Done()
		a.autoCheckForUpdate(ctx)
	}()
}

func (a *App) stopAutoCheckForUpdate() {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	if a.updateCheckStop == nil {
		return
	}
	a.updateCheckStop()
	a.updateCheckWG.Wait()
	a.updateCheckStop = nil
}

// autoCheckForUpdate looks for a new release shortly after launch, at most
// once a day (same throttle as the TUI version). It only ever notifies —
// installing stays a deliberate action in the update dialog.
func (a *App) autoCheckForUpdate(ctx context.Context) {
	// Let the window finish coming up first; a release check is never urgent.
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		return
	}

	if !updater.ShouldCheckForUpdate() {
		return
	}
	latest, err := updater.RefreshAvailableUpdateContext(ctx, Version)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("[update] automatic check failed: %v", err)
		return
	}
	if latest == "" {
		return
	}
	if ctx.Err() != nil {
		return
	}

	log.Printf("[update] %s available (running %s)", latest, Version)
	runtime.EventsEmit(ctx, "update:available", map[string]string{
		"version": latest,
		"current": Version,
	})
}

// IsDevMode returns whether the app is running in dev mode
func (a *App) IsDevMode() bool {
	return isDevMode
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// Installing the replacement executable is a transaction that must not be
	// interrupted by our own process teardown. Close the gate before stopping
	// other services so no new Wails update request can race this shutdown, and
	// wait for an already-running install to finish its final swap/rollback.
	a.stopUpdateInstall()

	// A project switch transfers an OS-level ownership lock. Serialize the
	// complete transfer with teardown and permanently close the project gate;
	// otherwise an in-flight SelectProject can acquire a new project lock after
	// shutdown has already released the old one.
	a.beginProjectShutdown()
	defer a.endProjectShutdown()

	// Stop every background storage/tmux reader before releasing the project
	// lock or removing terminal mirrors. Both pollers capture panes and read the
	// active project; letting either survive into teardown races the resources
	// shutdown is about to invalidate.
	a.stopAutoCheckForUpdate()
	a.stopAttentionWatcher()
	desktopNotificationCleanup(ctx)
	a.stopPreviewPolling()
	a.stopOrphanCleanup()
	a.stopTmuxMaintenance()
	// Dictation owns audio/hotkey goroutines whose callbacks can write to the
	// terminal and emit frontend events. Drain and detach them before stopping
	// the TerminalServer or releasing the active project's ownership.
	if a.dictation != nil {
		a.dictation.Shutdown()
	}
	legacyPTYCtx, cancelLegacyPTY := context.WithTimeout(context.Background(), 10*time.Second)
	if err := a.closeAllLegacyPTYs(legacyPTYCtx); err != nil {
		log.Printf("[terminal] legacy PTY shutdown did not complete cleanly: %v", err)
	}
	cancelLegacyPTY()
	if a.termServer != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.termServer.Stop(stopCtx); err != nil {
			log.Printf("[terminal] shutdown did not complete cleanly: %v", err)
		}
		cancel()
	}
	// An MCP client may still be inside its npx/startup handshake while its
	// Wails request holds the project read lock. Cancel and reap those starts
	// before waiting for exclusive project teardown; doing this at the end made
	// shutdown wait for the full external startup timeout (or forever if npx
	// itself wedged).
	stopAllTaskMasters()

	a.projectMu.Lock()
	// Persist Codex-generated conversation IDs before releasing the project
	// lock or cleaning up GUI mirrors. The base tmux sessions intentionally
	// survive app shutdown, so this is what makes a later reboot resume the
	// exact conversation.
	if a.projectLocked {
		a.persistActiveProjectCodexResumeIDs("shutdown")
		// Mirrors belong to the project lock owner. Remove them before dropping
		// ownership so the guard in cleanupAllGUISessions remains meaningful.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), session.TmuxCommandTimeout)
		a.cleanupAllGUISessions(cleanupCtx)
		cancel()
	}

	// Release the project lock only if WE hold it — a second instance that
	// failed to acquire it must not delete the real owner's lock file.
	if a.projectLocked {
		a.storage.UnlockProject()
		a.projectLocked = false
	}
	a.projectMu.Unlock()
	if a.activityStats != nil {
		if err := a.activityStats.Close(); err != nil {
			log.Printf("[statistics] failed to flush: %v", err)
		}
	}

}

// stopPreviewPolling cancels and reaps the sidebar polling goroutine. It is
// deliberately separate from shutdown's project teardown so the ordering is
// explicit and testable: the poller reads storage, captures tmux panes, writes
// statistics and emits events, none of which may continue after the active
// project's lock and mirrors are released.
func (a *App) stopPreviewPolling() {
	if a.previewCancel == nil {
		return
	}
	a.previewCancel()
	a.previewWG.Wait()
	a.previewCancel = nil
}

// stopAllTaskMasters stops and reaps every cached MCP child. The values are
// copied out before Stop so no potentially blocking process shutdown runs while
// the global lock is held.
func stopAllTaskMasters() {
	drainTaskMasters(true)
}

// drainTaskMasters cancels in-flight starts and reaps every cached provider.
// Project switches use a temporary gate and reopen it before returning;
// shutdown and an explicit feature disable keep it closed.
func drainTaskMasters(keepStartsBlocked bool) {
	taskMasterMu.Lock()
	taskMasterDrainEpoch++
	epoch := taskMasterDrainEpoch
	wasBlocked := taskMasterStartsBlocked
	// Close the registration gate before looking at in-flight starts. Without
	// this, a caller that observed the old enabled setting could register just
	// after the loop saw an empty map and leave a new npx child behind.
	taskMasterStartsBlocked = true
	taskMasterMu.Unlock()
	for {
		taskMasterMu.RLock()
		starts := make([]*taskMasterStart, 0, len(taskMasterStarts))
		for _, start := range taskMasterStarts {
			starts = append(starts, start)
		}
		taskMasterMu.RUnlock()
		if len(starts) == 0 {
			break
		}
		// The candidate is published before Start enters the external npx
		// handshake. Stop closes its pipes/processDone and makes Start return
		// promptly instead of forcing shutdown to wait out every startup phase.
		for _, start := range starts {
			if start.cancel != nil {
				start.cancel()
			}
			if start.tm != nil {
				_ = start.tm.Stop()
			}
		}
		for _, start := range starts {
			<-start.done
		}
	}
	taskMasterMu.Lock()
	taskMasters := make([]*mcp.TaskMaster, 0, len(taskMasterCache))
	for path, tm := range taskMasterCache {
		taskMasters = append(taskMasters, tm)
		delete(taskMasterCache, path)
	}
	// A project switch may only reopen a gate that it closed itself. If a
	// feature-disable or shutdown drain had already closed it, reopening here
	// would allow a late TaskMaster RPC to launch npx during teardown.
	if !keepStartsBlocked && !wasBlocked && taskMasterDrainEpoch == epoch {
		taskMasterStartsBlocked = false
	}
	taskMasterMu.Unlock()
	for _, tm := range taskMasters {
		_ = tm.Stop()
	}
}

// projectSwitchDrainTaskMasters is a seam for verifying the project-switch
// lock ordering. It must run before projectMu is taken for writing so it can
// cancel provider calls that still own a project read lock.
var projectSwitchDrainTaskMasters = func() {
	drainTaskMasters(false)
}

var cleanupTmuxList = func(ctx context.Context) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	defer cancel()
	return session.TmuxCommandContext(commandCtx, "list-sessions", "-F", "#{session_name}").Output()
}

var cleanupTmuxKill = func(ctx context.Context, name string) error {
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	defer cancel()
	return session.TmuxCommandContext(commandCtx, "kill-session", "-t", name).Run()
}

var resizeTerminalTmux = func(ctx context.Context, target, sessionName string) {
	workCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	defer cancel()
	_ = session.TmuxCommandContext(workCtx, "resize-window", "-t", target, "-A").Run()
	_ = session.RefreshSessionClientsContext(workCtx, sessionName)
}

func runBoundedTmuxCommand(ctx context.Context, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	defer cancel()
	return session.TmuxCommandContext(commandCtx, args...).Run()
}

func (a *App) lifecycleContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) startOrphanCleanup(parent context.Context) {
	a.orphanCleanupMu.Lock()
	defer a.orphanCleanupMu.Unlock()
	if a.orphanCleanupStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.orphanCleanupStop = cancel
	a.orphanCleanupWG.Add(1)
	go func() {
		defer a.orphanCleanupWG.Done()
		a.cleanupOrphanedGUISessions(ctx)
	}()
}

func (a *App) stopOrphanCleanup() {
	a.orphanCleanupMu.Lock()
	defer a.orphanCleanupMu.Unlock()
	if a.orphanCleanupStop == nil {
		return
	}
	a.orphanCleanupStop()
	a.orphanCleanupWG.Wait()
	a.orphanCleanupStop = nil
}

// cleanupOrphanedGUISessions removes GUI linked tmux sessions that belong to
// sessions that are no longer running (e.g. from a previous app crash).
func (a *App) cleanupOrphanedGUISessions(ctx context.Context) {
	// Startup launches this sweep in a goroutine. Pin the active project for
	// the whole scan so a simultaneous project switch cannot turn an ownership
	// check for project A into a tmux cleanup based on project B's sessions.
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	// Only the project owner may sweep mirrors. A second instance's "running"
	// set is built from ITS view of storage and would classify the owner's
	// live mirrors as orphaned, killing them and dropping the owner's
	// terminals. Non-owners created no mirrors, so there is nothing to reap.
	if !a.projectLocked {
		return
	}

	out, err := cleanupTmuxList(ctx)
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
			_ = cleanupTmuxKill(ctx, name)
		}
	}
}

// cleanupAllGUISessions removes THIS project's GUI linked tmux sessions on
// app shutdown.
func (a *App) cleanupAllGUISessions(ctx context.Context) {
	// Only the project owner sweeps, and only its OWN mirrors. Two guards:
	//  1. a non-owner created no mirrors (attaches refused) — nothing to do;
	//  2. even as owner, kill only mirrors whose base session is in THIS
	//     project's storage, so closing one project's window can't kill a
	//     different project's live terminals (the _gui_ namespace is shared
	//     across all projects on the machine).
	if !a.projectLocked {
		return
	}

	out, err := cleanupTmuxList(ctx)
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
			_ = cleanupTmuxKill(ctx, name)
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
	a.projectTransitionMu.Lock()
	defer a.projectTransitionMu.Unlock()
	if err := a.beginProjectSwitch(); err != nil {
		return err
	}
	defer a.endProjectSwitch()

	oldID := a.storage.GetActiveProjectID()
	if id == oldID {
		return nil
	}
	// Provider RPCs intentionally retain a project read lock for their whole
	// external side effect. Cancel and reap them before waiting for the writer
	// lock; doing this after Lock would make project switching wait for the very
	// RPCs it is meant to interrupt. The switch-intent gate above prevents a new
	// provider call from entering after this drain.
	projectSwitchDrainTaskMasters()

	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	// Once the active project changes, its running sessions are no longer part
	// of sidebar polling. Capture their Codex IDs while we still own that
	// project's lock and before switching storage to the new project.
	if a.projectLocked {
		a.persistActiveProjectCodexResumeIDs("project switch")
	}
	legacyDrainCtx, cancelLegacyDrain := context.WithTimeout(a.lifecycleContext(), 10*time.Second)
	legacyDrainErr := a.closeAllLegacyPTYs(legacyDrainCtx)
	cancelLegacyDrain()
	if legacyDrainErr != nil {
		return fmt.Errorf("cannot close legacy terminals for the current project: %w", legacyDrainErr)
	}
	// Terminal connections are owned by the same per-project lock as their
	// mirror sessions. Drain them before LockProjectForUse releases that lock;
	// otherwise another application instance can claim the old project while
	// this process still has a live PTY attached to it. The frontend normally
	// detaches first, but backend ownership must not depend on event timing.
	if a.termServer != nil && a.projectLocked {
		drainCtx, cancel := context.WithTimeout(a.lifecycleContext(), 2*session.TmuxCommandTimeout+2*time.Second)
		drainErr := a.termServer.CloseConnections(drainCtx)
		cancel()
		if drainErr != nil {
			return fmt.Errorf("cannot close terminals for the current project: %w", drainErr)
		}
	}
	clearProjectScopedCaches()
	// Close the attach/mutation gate before changing the Storage's active
	// project. Otherwise a connection can observe the old true value but load a
	// session from the newly selected, possibly foreign-owned project.
	a.projectLocked = false
	a.otherInstancePID = 0
	lockErr := a.storage.LockProjectForUse(id)
	var locked *session.ErrProjectLocked
	if lockErr != nil && !errors.As(lockErr, &locked) {
		_ = a.storage.SetActiveProject(oldID)
		if oldLockErr := a.storage.LockProjectForUse(oldID); oldLockErr == nil {
			a.projectLocked = true
		}
		return lockErr
	}
	if err := a.storage.SetActiveProject(id); err != nil {
		a.storage.UnlockProject()
		_ = a.storage.SetActiveProject(oldID)
		if oldLockErr := a.storage.LockProjectForUse(oldID); oldLockErr == nil {
			a.projectLocked = true
		}
		return err
	}
	if lockErr == nil {
		a.projectLocked = true
	} else {
		a.otherInstancePID = locked.PID
		log.Printf("[lock] switched to project %q which is open in pid %d — terminal attaches disabled", id, locked.PID)
	}
	// Opaque history entry IDs and portable-import snapshots are scoped to the
	// project in which they were issued. Invalidate history after the switch;
	// otherwise GlobalSearch/GetHistoryPreview can expose the previous
	// project's indexed prompts until the frontend happens to initialize again.
	a.historyMu.Lock()
	a.historyIndex = nil
	a.historyMu.Unlock()
	a.applyActiveProjectRuntimeSettings()
	return nil
}

// applyActiveProjectRuntimeSettings synchronizes process/server-wide state to
// the settings snapshot selected in Storage. A project switch changes the
// persistence target without going through SaveSettings, so leaving this work
// only on startup/save made shell and mouse bindings bleed across projects and
// left Task Master's global start gate closed after disabled A -> enabled B.
func (a *App) applyActiveProjectRuntimeSettings() {
	ctx := a.lifecycleContext()
	_, _, settings, err := a.storage.LoadAllWithSettingsContext(ctx)
	if err != nil {
		log.Printf("[settings] cannot load active project runtime settings: %v", err)
		settings = &session.Settings{}
	}
	if settings == nil {
		settings = &session.Settings{}
	}
	applyRuntimeTerminalShell(settings.TerminalShell)
	// Mouse bindings live in tmux's process-global key tables. A read-only
	// viewer shares that server with the process that owns this project, but is
	// forbidden from attaching terminals; applying the viewed project's value
	// here would nevertheless rewrite the real owner's live terminal behavior.
	// Only the project lock owner may mutate that shared runtime state.
	if a.projectLocked {
		applyCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
		applyRuntimeMouseCopy(applyCtx, settings.TerminalCopyMode == "select")
		cancel()
	}

	taskMasterMu.Lock()
	taskMasterStartsBlocked = !settings.TaskMasterEnabled
	taskMasterMu.Unlock()
}

func clearProjectScopedCaches() {
	taskManagerMu.Lock()
	taskManagerCache = make(map[string]*session.TaskManager)
	taskManagerMu.Unlock()

	fileIndexMu.Lock()
	fileIndexCache = make(map[fileIndexKey]*fileIndexCacheEntry)
	fileIndexMu.Unlock()

	gitBranchMu.Lock()
	gitBranchCache = make(map[string]gitBranchCacheEntry)
	gitBranchMu.Unlock()

	gitBranchListMu.Lock()
	gitBranchListCache = make(map[string]gitBranchListCacheEntry)
	gitBranchListMu.Unlock()

	tabWorkingDirMu.Lock()
	tabWorkingDirCache = make(map[string]tabWorkingDirCacheEntry)
	tabWorkingDirMu.Unlock()
}

func (a *App) persistActiveProjectCodexResumeIDs(reason string) {
	projectID, instances, _, err := a.storage.LoadAllWithProjectSnapshot()
	if err != nil {
		log.Printf("[%s] failed to load sessions for Codex resume capture: %v", reason, err)
		return
	}
	for _, instance := range instances {
		if !instance.NeedsCodexResumeCapture() {
			continue
		}
		if _, err := a.storage.CaptureCodexResumeIDsForProject(projectID, instance.ID); err != nil {
			log.Printf("[%s] failed to persist Codex resume IDs for session=%s: %v", reason, instance.ID, err)
		}
	}
}

// LockStatusInfo tells the frontend whether this instance owns the active
// project (and if not, which PID does) so it can warn the user.
type LockStatusInfo struct {
	Locked           bool `json:"locked"`
	OtherInstancePID int  `json:"otherInstancePid"`
}

// GetLockStatus reports whether this instance owns the active project's lock.
func (a *App) GetLockStatus() LockStatusInfo {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	return LockStatusInfo{Locked: a.projectLocked, OtherInstancePID: a.otherInstancePID}
}

func (a *App) beginProjectMutation() (func(), error) {
	// Project mutations are serialised as well as pinned to one active project.
	// The Storage mutex protects individual calls, but many App operations do a
	// load/change/save sequence or a tmux side effect followed by persistence.
	// Letting two such operations overlap loses fields from the older snapshot.
	a.projectMutationMu.Lock()
	if err := a.lockProjectOperationRead(); err != nil {
		a.projectMutationMu.Unlock()
		return nil, err
	}
	if !a.projectLocked {
		a.projectMu.RUnlock()
		a.projectMutationMu.Unlock()
		return nil, fmt.Errorf("project is read-only in this application instance")
	}
	return func() {
		a.projectMu.RUnlock()
		a.projectMutationMu.Unlock()
	}, nil
}

// lockProjectOperationRead closes the small gap between checking the switch
// intent and acquiring projectMu. Holding projectGateMu through RLock means a
// switch either observes this operation as an existing reader and cancels it,
// or announces its intent first and the operation is refused.
func (a *App) lockProjectOperationRead() error {
	a.projectGateMu.Lock()
	if a.projectShuttingDown {
		a.projectGateMu.Unlock()
		return fmt.Errorf("application is shutting down")
	}
	if a.projectSwitching {
		a.projectGateMu.Unlock()
		return fmt.Errorf("project switch in progress")
	}
	a.projectMu.RLock()
	a.projectGateMu.Unlock()
	return nil
}

func (a *App) beginProjectSwitch() error {
	a.projectGateMu.Lock()
	defer a.projectGateMu.Unlock()
	if a.projectShuttingDown {
		return fmt.Errorf("application is shutting down")
	}
	if a.projectSwitching {
		return fmt.Errorf("project switch already in progress")
	}
	a.projectSwitching = true
	return nil
}

func (a *App) endProjectSwitch() {
	a.projectGateMu.Lock()
	a.projectSwitching = false
	a.projectGateMu.Unlock()
}

func (a *App) beginProjectShutdown() {
	a.projectTransitionMu.Lock()
	a.projectGateMu.Lock()
	a.projectShuttingDown = true
	a.projectGateMu.Unlock()
}

func (a *App) endProjectShutdown() {
	a.projectTransitionMu.Unlock()
}

// beginExpectedProjectMutation additionally pins a frontend-captured project
// identity. This is needed by delayed UI writes: session IDs are not globally
// unique, so a debounce started in one project must not land in another one
// after SelectProject changes the Storage target.
func (a *App) beginExpectedProjectMutation(expectedProjectID string) (func(), error) {
	done, err := a.beginProjectMutation()
	if err != nil {
		return nil, err
	}
	if activeProjectID := a.storage.GetActiveProjectID(); activeProjectID != expectedProjectID {
		done()
		return nil, fmt.Errorf("active project changed: expected %q, got %q", expectedProjectID, activeProjectID)
	}
	return done, nil
}

func (a *App) beginTerminalAttach(expectedProjectID string) (func(), bool) {
	if err := a.lockProjectOperationRead(); err != nil {
		return nil, false
	}
	if !a.projectLocked || a.storage == nil || a.storage.GetActiveProjectID() != expectedProjectID {
		a.projectMu.RUnlock()
		return nil, false
	}
	return a.projectMu.RUnlock, true
}

func (a *App) startTmuxMaintenance(parent context.Context) {
	a.tmuxWorkMu.Lock()
	defer a.tmuxWorkMu.Unlock()
	if a.tmuxWorkStop != nil {
		return
	}
	a.tmuxWorkCtx, a.tmuxWorkStop = context.WithCancel(parent)
}

// queueTmuxMaintenance registers an asynchronous tmux operation with the app
// lifecycle. Registration and shutdown share tmuxWorkMu, so Wait can never
// race a late WaitGroup.Add. The operation must observe ctx for every external
// command it launches.
func (a *App) queueTmuxMaintenance(work func(context.Context)) bool {
	a.tmuxWorkMu.Lock()
	defer a.tmuxWorkMu.Unlock()
	if a.tmuxWorkCtx == nil || a.tmuxWorkStop == nil {
		return false
	}
	ctx := a.tmuxWorkCtx
	a.tmuxWorkWG.Add(1)
	go func() {
		defer a.tmuxWorkWG.Done()
		work(ctx)
	}()
	return true
}

func (a *App) stopTmuxMaintenance() {
	a.tmuxWorkMu.Lock()
	defer a.tmuxWorkMu.Unlock()
	if a.tmuxWorkStop == nil {
		return
	}
	a.tmuxWorkStop()
	a.tmuxWorkWG.Wait()
	a.tmuxWorkCtx = nil
	a.tmuxWorkStop = nil
}

func (a *App) beginProjectReadWithSideEffects() (func(), error) {
	if err := a.lockProjectOperationRead(); err != nil {
		return nil, err
	}
	if !a.projectLocked {
		a.projectMu.RUnlock()
		return nil, fmt.Errorf("project is read-only in this application instance")
	}
	return a.projectMu.RUnlock, nil
}

func (a *App) beginExpectedProjectReadWithSideEffects(expectedProjectID string) (func(), error) {
	done, err := a.beginProjectReadWithSideEffects()
	if err != nil {
		return nil, err
	}
	if activeProjectID := a.storage.GetActiveProjectID(); activeProjectID != expectedProjectID {
		done()
		return nil, fmt.Errorf("active project changed: expected %q, got %q", expectedProjectID, activeProjectID)
	}
	return done, nil
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
	done, err := a.beginProjectMutation()
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) ImportSessions(sourceProjectID string, sessionIDs []string, expectedProjectID string) (int, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return 0, err
	}
	defer done()
	// Load source project data atomically
	sourceInstances, sourceGroups, err := a.storage.LoadAllForProject(sourceProjectID)
	if err != nil {
		return 0, err
	}

	// Filter to only selected sessions
	selectedInstances := make([]*session.Instance, 0)
	for _, inst := range sourceInstances {
		for _, id := range sessionIDs {
			if inst.ID == id {
				selectedInstances = append(selectedInstances, inst)
				break
			}
		}
	}

	if len(selectedInstances) == 0 {
		return 0, fmt.Errorf("none of the selected sessions were found")
	}
	// Portable conversion intentionally strips source IDs, running state and
	// tmux metadata, while resolving group IDs to names for target-side remap.
	bundle := session.ToPortable(selectedInstances, sourceGroups, Version)
	return a.storage.ImportPortableSessions(bundle.Sessions, bundle.Groups)
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
	TerminalTheme      string                   `json:"terminalTheme"`
	TerminalFontSize   int                      `json:"terminalFontSize"`
	HideViewBar        int                      `json:"hideViewBar"`
	HideStatusBar      int                      `json:"hideStatusBar"`
	// The main window isn't always index 0, so the frontend needs it to map a
	// tab back to the session-level palette.
	MainWindowIndex int `json:"mainWindowIndex"`
	LastWindowIndex int `json:"lastWindowIndex"`
	// IsGitRepo drives whether the diff tab is offered at all — outside a
	// repository it could only ever report "not a git repository".
	IsGitRepo bool `json:"isGitRepo"`
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
		TerminalTheme:      inst.TerminalTheme,
		TerminalFontSize:   inst.TerminalFontSize,
		HideViewBar:        inst.HideViewBar,
		HideStatusBar:      inst.HideStatusBar,
		MainWindowIndex:    inst.GetMainWindowIndex(),
		LastWindowIndex:    inst.LastWindowIndex,
		IsGitRepo:          inst.IsGitRepo(),
	}
}

// CreateSession creates a new session
func (a *App) CreateSession(name, path string, agent string, autoYes bool, extraArgs, expectedProjectID string) (*SessionInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	inst, err := session.NewInstance(name, path, autoYes, session.AgentType(agent), extraArgs)
	if err != nil {
		return nil, err
	}
	// Refuse before storing anything. The session is created first and started
	// second, so an agent that is not installed used to leave a session in the
	// sidebar that had never run and never could — the error appeared, and the
	// dead entry stayed behind next to it.
	//
	// Both halves of what a session needs are checked: the multiplexer it runs
	// in, and the agent it runs. Only the agent was, so a missing multiplexer
	// still produced that dead entry — and on Windows, where the multiplexer is
	// a separate download rather than something most machines already have,
	// that is the likelier one to be missing.
	if err := session.CheckMultiplexer(); err != nil {
		return nil, err
	}
	if err := session.CheckAgentCommand(inst); err != nil {
		return nil, err
	}
	if err := a.storage.AddInstance(inst); err != nil {
		return nil, err
	}
	info := a.instanceToSessionInfo(inst)
	return &info, nil
}

// StartSession starts a session
func (a *App) StartSession(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}

	// StartSession is called for NEW sessions (no resume).
	// Clear any saved ResumeSessionID so it doesn't accidentally resume.
	// StartWithResume("") will generate a fresh --session-id if supported.
	log.Printf("[StartSession] id=%s agent=%s starting a fresh conversation", id, inst.Agent)
	inst.ResumeSessionID = ""

	if err := inst.Start(); err != nil {
		return err
	}
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.Stop() },
	)
}

// StartSessionWithResume starts a session with resume.
// If the supplied resume ID no longer exists on disk (Claude/Codex deleted
// the conversation file, moved machine, etc.), we drop it and start fresh
// instead of letting the CLI boot into a "No conversation found" error.
func (a *App) StartSessionWithResume(id, resumeID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	resumeID, clearSaved := resolveResumeID(inst.Agent, resumeID, inst.ResumeSessionID, session.ResumeIDExists)
	if clearSaved {
		// StartWithResume falls back to the instance's stored ID when its
		// argument is empty. Clear it as well or a rejected/missing ID still
		// reaches the agent through that fallback.
		inst.ResumeSessionID = ""
		log.Printf("[StartSessionWithResume] id=%s agent=%s saved conversation unavailable; starting fresh", id, inst.Agent)
	} else {
		log.Printf("[StartSessionWithResume] id=%s agent=%s resume=%t", id, inst.Agent, resumeID != "")
	}

	if err := inst.StartWithResume(resumeID); err != nil {
		return err
	}
	// StartWithResume may generate a new conversation ID when resumeID is
	// empty. Do not overwrite that generated identity with the empty request.
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.Stop() },
	)
}

func resolveResumeID(agent session.AgentType, requested, stored string, exists func(session.AgentType, string) bool) (string, bool) {
	candidate := requested
	if candidate == "" {
		candidate = stored
	}
	if candidate == "" {
		return "", false
	}
	if !session.IsSafeResumeID(candidate) || !exists(agent, candidate) {
		return "", true
	}
	return candidate, false
}

// StopSession stops a session
func (a *App) StopSession(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) RestartTab(id string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if err := inst.RestartWindow(windowIdx); err != nil {
		return err
	}
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.RestopWindow(windowIdx) },
	)
}

// RestartTabWithResume restarts a stopped tab with a specific resume session ID
func (a *App) RestartTabWithResume(id string, windowIdx int, resumeId, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}

	// Validate the resume ID exists for whichever agent owns this tab.
	if resumeId != "" {
		tabAgent := inst.Agent
		mainWindowIdx := inst.GetMainWindowIndex()
		if windowIdx != mainWindowIdx {
			for _, fw := range inst.FollowedWindows {
				if fw.Index == windowIdx {
					tabAgent = fw.Agent
					break
				}
			}
		}
		if !session.ResumeIDExists(tabAgent, resumeId) {
			log.Printf("[RestartTabWithResume] saved conversation unavailable for agent=%s — starting fresh", tabAgent)
			resumeId = ""
			// Also clear any persisted ID for this tab so future starts don't try again.
			if windowIdx == mainWindowIdx {
				inst.ResumeSessionID = ""
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
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.RestopWindow(windowIdx) },
	)
}

// StopTab stops a specific tab (tmux window) in a session
func (a *App) StopTab(id string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) DeleteTab(id string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.TrashTab(id, windowIdx)
}

// DeleteSession deletes a session
// UnfinishedTasksForSession returns the session's tasks that are not done.
//
// The frontend asks before closing or deleting a session, so work assigned to
// it is not thrown away silently. Only tasks explicitly tied to this session
// count — a project-wide task is not this session's business, and warning about
// it every time would train the warning away.
//
// A missing task store is not an error: most sessions never get tasks, and
// failing here would block deletion for all of them.
func (a *App) UnfinishedTasksForSession(sessionID string) ([]TaskInfo, error) {
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return []TaskInfo{}, nil
	}

	pending := tm.UnfinishedForSession(sessionID)
	result := make([]TaskInfo, len(pending))
	for i, t := range pending {
		result[i] = convertTask(t)
	}
	return result, nil
}

func (a *App) DeleteSession(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.TrashInstance(id)
}

type TrashItemInfo struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	ParentSessionID   string `json:"parentSessionId"`
	ParentSessionName string `json:"parentSessionName"`
	DeletedAt         string `json:"deletedAt"`
}

type BackupInfo struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Size      int64  `json:"size"`
}

func (a *App) GetTrashItems() ([]TrashItemInfo, error) {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	items, err := a.storage.ListTrash()
	if err != nil {
		return nil, err
	}
	result := make([]TrashItemInfo, 0, len(items))
	for _, item := range items {
		result = append(result, TrashItemInfo{
			ID:                item.ID,
			Kind:              item.Kind,
			Name:              item.SessionName,
			ParentSessionID:   item.ParentSessionID,
			ParentSessionName: item.ParentSessionName,
			DeletedAt:         item.DeletedAt.Format(time.RFC3339),
		})
	}
	return result, nil
}

func (a *App) RestoreTrashItem(id, expectedProjectID string) (*session.RestoreResult, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	return a.storage.RestoreTrashItem(id)
}

func (a *App) PermanentlyDeleteTrashItem(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.PermanentlyDeleteTrashItem(id)
}

func (a *App) EmptyTrash(expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.EmptyTrash()
}

func (a *App) GetBackups() ([]BackupInfo, error) {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	items, err := a.storage.ListBackups()
	if err != nil {
		return nil, err
	}
	result := make([]BackupInfo, 0, len(items))
	for _, item := range items {
		result = append(result, BackupInfo{
			ID:        item.ID,
			CreatedAt: item.CreatedAt.Format(time.RFC3339),
			Size:      item.Size,
		})
	}
	return result, nil
}

func (a *App) CreateBackup(expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if err := a.storage.CreateBackup(); err != nil {
		return err
	}
	// Tasks live in each working directory rather than in the store, so they
	// need their own snapshot — see session/task_backup.go. A failure here is
	// reported explicitly. The canonical backup is still a valid restore point,
	// so keep it and make the partial outcome clear rather than deleting the
	// only successful half during an I/O failure.
	if err := a.backupTaskFiles(); err != nil {
		return fmt.Errorf("session backup succeeded, but task backup failed: %w", err)
	}
	return nil
}

// backupTaskFiles snapshots the task file of every session's working directory.
func (a *App) backupTaskFiles() error {
	instances, err := a.storage.Load()
	if err != nil {
		return err
	}
	dirs := make([]string, 0, len(instances))
	for _, instance := range instances {
		if instance != nil && instance.Path != "" {
			dirs = append(dirs, instance.Path)
		}
	}
	return a.storage.BackupTaskFiles(dirs)
}

// GetTaskBackups lists the task snapshots, newest first.
func (a *App) GetTaskBackups() ([]BackupInfo, error) {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	items, err := a.storage.ListTaskBackups()
	if err != nil {
		return nil, err
	}
	result := make([]BackupInfo, 0, len(items))
	for _, item := range items {
		result = append(result, BackupInfo{
			ID:        item.ID,
			CreatedAt: item.CreatedAt.Format(time.RFC3339),
			Size:      item.Size,
		})
	}
	return result, nil
}

// RestoreTaskBackup puts a task snapshot back.
//
// Separate from RestoreBackup because the two cover different things and are
// wanted separately: recovering a deleted task should not also roll back every
// session and setting to that moment.
func (a *App) RestoreTaskBackup(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	restoreErr := a.storage.RestoreTaskBackup(id)
	// A multi-file restore can encounter an I/O failure after touching a target.
	// Reload even on error so memory never claims a different state than disk.
	reloadErr := reloadTaskManagerCache()
	return errors.Join(restoreErr, reloadErr)
}

func (a *App) RestoreBackup(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	instances, err := a.storage.Load()
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if instance.Status == session.StatusRunning {
			return fmt.Errorf("stop all sessions before restoring a backup")
		}
	}
	return a.storage.RestoreBackup(id)
}

// RenameSession renames a session
func (a *App) RenameSession(id, name, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Name = name
	return a.storage.UpdateInstance(inst)
}

// ToggleFavorite toggles favorite status
func (a *App) ToggleFavorite(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) CycleYoloMode(id string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
	return a.toggleAutoYes(id, expectedProjectID)
}

// ToggleAutoYes toggles YOLO mode and restarts the session if running
func (a *App) ToggleAutoYes(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.toggleAutoYes(id, expectedProjectID)
}

func (a *App) toggleAutoYes(id, expectedProjectID string) error {
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

		log.Printf("[ToggleAutoYes] session=%s resume=%t", id, resumeID != "")

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
		// Event delivery is asynchronous relative to SelectProject. Include the
		// frontend-captured project identity so a delayed event from project A
		// cannot tear down an identically named terminal already shown for B.
		runtime.EventsEmit(a.ctx, "session:restarted", SessionRestartedEvent{
			SessionID: id,
			ProjectID: expectedProjectID,
		})
		return nil
	}
	return nil
}

// SessionRestartedEvent pins an asynchronous terminal reconnect request to the
// project whose mutation produced it. Session IDs are only project-local.
type SessionRestartedEvent struct {
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
}

// getClaudeSessionIDFromTmux extracts the --resume or --session-id from the Claude process
// running in the given tmux session's main window (window 0).
func getClaudeSessionIDFromTmux(tmuxSession string) string {
	return getClaudeSessionIDFromTmuxWindow(tmuxSession, 0)
}

// getClaudeSessionIDFromTmuxWindow extracts the --resume or --session-id from the Claude process
// running in the given tmux session window by reading /proc/PID/cmdline.
func getClaudeSessionIDFromTmuxWindow(tmuxSession string, windowIdx int) string {
	return getClaudeSessionIDFromTmuxWindowContext(context.Background(), tmuxSession, windowIdx)
}

// getClaudeSessionIDFromTmuxWindowContext bounds the external process probes
// and lets the preview poller stop before project and terminal teardown.
func getClaudeSessionIDFromTmuxWindowContext(ctx context.Context, tmuxSession string, windowIdx int) string {
	// Get the PID of the process in the tmux pane
	target := fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	out, err := session.TmuxCommandContext(commandCtx, "display-message", "-t", target, "-p", "#{pane_pid}").Output()
	cancel()
	if err != nil {
		return ""
	}
	panePID := strings.TrimSpace(string(out))
	if panePID == "" {
		return ""
	}

	// Look at the pane's own process first, then its children.
	//
	// The pane process is USUALLY the agent itself: a tab runs the agent
	// directly, so tmux reports its pid, and the children are the MCP servers it
	// spawned. Checking only children — which this did — therefore inspected
	// every MCP server and never the agent. It went unnoticed because the id it
	// was looking for is one we put on the command line ourselves at launch, so
	// the value was already known; what it could never see was the user moving
	// the tab to a different conversation.
	//
	// Children still matter: a tab whose command is a shell wrapper has the
	// agent one level down.
	pids := []string{panePID}
	commandCtx, cancel = context.WithTimeout(ctx, session.TmuxCommandTimeout)
	childOut, childErr := session.CommandContext(commandCtx, "pgrep", "-P", panePID).Output()
	cancel()
	if childErr == nil {
		pids = append(pids, strings.Fields(string(childOut))...)
	}
	candidatePIDs := pids

	// Ask Claude which conversation it is on, before falling back to reading the
	// arguments it was started with.
	//
	// Those arguments are fixed at launch. Running /resume inside a session
	// switches the same process to another conversation without restarting it,
	// so from then on argv names the conversation the user moved AWAY from —
	// which is what got resumed on restart, or nothing at all when the session
	// was started from the picker with no id on the command line.
	if id := session.ClaudeSessionIDForPIDs(candidatePIDs); id != "" {
		return id
	}

	// Check each child process for --resume or --session-id flag
	for _, pidStr := range candidatePIDs {
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
					log.Printf("[getClaudeSessionIDFromTmux] PID %s %s value rejected (unsafe shape)", pidStr, arg)
					continue
				}
				log.Printf("[getClaudeSessionIDFromTmux] found a session ID from PID %s (flag %s)", pidStr, arg)
				return candidate
			}
		}
	}

	return ""
}

// SetSessionColor sets session colors
func (a *App) SetSessionColor(id, color, bgColor string, fullRow bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) SetSessionNotes(id string, notes string, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.Notes = notes
	return a.storage.UpdateInstance(inst)
}

// AssignToGroup assigns session to group
func (a *App) AssignToGroup(sessionID, groupID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.SetInstanceGroup(sessionID, groupID)
}

// ReorderSession moves a session up or down in the list
func (a *App) ReorderSession(sessionID string, direction int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.ReorderInstance(sessionID, direction)
}

// MoveSessionToIndex moves a session to a specific index in the list
func (a *App) MoveSessionToIndex(sessionID string, targetIndex int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.MoveInstanceToIndex(sessionID, targetIndex)
}

// SendPrompt sends text to session
func (a *App) SendPrompt(id string, text string, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.sendPrompt(id, text)
}

func (a *App) sendPrompt(id string, text string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	return inst.SendPrompt(text)
}

// SendPromptToWindow sends text to a specific tab rather than to whichever
// window the session happens to have active.
func (a *App) SendPromptToWindow(id string, windowIdx int, text, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	return inst.SendPromptToWindow(text, windowIdx)
}

// ============================================================================
// Fork Session (Claude only)
// ============================================================================

// ForkResult contains fork operation result
type ForkResult struct {
	SessionID string `json:"sessionId"`
}

// ForkSession forks a Claude session
// ForkSession branches the conversation in one window into a new one.
//
// windowIdx names the tab being forked. Without it this read the session's main
// window every time, so forking from a second Claude tab branched a different
// conversation than the one on screen.
func (a *App) ForkSession(id string, windowIdx int, expectedProjectID string) (*ForkResult, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	// Ask the running process which conversation it is on before branching it.
	//
	// The stored id is what the tab was started with, and /resume inside Claude
	// Code moves the same process to another conversation without restarting
	// it. The sidebar poll notices within a couple of seconds, but a fork asked
	// for in that window branched the conversation the user had just left
	// rather than the one on screen.
	if live := getClaudeSessionIDFromTmuxWindow(inst.TmuxSessionName(), windowIdx); live != "" {
		if a.recordLiveConversation(inst, windowIdx, live) {
			if err := a.storage.UpdateInstance(inst); err != nil {
				log.Printf("[Fork] could not store the refreshed id for %s: %v", inst.ID, err)
			}
		}
	}

	sessionID, err := inst.ForkSession(windowIdx)
	if err != nil {
		return nil, err
	}

	return &ForkResult{SessionID: sessionID}, nil
}

// recordLiveConversation stores a freshly detected conversation id on the right
// window, reporting whether anything changed. The tab it was forked from stops
// pointing at a stale conversation too — the poll would get there eventually,
// but the fork already knows.
func (a *App) recordLiveConversation(inst *session.Instance, windowIdx int, live string) bool {
	if windowIdx == inst.GetMainWindowIndex() {
		if inst.ResumeSessionID == live {
			return false
		}
		log.Printf("[Fork] session=%s refreshed the live conversation ID", inst.ID)
		inst.ResumeSessionID = live
		return true
	}
	for idx := range inst.FollowedWindows {
		fw := &inst.FollowedWindows[idx]
		if fw.Index != windowIdx {
			continue
		}
		if fw.ResumeSessionID == live {
			return false
		}
		log.Printf("[Fork] session=%s tab %d refreshed the live conversation ID", inst.ID, windowIdx)
		fw.ResumeSessionID = live
		return true
	}
	return false
}

// ForkToNewTab forks to a new tab
// ForkToNewTab creates the forked tab and returns its window index, so the
// frontend can switch to it immediately — the same as CreateTab.
func (a *App) ForkToNewTab(id, name, sessionID, expectedProjectID string) (int, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return 0, err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return 0, err
	}
	newIdx, err := inst.NewForkedTab(name, sessionID)
	if err != nil {
		return 0, err
	}
	if err := persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.DeleteWindow(newIdx) },
	); err != nil {
		return 0, err
	}
	return newIdx, nil
}

// ForkToNewSession creates a new session from forked Claude conversation
func (a *App) ForkToNewSession(id, name, sessionID, expectedProjectID string) (*SessionInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	origInst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}

	// Create new session with same settings
	// The branch runs the agent the original ran. Hard-coded to Claude, a fork
	// of a Codex session came back as a Claude one with a Codex conversation id
	// it could not resume.
	newInst, err := session.NewInstance(name, origInst.Path, origInst.AutoYes, origInst.Agent, origInst.ExtraArgs)
	if err != nil {
		return nil, err
	}

	// Copy settings from original
	newInst.GroupID = origInst.GroupID
	newInst.Color = origInst.Color
	newInst.BgColor = origInst.BgColor
	newInst.FullRowColor = origInst.FullRowColor
	newInst.Notes = fmt.Sprintf("Forked from: %s", origInst.Name)
	// Branched, not continued: ForkFrom makes the first start load this
	// conversation and carry on in a new one. Set as ResumeSessionID instead,
	// the session would simply BE the original — two tabs writing to one
	// conversation.
	newInst.ForkFrom = sessionID

	// Refuse before storing anything, as CreateSession does. A fork that cannot
	// start otherwise left a session in the sidebar that had never run and never
	// could — the dialog closed as though it had worked, and the only way to
	// find out was to press Start and watch it fail again.
	if err := session.CheckMultiplexer(); err != nil {
		return nil, err
	}
	if err := session.CheckAgentCommand(newInst); err != nil {
		return nil, err
	}

	// Save new session
	if err := a.storage.AddInstance(newInst); err != nil {
		return nil, err
	}

	// Start the forked session with resume.
	//
	// A failure here removes the session again rather than leaving a dead entry
	// behind: it was created for this branch and has nothing else in it, so
	// there is nothing to keep. The error goes to the caller, which is what puts
	// it in front of the user instead of only in the log.
	if err := newInst.StartWithResume(""); err != nil {
		runtime.LogWarning(a.ctx, fmt.Sprintf("Failed to auto-start forked session: %v", err))
		delErr := a.storage.RemoveInstance(newInst.ID)
		if delErr != nil {
			runtime.LogWarning(a.ctx, fmt.Sprintf("Failed to clean up the forked session: %v", delErr))
		}
		return nil, errors.Join(err, delErr)
	}
	if err := persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(newInst) },
		func() error {
			if stopErr := newInst.Stop(); stopErr != nil {
				// Keep the stored entry if the external process could not be
				// stopped; hiding a live orphan would be worse than stale status.
				return stopErr
			}
			return a.storage.RemoveInstance(newInst.ID)
		},
	); err != nil {
		return nil, err
	}

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
func (a *App) CreateGroup(name, expectedProjectID string) (*GroupInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
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
func (a *App) DeleteGroup(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.RemoveGroup(id)
}

// RenameGroup renames a group
func (a *App) RenameGroup(id, name, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.RenameGroup(id, name)
}

// MoveGroup moves a group to a new position in the sidebar order
func (a *App) MoveGroup(id string, newIndex int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.MoveGroup(id, newIndex)
}

// ToggleGroupCollapse toggles group collapsed state
func (a *App) ToggleGroupCollapse(id, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.ToggleGroupCollapsed(id)
}

// SetGroupColor sets group colors
func (a *App) SetGroupColor(id, color, bgColor string, fullRow bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.SetGroupColors(id, color, bgColor, fullRow)
}

// ============================================================================
// Tabs (Multi-window support)
// ============================================================================

// CreateTab creates a new tab in session
// CreateTab creates a new tab and returns the new tmux window index so the
// frontend can switch to (and focus) it immediately.
func (a *App) CreateTab(sessionID string, isAgent bool, agent string, name string, extraArgs string, workDir string, expectedProjectID string) (int, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return -1, err
	}
	defer done()
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
		idx, err := inst.NewWindowWithName(name, workDir)
		if err != nil {
			return -1, err
		}
		newIdx = idx
	}
	if err := persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.DeleteWindow(newIdx) },
	); err != nil {
		return -1, err
	}
	return newIdx, nil
}

// CloseTab closes a tab
func (a *App) CloseTab(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.TrashTab(sessionID, windowIdx)
}

// RenameTab renames a tab
func (a *App) RenameTab(sessionID string, windowIdx int, name, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	oldName, err := inst.RenameWindow(windowIdx, name)
	if err != nil {
		return err
	}
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error {
			_, rollbackErr := inst.RenameWindow(windowIdx, oldName)
			return rollbackErr
		},
	)
}

func persistOrRollbackExternalMutation(persist, rollback func() error) error {
	if err := persist(); err != nil {
		return errors.Join(err, rollback())
	}
	return nil
}

// ReorderTab reorders a tab within a session's display order.
// fromPos and toPos are indices into the tab display order (0-based, including main window).
func (a *App) ReorderTab(sessionID string, fromPos, toPos int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) SetTabNotes(sessionID string, windowIdx int, notes, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) SetTabColor(sessionID string, windowIdx int, textColor, backgroundColor, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) SetWindowAutoYes(sessionID string, windowIdx int, enabled bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) SetExtraArgs(sessionID string, windowIdx int, extraArgs, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	log.Printf("[SetExtraArgs] sessionID=%s windowIdx=%d value_changed=true", sessionID, windowIdx)
	if windowIdx == 0 {
		inst.ExtraArgs = extraArgs
		return a.storage.UpdateInstance(inst)
	}
	for i := range inst.FollowedWindows {
		if inst.FollowedWindows[i].Index == windowIdx {
			inst.FollowedWindows[i].ExtraArgs = extraArgs
			log.Printf("[SetExtraArgs] tab %d value changed", windowIdx)
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

// GetPreview returns session preview content pinned to the frontend-captured
// project. Session IDs are project-local and preview reads live tmux content,
// so a delayed request must not cross a concurrent project switch.
func (a *App) GetPreview(id string, lines int, expectedProjectID string) (*PreviewData, error) {
	done, err := a.beginExpectedProjectReadWithSideEffects(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
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
	ProjectID    string                     `json:"projectId"`
	Activities   map[string]string          `json:"activities"`
	StatusLines  map[string]string          `json:"statusLines"`
	SpinnerTexts map[string]string          `json:"spinnerTexts"`
	TabStatuses  map[string][]TabStatusInfo `json:"tabStatuses"`
	observations []activityObservation
}

// GetSidebarUpdates returns activity and status line data in one call (single LoadAll)
func (a *App) GetSidebarUpdates() SidebarUpdate {
	return a.getSidebarUpdates(context.Background())
}

func (a *App) getSidebarUpdates(ctx context.Context) SidebarUpdate {
	a.projectMu.RLock()
	mayPersist := a.projectLocked
	defer a.projectMu.RUnlock()

	result := SidebarUpdate{
		Activities:   make(map[string]string),
		StatusLines:  make(map[string]string),
		SpinnerTexts: make(map[string]string),
		TabStatuses:  make(map[string][]TabStatusInfo),
	}

	projectID, instances, _, err := a.storage.LoadAllWithProjectSnapshotContext(ctx)
	if err != nil {
		return result
	}
	result.ProjectID = projectID

	// Phase 1: auto-detect + persist session IDs (sequential; touches storage).
	// Phase 2 (below) runs the tmux capture + detection in parallel across
	// sessions so total wall time scales with max-per-session work rather
	// than sum-per-session work.
	type detectJob struct {
		inst *session.Instance
	}
	var jobs []detectJob

	for _, inst := range instances {
		if ctx.Err() != nil {
			return result
		}
		if inst.Status != session.StatusRunning {
			continue
		}

		// Auto-detect and persist Claude session ID from running process
		// so that resume works correctly after app/machine restart
		needSave := false
		if mayPersist && inst.NeedsCodexResumeCapture() {
			if _, err := a.storage.CaptureCodexResumeIDsForProjectContext(ctx, projectID, inst.ID); err != nil {
				log.Printf("[SidebarPoll] failed to capture Codex session IDs for session=%s: %v", inst.ID, err)
			}
		}
		// Detected on every poll, not only while nothing is recorded: /resume
		// inside a session moves it to another conversation, and stopping at the
		// first answer left us pointing at the one the user had left behind.
		// Detection reads Claude's own record of what each process is on, so a
		// value that disagrees is the stale one; finding nothing changes nothing.
		if inst.Agent == session.AgentClaude {
			if sid := getClaudeSessionIDFromTmuxWindowContext(ctx, inst.TmuxSessionName(), 0); sid != "" && sid != inst.ResumeSessionID {
				log.Printf("[SidebarPoll] refreshed conversation ID for session=%s", inst.ID)
				inst.ResumeSessionID = sid
				needSave = true
			}
		}

		// Auto-detect Claude session ID for followed windows (tabs)
		for idx := range inst.FollowedWindows {
			fw := &inst.FollowedWindows[idx]
			if fw.Agent == session.AgentClaude {
				if sid := getClaudeSessionIDFromTmuxWindowContext(ctx, inst.TmuxSessionName(), fw.Index); sid != "" && sid != fw.ResumeSessionID {
					log.Printf("[SidebarPoll] refreshed Claude conversation ID for tab=%s/%d", inst.ID, fw.Index)
					fw.ResumeSessionID = sid
					needSave = true
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
				log.Printf("[SidebarPoll] auto-detected Gemini conversation ID for session=%s", inst.ID)
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
					log.Printf("[SidebarPoll] auto-detected Gemini conversation ID for tab=%s/%d", inst.ID, fw.Index)
				}
			}
		}

		if mayPersist && needSave {
			if err := a.storage.MergeResumeSessionIDsForProject(projectID, inst); err != nil {
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
				activity, activityValid := inst.DetectActivityForWindowWithValidityContext(ctx, w.idx)
				validActivityWindows[w.idx] = activityValid
				info := inst.GetStatusInfoForWindowContext(ctx, w.idx, w.agent)

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
					Yolo:           inst.DetectYoloForWindowContext(ctx, w.idx),
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

			data := a.getSidebarUpdates(ctx)
			// Drop a completed snapshot if the user switched projects while
			// tmux captures were running. The snapshot itself carries the ID
			// captured atomically with its instance list, so A→B→A is safe.
			if data.ProjectID != a.storage.GetActiveProjectID() {
				continue
			}
			if a.activityStats != nil {
				a.activityStats.Observe(data.ProjectID, time.Now(), data.observations)
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
func (a *App) AttachSession(id string, windowIdx int, expectedProjectID string) (string, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return "", err
	}
	defer done()
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
	if existing, exists := a.ptys[ptyID]; exists {
		a.ptyMu.Unlock()
		if existing.projectID != expectedProjectID {
			return "", fmt.Errorf("error.ptyProjectChanged")
		}
		return ptyID, nil
	}

	// Get tmux session name
	tmuxSession := inst.TmuxSessionName()

	// Start tmux attach command with PTY
	ctx, cancel := context.WithCancel(context.Background())
	cmd := session.TmuxCommandContext(ctx, "attach-session", "-t", fmt.Sprintf("%s:%d", tmuxSession, windowIdx))

	ptmx, err := session.StartTerminal(cmd)
	if err != nil {
		cancel()
		a.ptyMu.Unlock()
		return "", fmt.Errorf("failed to start PTY: %w", err)
	}

	ps := &ptySession{
		ptmx:      ptmx,
		cmd:       cmd,
		session:   inst,
		windowID:  windowIdx,
		projectID: expectedProjectID,
		cancel:    cancel,
	}

	a.ptys[ptyID] = ps
	a.ptyMu.Unlock()

	// Start reading PTY output
	go a.readPTY(ptyID, ptmx)

	return ptyID, nil
}

// readPTY reads from PTY and emits events with batching for performance
func (a *App) readPTY(ptyID string, ptmx session.TerminalStream) {
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
	a.detachSessionIfCurrent(ptyID, ptmx)
}

// closeAllLegacyPTYs drains the older Wails PTY transport before active
// project ownership moves. These processes are not managed by TerminalServer,
// so closing only WebSocket connections left an attach to project A alive
// after project B became active. Snapshot-and-delete also makes readPTY's
// eventual DetachSession idempotent.
func (a *App) closeAllLegacyPTYs(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		a.ptyMu.Lock()
		if pending := a.ptyDrainDone; pending != nil {
			a.ptyMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		connections := make([]*ptySession, 0, len(a.ptys))
		for id, ps := range a.ptys {
			connections = append(connections, ps)
			delete(a.ptys, id)
		}
		if len(connections) == 0 {
			a.ptyMu.Unlock()
			return nil
		}
		done := make(chan struct{})
		a.ptyDrainDone = done
		a.ptyMu.Unlock()

		go func() {
			for _, ps := range connections {
				if ps.cancel != nil {
					ps.cancel()
				}
				if ps.ptmx != nil {
					_ = ps.ptmx.Close()
				}
				if ps.cmd != nil && ps.cmd.Process != nil {
					_ = ps.cmd.Wait()
				}
			}
			a.ptyMu.Lock()
			if a.ptyDrainDone == done {
				a.ptyDrainDone = nil
			}
			close(done)
			a.ptyMu.Unlock()
		}()

		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// DetachSession detaches from a session terminal
func (a *App) DetachSession(ptyID string) error {
	return a.detachSessionIfCurrent(ptyID, nil)
}

// detachSessionIfCurrent lets a reader clean up only the stream it was
// created for. A project switch removes legacy PTYs from the map before their
// reader goroutines necessarily observe Close; without this identity check a
// late reader from project A could detach a newly registered, same-ID PTY from
// project B.
func (a *App) detachSessionIfCurrent(ptyID string, expected session.TerminalStream) error {
	a.ptyMu.Lock()
	ps, exists := a.ptys[ptyID]
	if !exists || (expected != nil && ps.ptmx != expected) {
		a.ptyMu.Unlock()
		return nil
	}
	delete(a.ptys, ptyID)
	a.ptyMu.Unlock()

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

	runtime.EventsEmit(a.ctx, "pty:closed:"+ptyID, nil)
	return nil
}

// SendInput sends input to PTY
func (a *App) SendInput(ptyID string, data string, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	a.ptyMu.RLock()
	ps, exists := a.ptys[ptyID]
	a.ptyMu.RUnlock()

	if !exists || ps.ptmx == nil {
		return fmt.Errorf("error.ptyNotFound")
	}
	if ps.projectID != expectedProjectID {
		return fmt.Errorf("error.ptyProjectChanged")
	}

	_, err = ps.ptmx.Write([]byte(data))
	return err
}

// ResizeTerminal resizes PTY and refreshes tmux
func (a *App) ResizeTerminal(ptyID string, cols, rows int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	releaseProject := true
	defer func() {
		if releaseProject {
			done()
		}
	}()
	a.ptyMu.RLock()
	ps, exists := a.ptys[ptyID]
	a.ptyMu.RUnlock()

	if !exists {
		return fmt.Errorf("error.ptyNotFound")
	}
	if ps.projectID != expectedProjectID {
		return fmt.Errorf("error.ptyProjectChanged")
	}

	// Resize the stream itself: the PTY ioctl on Unix, and on Windows the
	// multiplexer's refresh-client over the control-mode channel — which there
	// is the only thing that works, since psmux ignores resize-window.
	//
	// A failure here is reported but must not abort the resize: on Windows this
	// is a write to a live command channel, so a client that has just gone away
	// would otherwise turn a cosmetic resize into a hard error.
	if err := session.SetTerminalSize(ps.ptmx, cols, rows); err != nil {
		log.Printf("[resize] set size %dx%d: %v", cols, rows, err)
	}

	// Force tmux to resize window and refresh
	if ps.session != nil {
		sessionName := ps.session.TmuxSessionName()
		target := fmt.Sprintf("%s:%d", sessionName, ps.windowID)
		if a.queueTmuxMaintenance(func(ctx context.Context) {
			// Keep the active project's read/mutation ownership until every tmux
			// side effect finishes. A project switch must not let this late resize
			// target a session it no longer owns.
			defer done()
			resizeTerminalTmux(ctx, target, sessionName)
		}) {
			releaseProject = false
		}
	}

	return nil
}

// RefreshWindow forces tmux to redraw the pane for the given session window.
// Fixes occasional rendering glitches (garbled characters) by sending Ctrl+L
// to the pane and refreshing all clients attached to the tmux session.
func (a *App) RefreshWindow(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
	//
	// This sends INPUT to whatever is running in the pane, so it belongs only
	// on the Refresh button, where the user has asked for it. Not all readers
	// treat Ctrl-L as a screen clear — an agent prompt takes it as text, and
	// Claude Code turned it into a stray "/clear" in the composer. Automatic
	// callers must use RedrawWindow() instead.
	// Always sent here, unlike the automatic path: the button exists precisely
	// for when the pane looks wrong, so suppressing it would defeat the only
	// manual recovery there is. Pressing it twice is the user's choice.
	ctx, cancel := context.WithTimeout(a.lifecycleContext(), session.TmuxCommandTimeout)
	defer cancel()
	_ = runBoundedTmuxCommand(ctx, "send-keys", "-t", target, "C-l")
	_ = runBoundedTmuxCommand(ctx, "resize-window", "-t", target, "-A")
	_ = session.RefreshSessionClientsContext(ctx, sessionName)

	return nil
}

// RedrawWindow repaints a window without sending it any input.
//
// The automatic counterpart to RefreshWindow: it re-announces the size and asks
// the multiplexer to repaint, but never injects keystrokes into a running
// agent. It cannot make a bottom-aligned TUI lay itself out again — only the
// program can do that, and asking it costs a keystroke — so a pane may stay
// visually offset until something else prompts a redraw. That is the right
// trade for something that runs on its own: a cosmetic offset is recoverable,
// text typed into an agent's prompt is not.
func (a *App) RedrawWindow(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if !inst.IsAlive() {
		return fmt.Errorf("error.sessionNotRunning")
	}

	sessionName := inst.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	ctx, cancel := context.WithTimeout(a.lifecycleContext(), session.TmuxCommandTimeout)
	defer cancel()
	_ = runBoundedTmuxCommand(ctx, "resize-window", "-t", target, "-A")
	_ = session.RefreshSessionClientsContext(ctx, sessionName)

	return nil
}

// OpenFolder shows a directory in the desktop's file manager.
//
// Takes a path rather than a session id because the caller is the status bar,
// which shows the TAB's directory resolved live from the pane: a tab can be
// opened in its own directory, and the pane may have been cd-ed elsewhere
// since. The session's own path would be the wrong one in both cases.
func (a *App) OpenFolder(path string) error {
	return session.OpenInFileManager(path)
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
func (a *App) GetSessionDiff(id string, windowIdx int, expectedRoot string) (*DiffData, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot

	diff := inst.GetSessionDiff()
	return &DiffData{
		Content: diff.Content,
		Added:   diff.Added,
		Removed: diff.Removed,
	}, nil
}

// GetFullDiff returns full uncommitted diff for path
func (a *App) GetFullDiff(id string, windowIdx int, expectedRoot string) (*DiffData, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot

	diff := inst.GetFullDiff()
	return &DiffData{
		Content: diff.Content,
		Added:   diff.Added,
		Removed: diff.Removed,
	}, nil
}

// GetSessionDiffFiles returns the per-file diff since the session started.
func (a *App) GetSessionDiffFiles(id string, windowIdx int, expectedRoot string) ([]session.DiffFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetSessionDiffFiles()
}

// GetFullDiffFiles returns the per-file uncommitted diff.
func (a *App) GetFullDiffFiles(id string, windowIdx int, expectedRoot string) ([]session.DiffFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetFullDiffFiles()
}

// PatternRefreshResult is what the Settings dialog shows after a manual check.
type PatternRefreshResult struct {
	Version int  `json:"version"`
	Updated bool `json:"updated"`
}

// RefreshDetectionPatterns fetches the activity-detection patterns now.
//
// The background refresh runs at most once a day, which is right for something
// nobody asked for but wrong when a fix has just been published and the user
// wants it. Reports the version in force afterwards, and whether it changed, so
// "already up to date" and "updated to 4" are distinguishable — otherwise a
// working check and a silently failing one look the same.
func (a *App) RefreshDetectionPatterns() (*PatternRefreshResult, error) {
	version, updated, err := session.ForceRefreshPatterns()
	if err != nil {
		return nil, err
	}
	return &PatternRefreshResult{Version: version, Updated: updated}, nil
}

// DetectionPatternsVersion reports the pattern version in force.
func (a *App) DetectionPatternsVersion() int {
	return session.PatternsVersion()
}

// GetSessionDiffFileList lists files changed since the session started, without
// their contents.
//
// The diff view lists first and loads a file when it is opened. Fetching every
// file's hunks up front is what made the view unusable while something was
// writing a lot of files — a build dropping its output into the tree produced a
// diff the webview never finished rendering. Listing costs the same whatever
// the files contain.
func (a *App) GetSessionDiffFileList(id string, windowIdx int, expectedRoot string) ([]session.DiffFileSummary, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetSessionDiffFileList()
}

// GetFullDiffFileList lists files with uncommitted changes, without contents.
func (a *App) GetFullDiffFileList(id string, windowIdx int, expectedRoot string) ([]session.DiffFileSummary, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetFullDiffFileList()
}

// GetSessionDiffForFile returns one file's diff since the session started.
//
// wholeFile asks for the whole file around the changes, for the view that shows
// a change in the context of the file it lives in.
func (a *App) GetSessionDiffForFile(id, path string, wholeFile bool, windowIdx int, expectedRoot string) (*session.DiffFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetSessionDiffForFile(path, wholeFile)
}

// GetFullDiffForFile returns one file's uncommitted diff.
func (a *App) GetFullDiffForFile(id, path string, wholeFile bool, windowIdx int, expectedRoot string) (*session.DiffFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.GetFullDiffForFile(path, wholeFile)
}

// RevertDiffFile discards every pending change to one file.
func (a *App) RevertDiffFile(id, path string, sessionScope bool, windowIdx int, expectedRoot, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return err
	}
	inst.BrowseRoot = resolvedRoot
	baseRef := ""
	if sessionScope {
		baseRef = inst.BaseCommitSHA
	}
	return inst.RevertFile(path, baseRef)
}

// RevertDiffHunk undoes a single change block. The patch is the text the UI
// displayed, so a file that moved on since makes git refuse instead of
// reverting something the user never saw.
func (a *App) RevertDiffHunk(id, patch string, windowIdx int, expectedRoot, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.RevertHunk(patch)
}

func validateDiffRoot(inst *session.Instance, expectedRoot string) (string, error) {
	return validateRootSnapshot(inst, expectedRoot, "the tab working directory changed; reopen the diff")
}

// ============================================================================
// File Browser
// ============================================================================

// ListSessionDirectory lists one directory inside a session's working
// directory. The path is relative to that directory; "" lists its root. Paths
// that would escape the tree are rejected in session.Instance.
//
// Not named BrowseDirectory: that is already the native directory picker.
// browseInstance loads a session with its file browser pointed at the given
// tab's directory.
//
// A tab can be opened in a directory of its own, and its pane may have been
// cd-ed elsewhere since — GetTabWorkingDirectory resolves both. Without this
// the files view showed the session's own tree whichever tab you were on.
//
// windowIdx < 0 means "the session itself", for callers with no tab in hand.
func (a *App) browseInstance(id string, windowIdx int) (*session.Instance, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if windowIdx >= 0 {
		if dir := a.GetTabWorkingDirectory(id, windowIdx); dir != "" {
			inst.BrowseRoot = dir
		}
	}
	return inst, nil
}

func (a *App) ListSessionDirectory(id, path string, windowIdx int, expectedRoot string) (*session.BrowseListing, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	isRoot := path == "" || filepath.Clean(filepath.FromSlash(path)) == "."
	if !isRoot || expectedRoot != "" {
		resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
		if err != nil {
			return nil, err
		}
		inst.BrowseRoot = resolvedRoot
	}
	return inst.ListDirectory(path)
}

// ReadSessionDirectoryFile returns one file's contents for display. Read-only:
// there is no counterpart that writes.
//
// The long name avoids ReadSessionFile, which already means "open the session
// export the user picked".
func (a *App) ReadSessionDirectoryFile(id, path string, windowIdx int, expectedRoot string) (*session.BrowseFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.ReadFileForBrowse(path)
}

// OpenSessionFileForEdit returns a file decomposed into editable text plus the
// byte-layout details the editor cannot represent (BOM, line-ending convention,
// trailing newline), which are handed straight back to SaveSessionFileEdit so an
// unmodified file saves byte-identically.
func (a *App) OpenSessionFileForEdit(id, path string, windowIdx int, expectedRoot string) (*session.EditableFile, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	inst.BrowseRoot = resolvedRoot
	return inst.ReadFileForEdit(path)
}

// SaveFileEditResult is what a save attempt reports back.
//
// A conflict is NOT returned as an error: Wails flattens errors to a string, and
// the UI has to tell "someone else changed this file" apart from every other
// failure in order to offer overwrite/reload/keep-editing instead of a message.
type SaveFileEditResult struct {
	// Saved is false when Conflict is set; the file on disk was not touched.
	Saved bool `json:"saved"`
	// Conflict is "", "modified" or "deleted".
	Conflict string `json:"conflict,omitempty"`
	// File carries the new version and shape after a successful save, so the
	// editor can keep going without re-reading.
	File *session.EditableFile `json:"file,omitempty"`
}

// SaveSessionFileEdit writes edited text back to a file in the session's working
// directory.
//
// Gated on the project lock for the same reason terminal attaches are: a second
// application instance holding no lock must not write into a project another
// instance owns.
func (a *App) SaveSessionFileEdit(id, path, text string, shape session.FileShape, version string, overwrite bool, windowIdx int, expectedRoot, expectedProjectID string) (*SaveFileEditResult, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := validateBrowseRoot(inst, expectedRoot)
	if err != nil {
		return nil, err
	}
	// Save through the already-resolved snapshot, not through a configured
	// symlink that could be retargeted after validation.
	inst.BrowseRoot = resolvedRoot
	saved, err := inst.SaveFileForEdit(path, text, shape, version, overwrite)
	if err != nil {
		var conflict *session.SaveConflictError
		if errors.As(err, &conflict) {
			return &SaveFileEditResult{Conflict: conflict.Kind}, nil
		}
		return nil, err
	}
	return &SaveFileEditResult{Saved: true, File: saved}, nil
}

// validateBrowseRoot makes a relative edit target stable across terminal cwd
// changes. Version checking protects one file's bytes; it cannot distinguish
// two different roots that both contain the same relative path, and overwrite
// intentionally bypasses that check. Root identity therefore fails closed.
func validateBrowseRoot(inst *session.Instance, expectedRoot string) (string, error) {
	return validateRootSnapshot(inst, expectedRoot, "the tab working directory changed; reopen the file")
}

func validateRootSnapshot(inst *session.Instance, expectedRoot, message string) (string, error) {
	actualRoot := inst.Path
	if inst.BrowseRoot != "" {
		actualRoot = inst.BrowseRoot
	}
	actualAbs, actualErr := filepath.Abs(actualRoot)
	expectedAbs, expectedErr := filepath.Abs(expectedRoot)
	actualResolved, actualResolveErr := filepath.EvalSymlinks(actualAbs)
	expectedResolved, expectedResolveErr := filepath.EvalSymlinks(expectedAbs)
	if expectedRoot == "" || actualRoot == "" || actualErr != nil || expectedErr != nil ||
		actualResolveErr != nil || expectedResolveErr != nil ||
		session.CanonicalProjectPath(actualResolved) != session.CanonicalProjectPath(expectedResolved) {
		return "", fmt.Errorf("%s", message)
	}
	return actualResolved, nil
}

// ============================================================================
// Global History Search
// ============================================================================

// HistoryEntryInfo represents history entry for frontend
type HistoryEntryInfo struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Content   string `json:"content"`
	SessionID string `json:"sessionId"`
	Score     int    `json:"score"`
}

// InitHistorySearch initializes history search index
func (a *App) InitHistorySearch() error {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	return a.initHistorySearchLocked()
}

func (a *App) initHistorySearchLocked() error {
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
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	if len(query) > 1024 {
		return nil, fmt.Errorf("search query is too long")
	}
	a.historyMu.Lock()
	if a.historyIndex == nil {
		if err := a.initHistorySearchLocked(); err != nil {
			a.historyMu.Unlock()
			return nil, err
		}
	}
	index := a.historyIndex
	a.historyMu.Unlock()

	results := index.Search(query)
	infos := make([]HistoryEntryInfo, len(results))
	for i, r := range results {
		infos[i] = HistoryEntryInfo{
			ID:        r.ID,
			Agent:     string(r.Agent),
			Content:   r.Snippet,
			SessionID: r.SessionID,
			Score:     r.Score,
		}
	}
	return infos, nil
}

// GetHistoryPreview loads conversation preview
func (a *App) GetHistoryPreview(entryID string) (string, error) {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	a.historyMu.Lock()
	index := a.historyIndex
	a.historyMu.Unlock()
	if index == nil {
		return "", fmt.Errorf("history index is not loaded")
	}
	entry, ok := index.GetEntry(entryID)
	if !ok {
		return "", fmt.Errorf("history entry is no longer available")
	}
	messages, err := entry.LoadConversation()
	if err != nil {
		return "", err
	}

	// Format messages as string
	var result strings.Builder
	for _, msg := range messages {
		if result.Len()+len(msg.Role)+len(msg.Content)+6 > 4<<20 {
			return "", fmt.Errorf("conversation preview is too large")
		}
		result.WriteString("[")
		result.WriteString(msg.Role)
		result.WriteString("]: ")
		result.WriteString(msg.Content)
		result.WriteString("\n\n")
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
		result[i] = AgentSessionInfo{
			ID:          s.SessionID,
			DisplayName: resumeSessionDisplayName(s),
			Path:        path,
			Timestamp:   s.UpdatedAt.Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

func resumeSessionDisplayName(s session.AgentSession) string {
	if s.FirstPrompt != "" {
		if len(s.FirstPrompt) > 50 {
			return s.FirstPrompt[:50] + "..."
		}
		return s.FirstPrompt
	}
	if len(s.SessionID) > 8 {
		return s.SessionID[:8] + "..."
	}
	return s.SessionID
}

// ============================================================================
// Settings
// ============================================================================

// SettingsInfo represents settings for frontend
type SettingsInfo struct {
	CompactList        bool   `json:"compactList"`
	HideStatusLines    bool   `json:"hideStatusLines"`
	ShowAgentIcons     bool   `json:"showAgentIcons"`
	HideYoloBadge      bool   `json:"hideYoloBadge"`
	ShowResumeBadge    bool   `json:"showResumeBadge"`
	SplitView          bool   `json:"splitView"`
	MarkedSessionID    string `json:"markedSessionId"`
	LastSessionID      string `json:"lastSessionId"`
	MarkedWindowIdx    int    `json:"markedWindowIdx"`
	Language           string `json:"language"`
	UITheme            string `json:"uiTheme"`
	UIAccent           string `json:"uiAccent"`
	TerminalRenderer   string `json:"terminalRenderer"`
	TerminalCopyMode   string `json:"terminalCopyMode"`
	TerminalFontFamily string `json:"terminalFontFamily"`
	TerminalShell      string `json:"terminalShell"`
	// ShellChoices is what this platform offers for TerminalShell. Supplied by
	// the backend because the answer is platform-specific and the frontend has
	// no way to know which platform it is running on.
	ShellChoices       []session.ShellChoice `json:"shellChoices"`
	GitBranchDisplay   string                `json:"gitBranchDisplay"`
	DiffFlatFileList   bool                  `json:"diffFlatFileList"`
	TrashRetentionDays int                   `json:"trashRetentionDays"`
	TaskMasterEnabled  bool                  `json:"taskMasterEnabled"`
	RestoreLastSession bool                  `json:"restoreLastSession"`
	TerminalFontSize   int                   `json:"terminalFontSize"`
	AgentFontSize      int                   `json:"agentFontSize"`
	HideViewBar        bool                  `json:"hideViewBar"`
	AgentHideViewBar   bool                  `json:"agentHideViewBar"`
	HideStatusBar      bool                  `json:"hideStatusBar"`
	AgentHideStatusBar bool                  `json:"agentHideStatusBar"`
	NotifyOnWaiting    bool                  `json:"notifyOnWaiting"`
	NotifyDesktop      bool                  `json:"notifyDesktop"`
	NotifyNtfy         bool                  `json:"notifyNtfy"`
	NtfyURL            string                `json:"ntfyUrl"`
	TerminalTheme      string                `json:"terminalTheme"`
	AgentDefaultTheme  string                `json:"agentDefaultTheme"`
	// ShortcutOverrides holds only the shortcuts the user has rebound, keyed by
	// shortcut id. Passed through untouched: the frontend owns what a binding
	// looks like, because that is where key events are matched.
	ShortcutOverrides map[string]any         `json:"shortcutOverrides"`
	DiffAboveHeight   int                    `json:"diffAboveHeight"`
	DictationBuffer   *session.PanelGeometry `json:"dictationBuffer"`
	DiffSideBySide    bool                   `json:"diffSideBySide"`
	DiffHunksOnly     bool                   `json:"diffHunksOnly"`
	DiffLastFile      map[string]string      `json:"diffLastFile"`
	// Per-agent-type palette overrides ("claude" → "dracula", …) and the
	// user-defined palette used when a theme id is "custom".
	AgentTerminalThemes  map[string]string             `json:"agentTerminalThemes"`
	CustomTerminalThemes []session.CustomTerminalTheme `json:"customTerminalThemes"`
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

	// Leave the renderer empty when unset, so the frontend can apply its own
	// per-platform default (defaultTerminalRenderer): DOM on macOS/Windows,
	// where the canvas renderer drops accented characters and box drawing, and
	// canvas on Linux. Defaulting to "canvas" here overrode that for every
	// fresh install on the platforms that must not have it.
	renderer := settings.TerminalRenderer

	// Copying stays opt-in behind Shift unless asked otherwise: a plain drag
	// copying by itself surprises people who only meant to highlight something.
	copyMode := settings.TerminalCopyMode
	if copyMode == "" {
		copyMode = "shift"
	}

	// The branch reads best next to the session name, so that's the default.
	branchDisplay := settings.GitBranchDisplay
	if branchDisplay == "" {
		branchDisplay = "header"
	}

	// Default the terminal palette to the app's own scheme if unset.
	theme := settings.TerminalTheme
	if theme == "" {
		theme = "asmgr"
	}

	// The agent side has its own default, independent of the terminal one.
	agentTheme := settings.AgentDefaultTheme
	if agentTheme == "" {
		agentTheme = "asmgr"
	}

	// Migrate the legacy single custom palette into the named list so old
	// configs keep their colours after the multi-palette change.
	customThemes := settings.CustomTerminalThemes
	if len(customThemes) == 0 && len(settings.CustomTerminalTheme) > 0 {
		customThemes = []session.CustomTerminalTheme{{
			ID:     "custom:1",
			Name:   "Custom",
			Colors: settings.CustomTerminalTheme,
		}}
	}

	return &SettingsInfo{
		CompactList:          settings.CompactList,
		HideStatusLines:      settings.HideStatusLines,
		ShowAgentIcons:       settings.ShowAgentIcons,
		HideYoloBadge:        settings.HideYoloBadge,
		ShowResumeBadge:      settings.ShowResumeBadge,
		SplitView:            settings.SplitView,
		MarkedSessionID:      settings.MarkedSessionID,
		LastSessionID:        settings.LastSessionID,
		MarkedWindowIdx:      settings.MarkedWindowIdx,
		Language:             lang,
		UITheme:              settings.UITheme,
		UIAccent:             settings.UIAccent,
		TerminalRenderer:     renderer,
		TerminalShell:        settings.TerminalShell,
		ShellChoices:         session.ShellChoices(),
		TerminalCopyMode:     copyMode,
		TerminalFontFamily:   settings.TerminalFontFamily,
		GitBranchDisplay:     branchDisplay,
		DiffFlatFileList:     settings.DiffFlatFileList,
		TrashRetentionDays:   settings.TrashRetentionDays,
		TaskMasterEnabled:    settings.TaskMasterEnabled,
		RestoreLastSession:   settings.RestoreLastSession,
		TerminalFontSize:     settings.TerminalFontSize,
		AgentFontSize:        settings.AgentFontSize,
		HideViewBar:          settings.HideViewBar,
		AgentHideViewBar:     settings.AgentHideViewBar,
		HideStatusBar:        settings.HideStatusBar,
		AgentHideStatusBar:   settings.AgentHideStatusBar,
		NotifyOnWaiting:      settings.NotifyOnWaiting,
		NotifyDesktop:        settings.NotifyDesktop,
		NotifyNtfy:           settings.NotifyNtfy,
		NtfyURL:              settings.NtfyURL,
		ShortcutOverrides:    settings.ShortcutOverrides,
		DiffAboveHeight:      settings.DiffAboveHeight,
		DictationBuffer:      settings.DictationBuffer,
		DiffSideBySide:       settings.DiffSideBySide,
		DiffHunksOnly:        settings.DiffHunksOnly,
		DiffLastFile:         settings.DiffLastFile,
		TerminalTheme:        theme,
		AgentDefaultTheme:    agentTheme,
		AgentTerminalThemes:  settings.AgentTerminalThemes,
		CustomTerminalThemes: customThemes,
	}, nil
}

// Runtime settings seams keep persistence ordering testable without launching
// a real multiplexer. Production values are the session package functions.
var (
	applyRuntimeMouseCopy     = session.SetMouseCopyEnabledContext
	applyRuntimeTerminalShell = session.SetTerminalShell
)

// SaveSettings saves UI settings
func (a *App) SaveSettings(settings SettingsInfo, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	err = a.storage.UpdateSettings(func(current *session.Settings) {
		// Update only frontend-owned fields. Backend-only values (for example
		// secrets and compatibility state) must survive an ordinary UI save.
		current.CompactList = settings.CompactList
		current.HideStatusLines = settings.HideStatusLines
		current.ShowAgentIcons = settings.ShowAgentIcons
		current.HideYoloBadge = settings.HideYoloBadge
		current.ShowResumeBadge = settings.ShowResumeBadge
		current.SplitView = settings.SplitView
		current.MarkedSessionID = settings.MarkedSessionID
		current.LastSessionID = settings.LastSessionID
		current.MarkedWindowIdx = settings.MarkedWindowIdx
		current.Language = settings.Language
		current.UITheme = settings.UITheme
		current.UIAccent = settings.UIAccent
		current.TerminalRenderer = settings.TerminalRenderer
		current.TerminalCopyMode = settings.TerminalCopyMode
		current.TerminalFontFamily = settings.TerminalFontFamily
		current.TerminalShell = settings.TerminalShell
		current.GitBranchDisplay = settings.GitBranchDisplay
		current.DiffFlatFileList = settings.DiffFlatFileList
		current.TrashRetentionDays = settings.TrashRetentionDays
		current.TaskMasterEnabled = settings.TaskMasterEnabled
		current.RestoreLastSession = settings.RestoreLastSession
		current.TerminalFontSize = settings.TerminalFontSize
		current.AgentFontSize = settings.AgentFontSize
		current.HideViewBar = settings.HideViewBar
		current.AgentHideViewBar = settings.AgentHideViewBar
		current.HideStatusBar = settings.HideStatusBar
		current.AgentHideStatusBar = settings.AgentHideStatusBar
		current.TerminalTheme = settings.TerminalTheme
		current.AgentDefaultTheme = settings.AgentDefaultTheme
		current.AgentTerminalThemes = settings.AgentTerminalThemes
		current.CustomTerminalThemes = settings.CustomTerminalThemes
		current.NotifyOnWaiting = settings.NotifyOnWaiting
		current.NotifyDesktop = settings.NotifyDesktop
		current.NotifyNtfy = settings.NotifyNtfy
		current.NtfyURL = settings.NtfyURL
		current.ShortcutOverrides = settings.ShortcutOverrides
		current.DiffAboveHeight = settings.DiffAboveHeight
		current.DictationBuffer = settings.DictationBuffer
		current.DiffSideBySide = settings.DiffSideBySide
		current.DiffHunksOnly = settings.DiffHunksOnly
		current.DiffLastFile = settings.DiffLastFile
	})
	if err != nil {
		return err
	}

	// Apply process/tmux state only after the settings snapshot is durable. A
	// failed write must leave runtime state on the old persisted value. tmux is
	// also external and used to run here while Storage held s.mu; one wedged
	// server then blocked every storage call indefinitely. One shared deadline
	// bounds the complete set of global binding updates.
	applyRuntimeTerminalShell(settings.TerminalShell)
	applyCtx, cancelApply := context.WithTimeout(a.lifecycleContext(), session.TmuxCommandTimeout)
	applyRuntimeMouseCopy(applyCtx, settings.TerminalCopyMode == "select")
	cancelApply()

	// Turning the feature off has to take effect on the process too, not only
	// on the next start: a client left running from before keeps an npx child
	// alive, which is precisely what "off" is supposed to mean there isn't.
	if !settings.TaskMasterEnabled {
		stopAllTaskMasters()
	} else {
		// stopAllTaskMasters closes the start-registration gate before draining
		// children. A deliberate re-enable is the only operation that opens it
		// again; shutdown and an "off" setting therefore cannot be raced by an
		// already-running RPC.
		taskMasterMu.Lock()
		taskMasterStartsBlocked = false
		taskMasterMu.Unlock()
	}
	return nil
}

// ============================================================================
// Updater
// ============================================================================

// UpdateInfo contains update information
type UpdateInfo struct {
	Available         bool   `json:"available"`
	CurrentVersion    string `json:"currentVersion"`
	LatestVersion     string `json:"latestVersion"`
	CanAutoInstall    bool   `json:"canAutoInstall"`
	ManualInstallHint string `json:"manualInstallHint"`
	ManualInstallURL  string `json:"manualInstallURL"`
}

// GetVersion returns the current application version (for the UI/about).
func (a *App) GetVersion() string {
	return Version
}

// MultiplexerStatus describes whether the terminal multiplexer is present.
type MultiplexerStatus struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	// Hint is the install command for this platform, present only when the
	// multiplexer is missing.
	Hint string `json:"hint,omitempty"`
	// CanInstall is true where the app can install it for the user — Windows
	// only, where winget does it without elevation. Elsewhere the hint is a
	// command for the user to run themselves.
	CanInstall bool `json:"canInstall,omitempty"`
}

// GetAppLog returns the tail of this run's log, for the viewer in Settings.
// Reported straight from the file rather than buffered in memory: the log is
// written by the standard logger from every goroutine, and a second copy would
// be one more thing to keep in step.
func (a *App) GetAppLog() AppLog {
	return ReadAppLog()
}

// GetLog returns the tail of one of the logs the viewer offers. The key names
// a known log rather than a path, so the frontend cannot ask for an arbitrary
// file.
func (a *App) GetLog(key string) AppLog {
	return ReadLogAt(key)
}

// OpenAppLogFolder shows the log file in the desktop's file manager, for when
// the tail is not enough and the whole file is wanted.
func (a *App) OpenAppLogFolder() error {
	path := LogFilePath()
	if path == "" {
		return fmt.Errorf("no log file for this run")
	}
	return a.OpenFolder(filepath.Dir(path))
}

// ClearLog empties one of the offered logs, for starting a fresh recording of
// a problem that is about to be reproduced.
func (a *App) ClearLog(key string) error {
	return ClearLog(key)
}

// OpenLogFolder shows the folder holding one of the offered logs.
func (a *App) OpenLogFolder(key string) error {
	log := ReadLogAt(key)
	if log.Path == "" {
		return fmt.Errorf("no log file for %q", key)
	}
	return a.OpenFolder(filepath.Dir(log.Path))
}

// GetMultiplexerStatus reports whether the multiplexer this platform needs is
// installed, so the interface can say so before the user tries to create a
// session and is refused.
func (a *App) GetMultiplexerStatus() MultiplexerStatus {
	status := MultiplexerStatus{Name: session.MultiplexerName()}
	if err := session.CheckMultiplexer(); err != nil {
		status.Hint = session.MultiplexerInstallHint()
		status.CanInstall = session.InstallMultiplexerSupported()
		return status
	}
	status.Available = true
	status.Version = session.MultiplexerVersion()
	return status
}

// InstallMultiplexer installs the multiplexer where the platform allows it,
// returning what the package manager printed so a failure is diagnosable.
func (a *App) InstallMultiplexer() (string, error) {
	return session.InstallMultiplexer()
}

// SetTabTerminalTheme sets a tab's own colour palette. An empty id clears
// the override so the tab falls back to the agent-type palette, then the
// global one. windowIdx 0 (main window) is stored on the instance.
// SetLastWindowIndex remembers which tab was open, so the session reopens
// where the user left it. Best-effort: a failure here must never block
// switching sessions, so callers may ignore the error.
func (a *App) SetLastWindowIndex(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if windowIdx < 0 {
		return nil
	}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if inst.LastWindowIndex == windowIdx {
		return nil // no write for an unchanged value
	}
	inst.LastWindowIndex = windowIdx
	return a.storage.UpdateInstance(inst)
}

// Terminal font size bounds. Below the minimum the text is unreadable; above
// the maximum a single line no longer fits, and both are awkward to undo from
// inside the terminal itself.
const (
	MinTerminalFontSize = 8
	MaxTerminalFontSize = 32
)

// View-bar visibility per tab. Tri-state because "follow the global setting"
// and "explicitly show" are different answers when the global is "hide".
const (
	ViewBarInherit = 0
	ViewBarHidden  = 1
	ViewBarShown   = 2
)

// SetTabStatusBar overrides whether the bottom status bar shows for one tab.
// ViewBarInherit clears the override so the tab follows the global setting.
func (a *App) SetTabStatusBar(sessionID string, windowIdx int, state int, expectedProjectID string) error {
	return a.setTabBarState(sessionID, windowIdx, state, false, expectedProjectID)
}

// SetTabViewBar overrides whether the view bar shows for one tab.
// ViewBarInherit clears the override so the tab follows the global setting.
func (a *App) SetTabViewBar(sessionID string, windowIdx int, state int, expectedProjectID string) error {
	return a.setTabBarState(sessionID, windowIdx, state, true, expectedProjectID)
}

// setTabBarState stores a tri-state override for one of the two bars. They
// differ only in which field they write, so the validation and the
// main-window lookup are shared.
func (a *App) setTabBarState(sessionID string, windowIdx, state int, viewBar bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if state < ViewBarInherit || state > ViewBarShown {
		return fmt.Errorf("invalid bar state %d", state)
	}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		if viewBar {
			inst.HideViewBar = state
		} else {
			inst.HideStatusBar = state
		}
	} else {
		found := false
		for i := range inst.FollowedWindows {
			if inst.FollowedWindows[i].Index != windowIdx {
				continue
			}
			if viewBar {
				inst.FollowedWindows[i].HideViewBar = state
			} else {
				inst.FollowedWindows[i].HideStatusBar = state
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("window %d not found", windowIdx)
		}
	}
	return a.storage.UpdateInstance(inst)
}

// SetTabFontSize overrides the terminal font size for one tab. A size of 0
// clears the override so the tab follows the global setting again.
func (a *App) SetTabFontSize(sessionID string, windowIdx int, size int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	// Guard the stored value, not just the UI: a size outside this range makes
	// the terminal unusable and is hard to recover from.
	if size != 0 && (size < MinTerminalFontSize || size > MaxTerminalFontSize) {
		return fmt.Errorf("font size must be between %d and %d",
			MinTerminalFontSize, MaxTerminalFontSize)
	}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		inst.TerminalFontSize = size
	} else {
		found := false
		for i := range inst.FollowedWindows {
			if inst.FollowedWindows[i].Index == windowIdx {
				inst.FollowedWindows[i].TerminalFontSize = size
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

func (a *App) SetTabTerminalTheme(sessionID string, windowIdx int, themeID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		inst.TerminalTheme = themeID
	} else {
		found := false
		for i := range inst.FollowedWindows {
			if inst.FollowedWindows[i].Index == windowIdx {
				inst.FollowedWindows[i].TerminalTheme = themeID
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

// SetTabStatusLineVisibility stores whether a tab's status line should be
// shown in the session list. windowIdx 0 (the main window) is stored on the
// instance; followed windows carry their own flag.
func (a *App) SetTabStatusLineVisibility(sessionID string, windowIdx int, hide bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) QuickReplyTab(sessionID string, windowIdx int, action, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) CheckForUpdate() (*UpdateInfo, error) {
	current := Version
	started := time.Now()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	latest, err := updater.RefreshAvailableUpdateContext(ctx, current)
	if err != nil {
		log.Printf("[update] check failed in %s: %v", time.Since(started).Round(time.Millisecond), err)
		return nil, err
	}

	// Logged because a check that answers "nothing new" looks identical to one
	// that never ran; without this there is no way to tell them apart after
	// the fact.
	if latest == "" {
		log.Printf("[update] checked in %s: %s is current", time.Since(started).Round(time.Millisecond), current)
	} else {
		log.Printf("[update] checked in %s: %s available (running %s)",
			time.Since(started).Round(time.Millisecond), latest, current)
	}

	return &UpdateInfo{
		Available:         latest != "",
		CurrentVersion:    current,
		LatestVersion:     latest,
		CanAutoInstall:    updater.AutomaticInstallSupported(),
		ManualInstallHint: updater.ManualUpdateHint(),
		ManualInstallURL:  updater.ReleasePageURL,
	}, nil
}

// PendingUpdate returns the version an earlier check found, so the UI can flag
// it at startup without waiting for (or forcing) a new network check.
func (a *App) PendingUpdate() string {
	return updater.CachedAvailableUpdate(Version)
}

// PerformUpdate downloads and installs update
func (a *App) PerformUpdate(version string) error {
	ctx, done, err := a.beginUpdateInstall()
	if err != nil {
		return err
	}
	defer done()

	if err := updater.DownloadAndInstallContext(ctx, version, a.withCriticalUpdateInstall); err != nil {
		return err
	}
	// Installed: stop advertising it. The running process is still the old
	// binary, so Version can't tell us this on its own.
	updater.ClearAvailableUpdate()
	return nil
}

func (a *App) beginUpdateInstall() (context.Context, func(), error) {
	a.updateInstallMu.Lock()
	defer a.updateInstallMu.Unlock()
	if a.updateShuttingDown {
		return nil, nil, fmt.Errorf("application is shutting down")
	}
	if a.updateInstalling {
		return nil, nil, fmt.Errorf("an update is already in progress")
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.updateInstalling = true
	a.updateInstallCancel = cancel
	return ctx, func() {
		cancel()
		a.updateInstallMu.Lock()
		a.updateInstalling = false
		a.updateInstallCancel = nil
		a.updateInstallMu.Unlock()
	}, nil
}

func (a *App) withCriticalUpdateInstall(action func() error) error {
	a.updateInstallMu.Lock()
	if a.updateShuttingDown {
		a.updateInstallMu.Unlock()
		return context.Canceled
	}
	a.updateCriticalWG.Add(1)
	a.updateInstallMu.Unlock()
	defer a.updateCriticalWG.Done()
	return action()

}

func (a *App) stopUpdateInstall() {
	a.updateInstallMu.Lock()
	a.updateShuttingDown = true
	if a.updateInstallCancel != nil {
		a.updateInstallCancel()
	}
	a.updateInstallMu.Unlock()
	// Downloading and staging are safe to abandon and observe the cancellation
	// above. Only a final executable/bundle/package transaction must finish
	// before the process is allowed to exit.
	a.updateCriticalWG.Wait()
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
	agents := []AgentInfo{
		{Type: "claude", Name: "Claude", Icon: "🤖"},
		{Type: "gemini", Name: "Gemini", Icon: "💎"},
		{Type: "aider", Name: "Aider", Icon: "🔧"},
		{Type: "codex", Name: "Codex", Icon: "📦"},
		{Type: "amazonq", Name: "Amazon Q", Icon: "🦜"},
		{Type: "opencode", Name: "OpenCode", Icon: "💻"},
		{Type: "custom", Name: "Custom", Icon: "⚙️"},
		{Type: "terminal", Name: "Terminal", Icon: "🖥️"},
	}

	// The capabilities come from the agent configuration rather than being
	// written out again here. Kept as a second list, the two drifted: Codex
	// gained a fork subcommand and this still said it could not fork, so the
	// button stayed hidden for it.
	for at := range agents {
		config, ok := session.AgentConfigs[session.AgentType(agents[at].Type)]
		if !ok {
			continue
		}
		agents[at].SupportsResume = config.SupportsResume
		agents[at].SupportsAutoYes = config.SupportsAutoYes
		agents[at].SupportsFork = config.ForkFlag != ""
	}
	return agents
}

// ============================================================================
// Task Management
// ============================================================================

// TaskInfo represents a task for the frontend
type TaskInfo struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Details      string        `json:"details,omitempty"`
	Status       string        `json:"status"`
	Priority     string        `json:"priority"`
	Tags         []string      `json:"tags"`
	Subtasks     []SubtaskInfo `json:"subtasks"`
	Dependencies []string      `json:"dependencies"`
	CreatedAt    string        `json:"createdAt"`
	UpdatedAt    string        `json:"updatedAt"`
	CompletedAt  *string       `json:"completedAt,omitempty"`
	// DueAt is RFC 3339, or nil when the task has no deadline. Kept a pointer
	// rather than an empty string so the frontend can tell "no deadline" from
	// a value it failed to parse.
	DueAt *string `json:"dueAt,omitempty"`
	// SessionID ties the task to one session, so closing it can warn about
	// what is still outstanding. Empty means the task belongs to the project.
	SessionID string `json:"sessionId,omitempty"`
}

// SubtaskInfo represents a subtask for the frontend
type SubtaskInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Status      string `json:"status"`
	Done        bool   `json:"done"`
	CreatedAt   string `json:"createdAt"`
}

// DeletedTaskSnapshot is the provider-neutral shape captured by the frontend
// before deletion. RestoreDeletedTask consumes it in one backend transaction,
// preserving IDs so reverse dependencies remain valid.
type DeletedTaskSnapshot struct {
	ID           string                   `json:"id"`
	Title        string                   `json:"title"`
	Description  string                   `json:"description"`
	Details      string                   `json:"details,omitempty"`
	Status       string                   `json:"status"`
	Priority     string                   `json:"priority"`
	Tags         []string                 `json:"tags"`
	Subtasks     []DeletedSubtaskSnapshot `json:"subtasks"`
	Dependencies []string                 `json:"dependencies"`
	Complexity   *int                     `json:"complexity,omitempty"`
	CreatedAt    string                   `json:"createdAt,omitempty"`
	UpdatedAt    string                   `json:"updatedAt,omitempty"`
	CompletedAt  string                   `json:"completedAt,omitempty"`
	DueAt        string                   `json:"dueAt,omitempty"`
	SessionID    string                   `json:"sessionId,omitempty"`
	TestStrategy string                   `json:"testStrategy,omitempty"`
	RawJSON      string                   `json:"rawJson,omitempty"`
}

type DeletedSubtaskSnapshot struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Status       string   `json:"status,omitempty"`
	Details      string   `json:"details,omitempty"`
	Done         bool     `json:"done,omitempty"`
	CreatedAt    string   `json:"createdAt,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	ParentID     string   `json:"parentId,omitempty"`
	TestStrategy string   `json:"testStrategy,omitempty"`
	RawJSON      string   `json:"rawJson,omitempty"`
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

	projectPath := session.CanonicalProjectPath(sess.Path)
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

func reloadTaskManagerCache() error {
	taskManagerMu.RLock()
	managers := make([]*session.TaskManager, 0, len(taskManagerCache))
	for _, manager := range taskManagerCache {
		managers = append(managers, manager)
	}
	taskManagerMu.RUnlock()
	var reloadErr error
	for _, manager := range managers {
		if err := manager.Load(); err != nil {
			reloadErr = errors.Join(reloadErr, fmt.Errorf("failed to reload restored task store: %w", err))
		}
	}
	return reloadErr
}

// convertTask converts session.Task to TaskInfo for frontend
func convertTask(t session.Task) TaskInfo {
	subtasks := make([]SubtaskInfo, len(t.Subtasks))
	for i, st := range t.Subtasks {
		status := st.Status
		if status == "" {
			status = session.TaskStatusBacklog
		}
		if st.Done {
			status = session.TaskStatusDone
		}
		subtasks[i] = SubtaskInfo{
			ID:          st.ID,
			Title:       st.Title,
			Description: st.Description,
			Details:     st.Details,
			Status:      string(status),
			Done:        st.Done,
			CreatedAt:   st.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
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
		Details:      t.Details,
		Status:       string(t.Status),
		Priority:     string(t.Priority),
		Tags:         tags,
		Subtasks:     subtasks,
		Dependencies: deps,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		SessionID:    t.SessionID,
	}

	if t.DueAt != nil {
		dueStr := t.DueAt.Format("2006-01-02T15:04:05Z07:00")
		info.DueAt = &dueStr
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
func (a *App) CreateTask(sessionID, title, description, priority string, tags []string, expectedProjectID string) (*TaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return nil, err
	}

	if tags == nil {
		tags = []string{}
	}

	task, err := tm.CreateTaskForSession(title, description, session.TaskPriority(priority), tags, sessionID)
	if err != nil {
		return nil, err
	}

	info := convertTask(*task)
	return &info, nil
}

// UpdateTask updates an existing task
func (a *App) UpdateTask(sessionID, taskID string, updates map[string]interface{}, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateTask(taskID, updates)
}

// DeleteTask deletes a task
func (a *App) DeleteTask(sessionID, taskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.DeleteTask(taskID)
}

// MoveTask changes the status of a task
func (a *App) MoveTask(sessionID, taskID, newStatus, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.MoveTask(taskID, session.TaskStatus(newStatus))
}

// AddSubtask adds a subtask to a task
func (a *App) AddSubtask(sessionID, taskID, title, expectedProjectID string) (*SubtaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
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
func (a *App) ToggleSubtask(sessionID, taskID, subtaskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.ToggleSubtask(taskID, subtaskID)
}

// DeleteSubtask removes a subtask
func (a *App) DeleteSubtask(sessionID, taskID, subtaskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	return tm.DeleteSubtask(taskID, subtaskID)
}

// RestoreDeletedTask restores a full deleted-task snapshot through the
// selected provider without allocating a replacement ID.
func (a *App) RestoreDeletedTask(sessionID, provider string, snapshot DeletedTaskSnapshot, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if snapshot.ID == "" {
		return fmt.Errorf("restored task has no ID")
	}

	switch provider {
	case "local":
		tm, err := a.getTaskManager(sessionID)
		if err != nil {
			return err
		}
		return tm.RestoreTask(localTaskFromSnapshot(snapshot))
	case "mcp":
		instance, err := a.storage.GetInstance(sessionID)
		if err != nil {
			return err
		}
		if instance.Path == "" {
			return fmt.Errorf("error.sessionNoPath")
		}
		// Restoring a file snapshot does not require starting npx/the MCP server.
		// Undo must remain available if the provider exited after deletion.
		return mcp.NewTaskMaster(instance.Path).RestoreTask(mcpTaskFromSnapshot(snapshot))
	default:
		return fmt.Errorf("unknown task provider %q", provider)
	}
}

// RestoreDeletedSubtask restores a complete subtask snapshot to its original
// parent and provider. The provider is captured when deletion happens.
func (a *App) RestoreDeletedSubtask(sessionID, provider, taskID string, snapshot DeletedSubtaskSnapshot, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if taskID == "" || snapshot.ID == "" {
		return fmt.Errorf("restored subtask has no parent or ID")
	}

	switch provider {
	case "local":
		tm, err := a.getTaskManager(sessionID)
		if err != nil {
			return err
		}
		return tm.RestoreSubtask(taskID, localSubtaskFromSnapshot(snapshot))
	case "mcp":
		instance, err := a.storage.GetInstance(sessionID)
		if err != nil {
			return err
		}
		if instance.Path == "" {
			return fmt.Errorf("error.sessionNoPath")
		}
		return mcp.NewTaskMaster(instance.Path).RestoreSubtask(taskID, mcpSubtaskFromSnapshot(snapshot))
	default:
		return fmt.Errorf("unknown task provider %q", provider)
	}
}

func localTaskFromSnapshot(snapshot DeletedTaskSnapshot) session.Task {
	status := snapshot.Status
	if status == "" || status == "pending" {
		status = string(session.TaskStatusBacklog)
	}
	task := session.Task{
		ID:           snapshot.ID,
		Title:        snapshot.Title,
		Description:  snapshot.Description,
		Details:      snapshot.Details,
		Status:       session.TaskStatus(status),
		Priority:     session.TaskPriority(snapshot.Priority),
		Tags:         append([]string(nil), snapshot.Tags...),
		Dependencies: append([]string(nil), snapshot.Dependencies...),
		SessionID:    snapshot.SessionID,
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, snapshot.CreatedAt)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
	if completed, err := time.Parse(time.RFC3339Nano, snapshot.CompletedAt); err == nil {
		task.CompletedAt = &completed
	}
	if due, err := time.Parse(time.RFC3339Nano, snapshot.DueAt); err == nil {
		task.DueAt = &due
	}
	task.Subtasks = make([]session.Subtask, 0, len(snapshot.Subtasks))
	for _, subtask := range snapshot.Subtasks {
		created, _ := time.Parse(time.RFC3339Nano, subtask.CreatedAt)
		status := subtask.Status
		if status == "" || status == "pending" {
			status = string(session.TaskStatusBacklog)
		}
		done := subtask.Done || status == string(session.TaskStatusDone)
		if done {
			status = string(session.TaskStatusDone)
		}
		task.Subtasks = append(task.Subtasks, session.Subtask{
			ID:          subtask.ID,
			Title:       subtask.Title,
			Description: subtask.Description,
			Details:     subtask.Details,
			Status:      session.TaskStatus(status),
			Done:        done,
			CreatedAt:   created,
		})
	}
	return task
}

func localSubtaskFromSnapshot(snapshot DeletedSubtaskSnapshot) session.Subtask {
	status := snapshot.Status
	if status == "" || status == "pending" {
		status = string(session.TaskStatusBacklog)
	}
	subtask := session.Subtask{
		ID: snapshot.ID, Title: snapshot.Title, Description: snapshot.Description,
		Details: snapshot.Details, Status: session.TaskStatus(status), Done: snapshot.Done,
	}
	if parsed, err := time.Parse(time.RFC3339Nano, snapshot.CreatedAt); err == nil {
		subtask.CreatedAt = parsed
	}
	return subtask
}

func mcpTaskFromSnapshot(snapshot DeletedTaskSnapshot) mcp.Task {
	task := mcp.Task{
		ID:           snapshot.ID,
		Title:        snapshot.Title,
		Description:  snapshot.Description,
		Details:      snapshot.Details,
		Status:       snapshot.Status,
		Priority:     snapshot.Priority,
		Tags:         append([]string(nil), snapshot.Tags...),
		Dependencies: append([]string(nil), snapshot.Dependencies...),
		Complexity:   snapshot.Complexity,
		CreatedAt:    snapshot.CreatedAt,
		UpdatedAt:    snapshot.UpdatedAt,
		CompletedAt:  snapshot.CompletedAt,
		DueAt:        snapshot.DueAt,
		SessionID:    snapshot.SessionID,
		TestStrategy: snapshot.TestStrategy,
		RawJSON:      snapshot.RawJSON,
		Subtasks:     make([]mcp.Subtask, 0, len(snapshot.Subtasks)),
	}
	for _, subtask := range snapshot.Subtasks {
		task.Subtasks = append(task.Subtasks, mcp.Subtask{
			ID:           subtask.ID,
			Title:        subtask.Title,
			Description:  subtask.Description,
			Status:       subtask.Status,
			Details:      subtask.Details,
			CreatedAt:    subtask.CreatedAt,
			Dependencies: append([]string(nil), subtask.Dependencies...),
			ParentID:     subtask.ParentID,
			TestStrategy: subtask.TestStrategy,
			RawJSON:      subtask.RawJSON,
		})
	}
	return task
}

func mcpSubtaskFromSnapshot(snapshot DeletedSubtaskSnapshot) mcp.Subtask {
	status := snapshot.Status
	if status == "" {
		if snapshot.Done {
			status = "done"
		} else {
			status = "pending"
		}
	}
	return mcp.Subtask{
		ID: snapshot.ID, Title: snapshot.Title, Description: snapshot.Description,
		Details: snapshot.Details, Status: status, CreatedAt: snapshot.CreatedAt,
		Dependencies: append([]string(nil), snapshot.Dependencies...), ParentID: snapshot.ParentID,
		TestStrategy: snapshot.TestStrategy, RawJSON: snapshot.RawJSON,
	}
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
func (a *App) SendTaskToAgent(sessionID, taskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskManager(sessionID)
	if err != nil {
		return err
	}

	prompt, err := tm.FormatTaskForAgent(taskID)
	if err != nil {
		return err
	}

	log.Printf("[TaskManager] SendToAgent taskID=%s", taskID)
	// Send the prompt to the active terminal
	return a.sendPrompt(sessionID, prompt)
}

// ============================================================================
// Task Master MCP Integration
// ============================================================================

// taskMasterCache stores TaskMaster instances per project
var taskMasterCache = make(map[string]*mcp.TaskMaster)
var taskMasterMu sync.RWMutex

type taskMasterStart struct {
	done   chan struct{}
	cancel context.CancelFunc
	tm     *mcp.TaskMaster
	err    error
}

var taskMasterStarts = make(map[string]*taskMasterStart)
var taskMasterStartsBlocked bool
var taskMasterDrainEpoch uint64

// taskMasterEnabled reports whether the user opted into the Task Master panel.
// Read from storage on every call rather than cached: the setting can be
// toggled while the app runs, and a stale "on" would let npx fire after the
// user turned the feature back off.
func (a *App) taskMasterEnabled() bool {
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil || settings == nil {
		return false // unreadable settings are not consent
	}
	return settings.TaskMasterEnabled
}

// getTaskMasterMCP returns or creates a TaskMaster MCP client for a project.
//
// Every exported TaskMaster* method goes through here, so this is the one place
// the opt-in has to be enforced: starting a client runs `npx task-master-ai`,
// which INSTALLS the package if it is missing. Refusing before that call is
// what keeps a machine whose owner never enabled the feature untouched.
func (a *App) getTaskMasterMCP(sessionID string) (*mcp.TaskMaster, error) {
	if !a.taskMasterEnabled() {
		return nil, fmt.Errorf("error.taskMasterDisabled")
	}

	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	projectPath := taskMasterProjectPath(sess.Path)
	if projectPath == "" {
		return nil, fmt.Errorf("error.sessionNoPath")
	}

	for {
		taskMasterMu.RLock()
		blocked := taskMasterStartsBlocked
		tm := taskMasterCache[projectPath]
		starting := taskMasterStarts[projectPath]
		taskMasterMu.RUnlock()
		if blocked {
			return nil, fmt.Errorf("error.taskMasterDisabled")
		}
		if tm != nil && tm.IsRunning() {
			return tm, nil
		}
		if starting != nil {
			<-starting.done
			if starting.err != nil {
				return nil, starting.err
			}
			continue
		}

		taskMasterMu.Lock()
		if taskMasterStartsBlocked {
			taskMasterMu.Unlock()
			return nil, fmt.Errorf("error.taskMasterDisabled")
		}
		if tm = taskMasterCache[projectPath]; tm != nil && tm.IsRunning() {
			taskMasterMu.Unlock()
			return tm, nil
		}
		if starting = taskMasterStarts[projectPath]; starting != nil {
			taskMasterMu.Unlock()
			<-starting.done
			if starting.err != nil {
				return nil, starting.err
			}
			continue
		}
		stale := taskMasterCache[projectPath]
		delete(taskMasterCache, projectPath)
		startCtx, cancelStart := context.WithCancel(context.Background())
		starting = &taskMasterStart{done: make(chan struct{}), cancel: cancelStart}
		candidate := mcp.NewTaskMaster(projectPath)
		starting.tm = candidate
		taskMasterStarts[projectPath] = starting
		taskMasterMu.Unlock()

		// Process shutdown/startup may block; only callers for this same project
		// wait on starting.done. Other projects never queue behind it.
		if stale != nil {
			_ = stale.Stop()
		}
		startErr := candidate.StartContext(startCtx)
		if startErr != nil {
			cancelStart()
			startErr = fmt.Errorf("failed to start Task Master: %w", startErr)
		}

		taskMasterMu.Lock()
		starting.err = startErr
		if startErr == nil {
			taskMasterCache[projectPath] = candidate
		}
		delete(taskMasterStarts, projectPath)
		close(starting.done)
		taskMasterMu.Unlock()
		if startErr != nil {
			return nil, startErr
		}
		return candidate, nil
	}
}

func taskMasterProjectPath(projectPath string) string {
	return session.CanonicalProjectPath(projectPath)
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
	UpdatedAt    string           `json:"updatedAt,omitempty"`
	CompletedAt  string           `json:"completedAt,omitempty"`
	DueAt        string           `json:"dueAt,omitempty"`
	SessionID    string           `json:"sessionId,omitempty"`
	TestStrategy string           `json:"testStrategy,omitempty"`
	RawJSON      string           `json:"rawJson,omitempty"`
}

// MCPSubtaskInfo represents a subtask for the frontend
type MCPSubtaskInfo struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Status       string   `json:"status"`
	Details      string   `json:"details,omitempty"`
	CreatedAt    string   `json:"createdAt,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	ParentID     string   `json:"parentId,omitempty"`
	TestStrategy string   `json:"testStrategy,omitempty"`
	RawJSON      string   `json:"rawJson,omitempty"`
}

// convertMCPTask converts mcp.Task to MCPTaskInfo
func convertMCPTask(t mcp.Task) MCPTaskInfo {
	subtasks := make([]MCPSubtaskInfo, len(t.Subtasks))
	for i, st := range t.Subtasks {
		subtasks[i] = MCPSubtaskInfo{
			ID:           st.ID,
			Title:        st.Title,
			Description:  st.Description,
			Status:       st.Status,
			Details:      st.Details,
			CreatedAt:    st.CreatedAt,
			Dependencies: append([]string(nil), st.Dependencies...),
			ParentID:     st.ParentID,
			TestStrategy: st.TestStrategy,
			RawJSON:      st.RawJSON,
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
		Details:      t.Details,
		Status:       t.Status,
		Priority:     t.Priority,
		Tags:         tags,
		Subtasks:     subtasks,
		Dependencies: deps,
		Complexity:   t.Complexity,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
		CompletedAt:  t.CompletedAt,
		DueAt:        t.DueAt,
		SessionID:    t.SessionID,
		TestStrategy: t.TestStrategy,
		RawJSON:      t.RawJSON,
	}
}

// TaskMasterStatus returns the status of Task Master for a session
func (a *App) TaskMasterStatus(sessionID, expectedProjectID string) map[string]interface{} {
	result := map[string]interface{}{
		"initialized": false,
		"running":     false,
		"error":       nil,
	}
	release, guardErr := a.beginExpectedProjectReadWithSideEffects(expectedProjectID)
	if guardErr != nil {
		result["error"] = guardErr.Error()
		return result
	}
	defer release()

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
func (a *App) TaskMasterInit(sessionID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.InitializeProject(false)
}

// TaskMasterParsePRD parses a PRD file into tasks
func (a *App) TaskMasterParsePRD(sessionID, prdContent string, numTasks int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
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
func (a *App) TaskMasterGetTasks(sessionID, status, expectedProjectID string) ([]MCPTaskInfo, error) {
	release, err := a.beginExpectedProjectReadWithSideEffects(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer release()
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
func (a *App) TaskMasterGetTask(sessionID, taskID, expectedProjectID string) (*MCPTaskInfo, error) {
	release, err := a.beginExpectedProjectReadWithSideEffects(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer release()
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
func (a *App) TaskMasterNextTask(sessionID, expectedProjectID string) (*MCPTaskInfo, error) {
	release, err := a.beginExpectedProjectReadWithSideEffects(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer release()
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
func (a *App) TaskMasterSetStatus(sessionID, taskID, status, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.SetTaskStatus(taskID, status)
}

// TaskMasterAddTask adds a new task
func (a *App) TaskMasterAddTask(sessionID, prompt string, research bool, priority, expectedProjectID string) (*MCPTaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
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
func (a *App) TaskMasterAddManualTask(sessionID, title, description, details, priority, expectedProjectID string) (*MCPTaskInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()
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
func (a *App) TaskMasterUpdateTask(sessionID, taskID, prompt string, research bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateTask(taskID, prompt, research)
}

// TaskMasterUpdateSubtask updates a subtask with notes
func (a *App) TaskMasterUpdateSubtask(sessionID, subtaskID, prompt, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateSubtask(subtaskID, prompt)
}

// TaskMasterExpandTask expands a task into subtasks
func (a *App) TaskMasterExpandTask(sessionID, taskID string, research, force bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ExpandTask(taskID, research, force, 0)
}

// TaskMasterExpandAll expands all eligible tasks
func (a *App) TaskMasterExpandAll(sessionID string, research bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ExpandAllTasks(research, false)
}

// TaskMasterAnalyzeComplexity analyzes task complexity
func (a *App) TaskMasterAnalyzeComplexity(sessionID string, research bool, expectedProjectID string) (string, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return "", err
	}
	defer done()
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
func (a *App) TaskMasterRemoveTask(sessionID, taskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveTask(taskID)
}

// TaskMasterSendToAgent sends a task as a prompt to the agent
func (a *App) TaskMasterSendToAgent(sessionID, taskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	task, err := tm.GetTask(taskID)
	if err != nil {
		return err
	}

	prompt := mcp.FormatTaskForPrompt(task)
	log.Printf("[TaskMaster] SendToAgent taskID=%s", taskID)
	return a.sendPrompt(sessionID, prompt)
}

// StopTaskMaster stops the Task Master MCP server for a project
func (a *App) StopTaskMaster(sessionID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if sess.Path == "" {
		return fmt.Errorf("error.sessionNoPath")
	}
	projectPath := taskMasterProjectPath(sess.Path)

	for {
		taskMasterMu.Lock()
		starting := taskMasterStarts[projectPath]
		if starting == nil {
			tm := taskMasterCache[projectPath]
			delete(taskMasterCache, projectPath)
			taskMasterMu.Unlock()
			if tm != nil {
				return tm.Stop()
			}
			return nil
		}
		taskMasterMu.Unlock()
		// "Stop" must cancel startup too. Waiting passively here left the RPC
		// blocked through npx readiness and initialize/tools-list timeouts.
		if starting.cancel != nil {
			starting.cancel()
		}
		if starting.tm != nil {
			_ = starting.tm.Stop()
		}
		<-starting.done
	}
}

// TaskMasterAddSubtask adds a subtask to a task
func (a *App) TaskMasterAddSubtask(sessionID, taskID, title, description, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	_, err = tm.AddSubtask(taskID, title, description)
	return err
}

// TaskMasterRemoveSubtask removes a specific subtask
func (a *App) TaskMasterRemoveSubtask(sessionID, subtaskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveSubtask(subtaskID)
}

// TaskMasterClearSubtasks removes all subtasks from a task
func (a *App) TaskMasterClearSubtasks(sessionID, taskID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.ClearSubtasks(taskID)
}

// TaskMasterSetSubtaskStatus sets the status of a subtask
func (a *App) TaskMasterSetSubtaskStatus(sessionID, subtaskID, status, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.SetSubtaskStatus(subtaskID, status)
}

// TaskMasterUpdateTaskDirect updates a task with direct field values (no AI).
// Modifies the tasks.json file directly instead of using MCP to avoid slow AI calls.
//
// The only TaskMaster method that bypasses getTaskMasterMCP, so it needs its own
// copy of the opt-in check. It spawns nothing, but it does write into the
// project's .taskmaster directory, which a disabled feature has no business
// touching either.
func (a *App) TaskMasterUpdateTaskDirect(sessionID, taskID, title, description, details, priority, dueAt, taskSessionID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	if !a.taskMasterEnabled() {
		return fmt.Errorf("error.taskMasterDisabled")
	}

	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	projectPath := sess.Path
	if projectPath == "" {
		return fmt.Errorf("error.sessionNoPath")
	}

	tasksFile := filepath.Join(projectPath, ".taskmaster", "tasks", "tasks.json")
	return updateTaskMasterFileDirect(tasksFile, taskID, title, description, details, priority, dueAt, taskSessionID)
}

// updateTaskMasterFileDirect applies every field from the edit dialog to one
// Task Master snapshot and replaces the file once. Splitting deadline/session
// fields into the local task store made an MCP edit partially succeed and then
// report "task not found" from a different provider.
func updateTaskMasterFileDirect(tasksFile, taskID, title, description, details, priority, dueAt, taskSessionID string) error {
	return mcp.MutateTaskMasterFile(tasksFile, func(root map[string]interface{}) error {
		context, err := directEditTaskMasterContext(root, taskID)
		if err != nil {
			return err
		}
		tasks := context["tasks"].([]interface{})
		for _, rawTask := range tasks {
			task, ok := rawTask.(map[string]interface{})
			if !ok || taskMasterJSONID(task["id"]) != taskID {
				continue
			}
			task["title"] = title
			task["description"] = description
			task["details"] = details
			task["priority"] = priority
			if dueAt == "" {
				delete(task, "dueAt")
			} else {
				task["dueAt"] = dueAt
			}
			if taskSessionID == "" {
				delete(task, "sessionId")
			} else {
				task["sessionId"] = taskSessionID
			}
			return nil
		}
		return fmt.Errorf("task %s disappeared from selected context", taskID)
	})
}

func directEditTaskMasterContext(root map[string]interface{}, taskID string) (map[string]interface{}, error) {
	if master, ok := root["master"].(map[string]interface{}); ok && taskMasterContextContains(master, taskID) {
		return master, nil
	}

	matches := make([]map[string]interface{}, 0, 1)
	for key, rawContext := range root {
		if key == "master" {
			continue
		}
		context, ok := rawContext.(map[string]interface{})
		if ok && taskMasterContextContains(context, taskID) {
			matches = append(matches, context)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("task %s not found in tasks.json", taskID)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("task %s is ambiguous across %d Task Master contexts", taskID, len(matches))
	}
}

func taskMasterContextContains(context map[string]interface{}, taskID string) bool {
	tasks, ok := context["tasks"].([]interface{})
	if !ok {
		return false
	}
	for _, rawTask := range tasks {
		if task, ok := rawTask.(map[string]interface{}); ok && taskMasterJSONID(task["id"]) == taskID {
			return true
		}
	}
	return false
}

func taskMasterJSONID(value interface{}) string {
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	return fmt.Sprint(value)
}

// TaskMasterAddDependency adds a dependency to a task
func (a *App) TaskMasterAddDependency(sessionID, taskID, dependsOnID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.AddDependency(taskID, dependsOnID)
}

// TaskMasterRemoveDependency removes a dependency from a task
func (a *App) TaskMasterRemoveDependency(sessionID, taskID, dependsOnID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.RemoveDependency(taskID, dependsOnID)
}
