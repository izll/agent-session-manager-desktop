package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type Storage struct {
	mu         sync.Mutex
	projectsMu sync.Mutex
	configDir  string
	configPath string
	projectID  string // Active project ID ("" = default)
	lockPath   string // Current lock file path
}

// Group represents a session group for organizing sessions
type Group struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Collapsed    bool   `json:"collapsed"`
	Color        string `json:"color,omitempty"`          // Group name color
	BgColor      string `json:"bg_color,omitempty"`       // Background color
	FullRowColor bool   `json:"full_row_color,omitempty"` // Extend background to full row
}

// Settings stores UI preferences
type Settings struct {
	CompactList     bool   `json:"compact_list"`
	HideStatusLines bool   `json:"hide_status_lines"`
	ShowAgentIcons  bool   `json:"show_agent_icons,omitempty"`
	SplitView       bool   `json:"split_view,omitempty"`
	MarkedSessionID string `json:"marked_session_id,omitempty"`
	Cursor          int    `json:"cursor,omitempty"`
	SplitFocus      int    `json:"split_focus,omitempty"`
	AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
	Language        string `json:"language,omitempty"`
	// TerminalRenderer selects the xterm.js renderer: "canvas" (default),
	// "webgl" (fastest but flaky on some WebKitGTK), or "dom" (most compatible).
	TerminalRenderer string `json:"terminal_renderer,omitempty"`
	// Attention notifications: fire when an agent flips to "waiting"
	// (needs user input). Desktop uses notify-send/osascript; ntfy POSTs
	// to NtfyURL (e.g. https://ntfy.sh/my-topic) for mobile push.
	NotifyOnWaiting bool   `json:"notify_on_waiting,omitempty"`
	NotifyDesktop   bool   `json:"notify_desktop,omitempty"`
	NotifyNtfy      bool   `json:"notify_ntfy,omitempty"`
	NtfyURL         string `json:"ntfy_url,omitempty"`
}

type StorageData struct {
	Instances []*Instance `json:"instances"`
	Groups    []*Group    `json:"groups,omitempty"`
	Settings  *Settings   `json:"settings,omitempty"`
}

// DefaultSettings returns the initial settings a brand-new install should have.
// Called on first launch (when no sessions.json exists yet) so UI toggles that
// are expected to be on by default (agent icons, English locale) actually are.
func DefaultSettings() *Settings {
	return &Settings{
		ShowAgentIcons: true,
		Language:       "en",
	}
}

func NewStorage() (*Storage, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "agent-session-manager-desktop")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	s := &Storage{
		configDir:  configDir,
		configPath: filepath.Join(configDir, "sessions.json"),
		projectID:  "",
	}

	// Seed sessions.json with default settings on first launch so that
	// UI flags which should be "on" by default are actually persisted.
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		_ = s.saveAllLocked([]*Instance{}, []*Group{}, DefaultSettings())
	}

	return s, nil
}

// SetActiveProject switches to a different project
func (s *Storage) SetActiveProject(projectID string) error {
	if !validProjectID(projectID) {
		return fmt.Errorf("invalid project ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setActiveProjectLocked(projectID)
}

// setActiveProjectLocked is the internal version that assumes the mutex is held.
func (s *Storage) setActiveProjectLocked(projectID string) error {
	if !validProjectID(projectID) {
		return fmt.Errorf("invalid project ID")
	}
	s.projectID = projectID
	if projectID == "" {
		s.configPath = filepath.Join(s.configDir, "sessions.json")
	} else {
		projectDir := filepath.Join(s.configDir, "projects", projectID)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			return fmt.Errorf("failed to create project directory: %w", err)
		}
		s.configPath = filepath.Join(projectDir, "sessions.json")
	}
	return nil
}

// GetActiveProjectID returns the currently active project ID
func (s *Storage) GetActiveProjectID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectID
}

// getLockPath returns the lock file path for a project
func (s *Storage) getLockPath(projectID string) string {
	if projectID == "" {
		return filepath.Join(s.configDir, "default.lock")
	}
	return filepath.Join(s.configDir, "projects", projectID, "project.lock")
}

