package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/mcp"
	"asmgr-desktop/session"
	"asmgr-desktop/updater"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds the application state
type App struct {
	ctx          context.Context
	storage      *session.Storage
	historyIndex *session.HistoryIndex
	ptys         map[string]*ptySession
	ptyMu        sync.RWMutex
	termServer   *TerminalServer
	dictation    *DictationService
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

	// Initialize storage
	storage, err := session.NewStorage()
	if err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to initialize storage: %v", err))
		return
	}
	a.storage = storage

	// Start WebSocket terminal server for low-latency terminal I/O
	a.termServer = NewTerminalServer(storage, 9753)
	if err := a.termServer.Start(); err != nil {
		runtime.LogError(ctx, fmt.Sprintf("Failed to start terminal server: %v", err))
	}

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

	// Start preview polling in background
	go a.startPreviewPolling()
}

// IsDevMode returns whether the app is running in dev mode
func (a *App) IsDevMode() bool {
	return isDevMode
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// Close all PTY sessions
	a.ptyMu.Lock()
	for id, ps := range a.ptys {
		if ps.cancel != nil {
			ps.cancel()
		}
		if ps.ptmx != nil {
			ps.ptmx.Close()
		}
		delete(a.ptys, id)
	}
	a.ptyMu.Unlock()

	// Shutdown dictation service
	if a.dictation != nil {
		a.dictation.Shutdown()
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

// SelectProject switches to a project
func (a *App) SelectProject(id string) error {
	return a.storage.SetActiveProject(id)
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
	// Save current project
	currentProject := a.storage.GetActiveProjectID()

	// Switch to target project temporarily
	if err := a.storage.SetActiveProject(projectID); err != nil {
		return nil, err
	}

	// Load sessions from that project
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		a.storage.SetActiveProject(currentProject)
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

	// Switch back to current project
	a.storage.SetActiveProject(currentProject)

	return result, nil
}

// ImportSessions imports selected sessions from another project
func (a *App) ImportSessions(sourceProjectID string, sessionIDs []string) (int, error) {
	// Save current project
	currentProject := a.storage.GetActiveProjectID()

	// Switch to source project
	if err := a.storage.SetActiveProject(sourceProjectID); err != nil {
		return 0, err
	}

	// Load sessions from source project
	sourceInstances, sourceGroups, err := a.storage.LoadAll()
	if err != nil {
		a.storage.SetActiveProject(currentProject)
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

	// Switch to current project
	if err := a.storage.SetActiveProject(currentProject); err != nil {
		return 0, err
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
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Path            string                   `json:"path"`
	Status          string                   `json:"status"`
	Agent           string                   `json:"agent"`
	Color           string                   `json:"color"`
	BgColor         string                   `json:"bgColor"`
	FullRowColor    bool                     `json:"fullRowColor"`
	GroupID         string                   `json:"groupId"`
	AutoYes         bool                     `json:"autoYes"`
	Notes           string                   `json:"notes"`
	Favorite        bool                     `json:"favorite"`
	ResumeSessionID string                   `json:"resumeSessionId"`
	FollowedWindows []session.FollowedWindow `json:"followedWindows"`
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

func (a *App) instanceToSessionInfo(inst *session.Instance) SessionInfo {
	return SessionInfo{
		ID:              inst.ID,
		Name:            inst.Name,
		Path:            inst.Path,
		Status:          string(inst.Status),
		Agent:           string(inst.Agent),
		Color:           inst.Color,
		BgColor:         inst.BgColor,
		FullRowColor:    inst.FullRowColor,
		GroupID:         inst.GroupID,
		AutoYes:         inst.AutoYes,
		Notes:           inst.Notes,
		Favorite:        inst.Favorite,
		ResumeSessionID: inst.ResumeSessionID,
		FollowedWindows: inst.FollowedWindows,
	}
}

// CreateSession creates a new session
func (a *App) CreateSession(name, path string, agent string, autoYes bool) (*SessionInfo, error) {
	inst, err := session.NewInstance(name, path, autoYes, session.AgentType(agent))
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
	if err := inst.Start(); err != nil {
		return err
	}
	return a.storage.UpdateInstance(inst)
}

// StartSessionWithResume starts a session with resume
func (a *App) StartSessionWithResume(id, resumeID string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
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
		if err := inst.Stop(); err != nil {
			return fmt.Errorf("failed to stop session for YOLO toggle: %w", err)
		}
		// Brief pause for tmux cleanup
		time.Sleep(500 * time.Millisecond)
		if err := inst.Start(); err != nil {
			return fmt.Errorf("failed to restart session after YOLO toggle: %w", err)
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
		return fmt.Errorf("session not found")
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
		return fmt.Errorf("session not found")
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
		return nil, fmt.Errorf("fork is only supported for Claude sessions")
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
	newInst, err := session.NewInstance(name, origInst.Path, origInst.AutoYes, session.AgentClaude)
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
	return fmt.Errorf("group not found")
}

// ============================================================================
// Tabs (Multi-window support)
// ============================================================================

// CreateTab creates a new tab in session
func (a *App) CreateTab(sessionID string, isAgent bool, agent string, name string) error {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}

	if isAgent {
		agentType := session.AgentType(agent)
		_, err := inst.NewAgentWindow(name, agentType, "")
		if err != nil {
			return err
		}
	} else {
		if err := inst.NewWindowWithName(name); err != nil {
			return err
		}
	}
	return a.storage.UpdateInstance(inst)
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
	return fmt.Errorf("window not found")
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
	return fmt.Errorf("window not found")
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

// SidebarUpdate contains combined activity and status line data
type SidebarUpdate struct {
	Activities  map[string]string `json:"activities"`
	StatusLines map[string]string `json:"statusLines"`
}

// GetSidebarUpdates returns activity and status line data in one call (single LoadAll)
func (a *App) GetSidebarUpdates() SidebarUpdate {
	result := SidebarUpdate{
		Activities:  make(map[string]string),
		StatusLines: make(map[string]string),
	}

	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return result
	}

	for _, inst := range instances {
		if inst.Status == session.StatusRunning {
			// Activity detection
			activity := inst.DetectAggregatedActivity()
			activityStr := "idle"
			switch activity {
			case session.ActivityBusy:
				activityStr = "busy"
			case session.ActivityWaiting:
				activityStr = "waiting"
			}
			result.Activities[inst.ID] = activityStr

			// Status line
			line := inst.GetLastLine()
			line = session.StripANSI(line)
			line = strings.ReplaceAll(line, "\n", " ")
			line = strings.ReplaceAll(line, "\r", "")
			line = strings.TrimSpace(line)
			if len(line) > 100 {
				line = line[:97] + "..."
			}
			result.StatusLines[inst.ID] = line
		}
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

// startPreviewPolling polls sessions and emits updates
func (a *App) startPreviewPolling() {
	// Preview polling will be implemented with events
	// For now, frontend will poll GetPreview
}

// GetTerminalWSPort returns the WebSocket terminal server port
func (a *App) GetTerminalWSPort() int {
	if a.termServer != nil {
		return a.termServer.GetPort()
	}
	return 9753
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

	// Check if already attached
	a.ptyMu.RLock()
	if _, exists := a.ptys[ptyID]; exists {
		a.ptyMu.RUnlock()
		return ptyID, nil
	}
	a.ptyMu.RUnlock()

	// Get tmux session name
	tmuxSession := inst.TmuxSessionName()

	// Start tmux attach command with PTY
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", fmt.Sprintf("%s:%d", tmuxSession, windowIdx))

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return "", fmt.Errorf("failed to start PTY: %w", err)
	}

	ps := &ptySession{
		ptmx:     ptmx,
		cmd:      cmd,
		session:  inst,
		windowID: windowIdx,
		cancel:   cancel,
	}

	a.ptyMu.Lock()
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
	delete(a.ptys, ptyID)

	runtime.EventsEmit(a.ctx, "pty:closed:"+ptyID, nil)
	return nil
}

// SendInput sends input to PTY
func (a *App) SendInput(ptyID string, data string) error {
	a.ptyMu.RLock()
	ps, exists := a.ptys[ptyID]
	a.ptyMu.RUnlock()

	if !exists {
		return fmt.Errorf("PTY session not found")
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
		return fmt.Errorf("PTY session not found")
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
	CompactList     bool   `json:"compactList"`
	HideStatusLines bool   `json:"hideStatusLines"`
	ShowAgentIcons  bool   `json:"showAgentIcons"`
	SplitView       bool   `json:"splitView"`
	MarkedSessionID string `json:"markedSessionId"`
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
	return &SettingsInfo{
		CompactList:     settings.CompactList,
		HideStatusLines: settings.HideStatusLines,
		ShowAgentIcons:  settings.ShowAgentIcons,
		SplitView:       settings.SplitView,
		MarkedSessionID: settings.MarkedSessionID,
	}, nil
}

// SaveSettings saves UI settings
func (a *App) SaveSettings(settings SettingsInfo) error {
	return a.storage.SaveSettings(&session.Settings{
		CompactList:     settings.CompactList,
		HideStatusLines: settings.HideStatusLines,
		ShowAgentIcons:  settings.ShowAgentIcons,
		SplitView:       settings.SplitView,
		MarkedSessionID: settings.MarkedSessionID,
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

// CheckForUpdate checks for updates
func (a *App) CheckForUpdate() *UpdateInfo {
	current := "0.1.0" // TODO: embed version
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

// getTaskManager returns or creates a task manager for a session's project path
func (a *App) getTaskManager(sessionID string) (*session.TaskManager, error) {
	sess, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	projectPath := sess.Path
	if projectPath == "" {
		return nil, fmt.Errorf("session has no project path")
	}

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
		return nil, fmt.Errorf("session has no project path")
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

	prompt := mcp.FormatTaskForPrompt(task)
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

// TaskMasterUpdateTaskDirect updates a task with direct field values (no AI)
func (a *App) TaskMasterUpdateTaskDirect(sessionID, taskID, title, description, details, priority string) error {
	tm, err := a.getTaskMasterMCP(sessionID)
	if err != nil {
		return err
	}

	return tm.UpdateTaskDirect(taskID, title, description, details, priority)
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