// validProjectID ensures a caller-controlled ID can never escape the projects
// directory. Existing IDs only use this conservative portable character set.
func validProjectID(projectID string) bool {
	if projectID == "" {
		return true
	}
	if projectID == "." || projectID == ".." || filepath.Base(projectID) != projectID {
		return false
	}
	for _, r := range projectID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// IsProjectLocked checks if a project is already running
func (s *Storage) IsProjectLocked(projectID string) (bool, int) {
	if !validProjectID(projectID) {
		return false, 0
	}
	lockPath := s.getLockPath(projectID)
	data, err := os.ReadFile(lockPath)
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		return false, 0
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		// Invalid lock file, remove it
		os.Remove(lockPath)
		return false, 0
	}

	// Check if the process is still running
	process, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(lockPath)
		return false, 0
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process is not running, remove stale lock
		os.Remove(lockPath)
		return false, 0
	}

	return true, pid
}

// LockProject creates a lock file for the current project
func (s *Storage) LockProject(projectID string) error {
	if !validProjectID(projectID) {
		return fmt.Errorf("invalid project ID")
	}
	lockPath := s.getLockPath(projectID)

	// Ensure directory exists
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Write current PID to lock file
	pid := os.Getpid()
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	s.lockPath = lockPath
	return nil
}

// UnlockProject removes the lock file
func (s *Storage) UnlockProject() {
	if s.lockPath != "" {
		os.Remove(s.lockPath)
		s.lockPath = ""
	}
}

// LoadProjects loads the list of projects
func (s *Storage) LoadProjects() (*ProjectsData, error) {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	return s.loadProjectsLocked()
}

func (s *Storage) loadProjectsLocked() (*ProjectsData, error) {
	projectsFile := filepath.Join(s.configDir, "projects.json")
	data, err := os.ReadFile(projectsFile)
	if os.IsNotExist(err) {
		return &ProjectsData{Projects: []*Project{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read projects file: %w", err)
	}

	var projectsData ProjectsData
	if err := json.Unmarshal(data, &projectsData); err != nil {
		return nil, fmt.Errorf("failed to parse projects file: %w", err)
	}

	if projectsData.Projects == nil {
		projectsData.Projects = []*Project{}
	}

	return &projectsData, nil
}

// SaveProjects saves the list of projects (atomic write).
func (s *Storage) SaveProjects(projectsData *ProjectsData) error {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	return s.saveProjectsLocked(projectsData)
}

func (s *Storage) saveProjectsLocked(projectsData *ProjectsData) error {
	projectsFile := filepath.Join(s.configDir, "projects.json")
	data, err := json.MarshalIndent(projectsData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal projects: %w", err)
	}

	tmp := projectsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write projects temp file: %w", err)
	}
	if err := os.Rename(tmp, projectsFile); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename projects file: %w", err)
	}

	return nil
}

// AddProject creates a new project
func (s *Storage) AddProject(name string) (*Project, error) {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	projectsData, err := s.loadProjectsLocked()
	if err != nil {
		return nil, err
	}

	// Check for duplicate names
	for _, p := range projectsData.Projects {
		if p.Name == name {
			return nil, fmt.Errorf("project with name '%s' already exists", name)
		}
	}

	project := NewProject(name)
	projectsData.Projects = append(projectsData.Projects, project)

	if err := s.saveProjectsLocked(projectsData); err != nil {
		return nil, err
	}

	return project, nil
}

// RemoveProject removes a project and its data
func (s *Storage) RemoveProject(id string) error {
	if !validProjectID(id) {
		return fmt.Errorf("invalid project ID")
	}
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	projectsData, err := s.loadProjectsLocked()
	if err != nil {
		return err
	}

	newProjects := make([]*Project, 0, len(projectsData.Projects))
	found := false
	for _, p := range projectsData.Projects {
		if p.ID == id {
			found = true
			continue
		}
		newProjects = append(newProjects, p)
	}

	if !found {
		return fmt.Errorf("project not found")
	}

	projectsData.Projects = newProjects

	// Remove project directory
	projectDir := filepath.Join(s.configDir, "projects", id)
	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("failed to remove project data: %w", err)
	}

	return s.saveProjectsLocked(projectsData)
}

// RenameProject renames a project
func (s *Storage) RenameProject(id, name string) error {
	if !validProjectID(id) {
		return fmt.Errorf("invalid project ID")
	}
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	projectsData, err := s.loadProjectsLocked()
	if err != nil {
		return err
	}

	for _, p := range projectsData.Projects {
		if p.ID == id {
			p.Name = name
			return s.saveProjectsLocked(projectsData)
		}
	}

	return fmt.Errorf("project not found")
}

// GetProject returns a project by ID
func (s *Storage) GetProject(id string) (*Project, error) {
	if !validProjectID(id) {
		return nil, fmt.Errorf("invalid project ID")
	}
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	projectsData, err := s.loadProjectsLocked()
	if err != nil {
		return nil, err
	}

	for _, p := range projectsData.Projects {
		if p.ID == id {
			return p, nil
		}
	}

	return nil, fmt.Errorf("project not found")
}

// ImportDefaultSessions moves sessions from default storage to a project
func (s *Storage) ImportDefaultSessions(projectID string) (int, error) {
	if !validProjectID(projectID) || projectID == "" {
		return 0, fmt.Errorf("invalid project ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Save current project
	originalProject := s.projectID

	// Load default sessions
	s.projectID = ""
	s.configPath = filepath.Join(s.configDir, "sessions.json")
	defaultInstances, defaultGroups, _, err := s.loadAllWithSettingsLocked()
	if err != nil {
		s.setActiveProjectLocked(originalProject)
		return 0, err
	}

	if len(defaultInstances) == 0 {
		s.setActiveProjectLocked(originalProject)
		return 0, nil
	}

	// Switch to target project
	if err := s.setActiveProjectLocked(projectID); err != nil {
		s.setActiveProjectLocked(originalProject)
		return 0, err
	}

	// Load project's existing sessions
	projectInstances, projectGroups, projectSettings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		s.setActiveProjectLocked(originalProject)
		return 0, err
	}

	// Merge sessions and groups
	projectInstances = append(projectInstances, defaultInstances...)
	for _, g := range defaultGroups {
		// Check if group with same name exists
		exists := false
		for _, pg := range projectGroups {
			if pg.Name == g.Name {
				exists = true
				// Update instance group IDs to point to existing group
				for _, inst := range defaultInstances {
					if inst.GroupID == g.ID {
						inst.GroupID = pg.ID
					}
				}
				break
			}
		}
		if !exists {
			projectGroups = append(projectGroups, g)
		}
	}

	// Save merged data to project
	if err := s.saveAllLocked(projectInstances, projectGroups, projectSettings); err != nil {
		s.setActiveProjectLocked(originalProject)
		return 0, err
	}

	// Clear default sessions
	s.projectID = ""
	s.configPath = filepath.Join(s.configDir, "sessions.json")
	if err := s.saveAllLocked([]*Instance{}, []*Group{}, &Settings{}); err != nil {
		s.setActiveProjectLocked(originalProject)
		return len(defaultInstances), err
	}

	// Restore original project
	s.setActiveProjectLocked(originalProject)

	return len(defaultInstances), nil
}

// refreshInstanceStatuses updates each instance's Status by probing tmux,
// concurrently and WITHOUT holding s.mu. Called by the public Load* entry
// points after the lock is released so the per-instance `tmux has-session`
// subprocesses don't serialize the storage mutex.
func refreshInstanceStatuses(instances []*Instance) {
	if len(instances) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, inst := range instances {
		wg.Add(1)
		go func(in *Instance) {
			defer wg.Done()
			in.UpdateStatus()
		}(inst)
	}
	wg.Wait()
}

func (s *Storage) Load() ([]*Instance, error) {
	s.mu.Lock()
	instances, _, _, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatuses(instances)
	}
	return instances, err
}

// LoadAll loads instances, groups, and settings
func (s *Storage) LoadAll() ([]*Instance, []*Group, error) {
	s.mu.Lock()
	instances, groups, _, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatuses(instances)
	}
	return instances, groups, err
}

// LoadAllWithProjectSnapshot atomically captures the active project ID and its
// data. Callers doing expensive work can attach the captured ID to their result
// without an active-project ABA race.
func (s *Storage) LoadAllWithProjectSnapshot() (string, []*Instance, []*Group, error) {
	s.mu.Lock()
	projectID := s.projectID
	instances, groups, _, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatuses(instances)
	}
	return projectID, instances, groups, err
}

// LoadAllWithSettings loads instances, groups, and settings
func (s *Storage) LoadAllWithSettings() ([]*Instance, []*Group, *Settings, error) {
	s.mu.Lock()
	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatuses(instances)
	}
	return instances, groups, settings, err
}

// loadAllWithSettingsLocked is the internal version that assumes the mutex is held.
func (s *Storage) loadAllWithSettingsLocked() ([]*Instance, []*Group, *Settings, error) {
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		return []*Instance{}, []*Group{}, &Settings{}, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var storageData StorageData
	if err := json.Unmarshal(data, &storageData); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// NOTE: deliberately NOT calling instance.UpdateStatus() here.
	// UpdateStatus() shells out to `tmux has-session` per instance, and this
	// function runs with s.mu held — so every load (1Hz sidebar poll, every
	// terminal connect, every RPC mutate) would serialize on the storage
	// mutex while spawning N tmux subprocesses, stalling the UI.
	// Status is refreshed concurrently AFTER the lock is released by the
	// public Load* entry points via refreshInstanceStatuses(). Internal
	// callers that immediately re-persist don't need live status anyway —
	// they keep the persisted value.

	if storageData.Groups == nil {
		storageData.Groups = []*Group{}
	}

	if storageData.Settings == nil {
		storageData.Settings = &Settings{}
	}

	return storageData.Instances, storageData.Groups, storageData.Settings, nil
}

func (s *Storage) Save(instances []*Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return fmt.Errorf("failed to load existing data for merge: %w", err)
	}
	return s.saveAllLocked(instances, groups, settings)
}

// SaveWithGroups saves instances and groups (preserves settings)
func (s *Storage) SaveWithGroups(instances []*Instance, groups []*Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return fmt.Errorf("failed to load existing data for merge: %w", err)
	}
	return s.saveAllLocked(instances, groups, settings)
}

// SaveSettings saves only the settings (preserves instances and groups)
func (s *Storage) SaveSettings(settings *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, groups, _, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return fmt.Errorf("failed to load existing data for merge: %w", err)
	}
	return s.saveAllLocked(instances, groups, settings)
}

// SaveAll saves instances, groups, and settings
func (s *Storage) SaveAll(instances []*Instance, groups []*Group, settings *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveAllLocked(instances, groups, settings)
}

// saveAllLocked is the internal version that assumes the mutex is held.
// Uses atomic write (temp file + rename) to avoid corrupting config on crash.
func (s *Storage) saveAllLocked(instances []*Instance, groups []*Group, settings *Settings) error {
	storageData := StorageData{
		Instances: instances,
		Groups:    groups,
		Settings:  settings,
	}

	data, err := json.MarshalIndent(storageData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Atomic write: write to temp file in same dir, then rename.
	// 0600: the config can hold an Anthropic API key — owner-only.
	tmpPath := s.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, s.configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp config file: %w", err)
	}

	return nil
}

func (s *Storage) AddInstance(instance *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	// Check for duplicate names
	for _, inst := range instances {
		if inst.Name == instance.Name {
			return fmt.Errorf("instance with name '%s' already exists", instance.Name)
		}
	}

	instances = append(instances, instance)
	return s.saveAllLocked(instances, groups, settings)
}

func (s *Storage) RemoveInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	newInstances := make([]*Instance, 0, len(instances))
	found := false
	for _, inst := range instances {
		if inst.ID == id {
			found = true
			// Stop the instance if running
			inst.Stop()
			continue
		}
		newInstances = append(newInstances, inst)
	}

	if !found {
		return fmt.Errorf("instance not found")
	}

	return s.saveAllLocked(newInstances, groups, settings)
}

func (s *Storage) UpdateInstance(instance *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	for i, inst := range instances {
		if inst.ID == instance.ID {
			instances[i] = instance
			return s.saveAllLocked(instances, groups, settings)
		}
	}

	return fmt.Errorf("instance not found")
}

// UpdateInstanceForProject updates an instance in an explicitly selected
// project while holding the storage mutex for the complete switch/load/save
// sequence.
func (s *Storage) UpdateInstanceForProject(projectID string, instance *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	originalProject := s.projectID
	if err := s.setActiveProjectLocked(projectID); err != nil {
		return err
	}
	defer s.setActiveProjectLocked(originalProject)

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}
	for i, current := range instances {
		if current.ID == instance.ID {
			instances[i] = instance
			return s.saveAllLocked(instances, groups, settings)
		}
	}
	return fmt.Errorf("instance not found")
}

func (s *Storage) GetInstance(id string) (*Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, _, _, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.ID == id {
			return inst, nil
		}
	}

	return nil, fmt.Errorf("instance not found")
}

func (s *Storage) GetInstanceByName(name string) (*Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, _, _, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.Name == name {
			return inst, nil
		}
	}

	return nil, fmt.Errorf("instance not found")
}

// GetGroups returns all groups
func (s *Storage) GetGroups() ([]*Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, groups, _, err := s.loadAllWithSettingsLocked()
	return groups, err
}

// AddGroup adds a new group
func (s *Storage) AddGroup(name string) (*Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return nil, err
	}

	// Check for duplicate names
	for _, g := range groups {
		if g.Name == name {
			return nil, fmt.Errorf("group with name '%s' already exists", name)
		}
	}

	group := &Group{
		ID:        fmt.Sprintf("grp_%d", time.Now().UnixNano()),
		Name:      name,
		Collapsed: false,
	}

	groups = append(groups, group)
	if err := s.saveAllLocked(instances, groups, settings); err != nil {
		return nil, err
	}

	return group, nil
}

// RemoveGroup removes a group (sessions become ungrouped)
func (s *Storage) RemoveGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	// Ungroup all sessions in this group
	for _, inst := range instances {
		if inst.GroupID == id {
			inst.GroupID = ""
		}
	}

	// Remove the group
	newGroups := make([]*Group, 0, len(groups))
	found := false
	for _, g := range groups {
		if g.ID == id {
			found = true
			continue
		}
		newGroups = append(newGroups, g)
	}

	if !found {
		return fmt.Errorf("group not found")
	}

	return s.saveAllLocked(instances, newGroups, settings)
}

// RenameGroup renames a group
func (s *Storage) RenameGroup(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	for _, g := range groups {
		if g.ID == id {
			g.Name = name
			return s.saveAllLocked(instances, groups, settings)
		}
	}

	return fmt.Errorf("group not found")
}

// ToggleGroupCollapsed toggles the collapsed state of a group
func (s *Storage) ToggleGroupCollapsed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	for _, g := range groups {
		if g.ID == id {
			g.Collapsed = !g.Collapsed
			return s.saveAllLocked(instances, groups, settings)
		}
	}

	return fmt.Errorf("group not found")
}

// SetInstanceGroup assigns an instance to a group
func (s *Storage) SetInstanceGroup(instanceID, groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	for i := range instances {
		if instances[i].ID == instanceID {
			instances[i].GroupID = groupID
			return s.saveAllLocked(instances, groups, settings)
		}
	}

	return fmt.Errorf("instance not found")
}

// LoadAllForProject temporarily switches to a different project, loads its data, and switches back.
// This is atomic with respect to other storage operations.
func (s *Storage) LoadAllForProject(projectID string) ([]*Instance, []*Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	originalProject := s.projectID
	if err := s.setActiveProjectLocked(projectID); err != nil {
		return nil, nil, err
	}
	instances, groups, _, err := s.loadAllWithSettingsLocked()
	s.setActiveProjectLocked(originalProject)
	if err != nil {
		return nil, nil, err
	}
	return instances, groups, nil
}
