package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Storage struct {
	mu         sync.Mutex
	projectsMu sync.Mutex
	lockMu     sync.Mutex
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

// CustomTerminalTheme is one user-defined terminal palette.
type CustomTerminalTheme struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Colors map[string]string `json:"colors"`
}

// Settings stores UI preferences
type Settings struct {
	CompactList     bool `json:"compact_list"`
	HideStatusLines bool `json:"hide_status_lines"`
	ShowAgentIcons  bool `json:"show_agent_icons,omitempty"`
	// YOLO shows by default (it flags bypassed permissions, worth seeing), so
	// it's a "hide" flag whose zero value keeps it visible. The resume marker
	// is the opposite: most sessions continue an earlier conversation, so it
	// adds noise more than information and is opt-in.
	HideYoloBadge   bool   `json:"hide_yolo_badge,omitempty"`
	ShowResumeBadge bool   `json:"show_resume_badge,omitempty"`
	SplitView       bool   `json:"split_view,omitempty"`
	MarkedSessionID string `json:"marked_session_id,omitempty"`
	// LastSessionID is the session that was selected when the app last closed,
	// so it reopens where the user left off. The tab within it is remembered
	// separately, per session (Instance.LastWindowIndex).
	LastSessionID   string `json:"last_session_id,omitempty"`
	MarkedWindowIdx int    `json:"marked_window_idx,omitempty"`
	// Cursor and SplitFocus are TUI-era fields that nothing here reads. Kept so
	// a config shared with, or migrated from, the terminal version round-trips
	// unchanged — dropping them would silently discard the TUI's state.
	Cursor     int `json:"cursor,omitempty"`
	SplitFocus int `json:"split_focus,omitempty"`
	// AnthropicAPIKey is likewise not settable from this app, but it is handled
	// deliberately in recovery.go: stripped before a backup is written and
	// carried across a restore, so a key that arrives from elsewhere is never
	// copied into the backup directory.
	AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
	Language        string `json:"language,omitempty"`
	// UITheme is the interface accent colour (see uiThemes.ts); empty = default.
	UITheme string `json:"ui_theme,omitempty"`
	// UIAccent is the custom accent hex, used when UITheme is "custom".
	UIAccent string `json:"ui_accent,omitempty"`
	// TerminalRenderer selects the xterm.js renderer: "canvas" (default),
	// "webgl" (fastest but flaky on some WebKitGTK), or "dom" (most compatible).
	TerminalRenderer string `json:"terminal_renderer,omitempty"`
	// TerminalCopyMode decides what copies a terminal selection: "shift"
	// (default) for a Shift-held drag only, or "select" for any drag.
	TerminalCopyMode string `json:"terminal_copy_mode,omitempty"`
	// TerminalFontFamily overrides the terminal's font stack. Empty means the
	// built-in default.
	TerminalFontFamily string `json:"terminal_font_family,omitempty"`
	// QuickJump is the hand-ordered list of places worth returning to, shown
	// in the jump window. Separate from the Favorite mark on an instance: that
	// says "this matters" and shows it in the sidebar, this says "I keep going
	// here" and puts it behind a number key.
	QuickJump []QuickJumpEntry `json:"quick_jump,omitempty"`
	// TerminalShell is the command a plain terminal tab starts. Empty means the
	// system default — $SHELL on Unix, COMSPEC on Windows — which is what a
	// newly created tab gets from the multiplexer itself.
	//
	// It exists mainly for Windows, where "the shell" is genuinely ambiguous:
	// cmd.exe and PowerShell are both reasonable and neither is discoverable
	// from the environment alone. Elsewhere it is an override for anyone whose
	// login shell is not what they want their tabs to run.
	TerminalShell string `json:"terminal_shell,omitempty"`
	// GitBranchDisplay places the session's git branch: "header" (default),
	// "statusbar" or "off".
	GitBranchDisplay string `json:"git_branch_display,omitempty"`
	// The diff file list groups into a directory tree by default. Stored as a
	// "flat" flag rather than a "tree" one so the zero value keeps the default
	// in place — with omitempty there is no entry at all in existing configs.
	DiffFlatFileList bool `json:"diff_flat_file_list,omitempty"`
	// Days a deleted session or tab stays in the trash. Zero can't mean "keep
	// everything" here: with omitempty it is also what every config written
	// before this setting existed says, and reading those as "never expire"
	// would leave the growing trash this was added to bound. So zero means the
	// default and "keep everything" is stored as a negative.
	TrashRetentionDays int `json:"trash_retention_days,omitempty"`
	// The Task Master panel shells out to `npx task-master-ai`, which installs
	// the package on first use — not something to do to a machine nobody asked.
	// So it is opt-in, and plain false is the right zero value here: with
	// omitempty an absent key and an explicit false are indistinguishable, and
	// both mean "off", which is exactly the default we want. Still experimental.
	TaskMasterEnabled bool `json:"task_master_enabled,omitempty"`
	// RestoreLastSession reopens the session that was selected at shutdown
	// instead of starting on the dashboard. Off by default: the dashboard is
	// the neutral starting point, and landing straight in a session is a
	// preference rather than an obvious improvement.
	RestoreLastSession bool `json:"restore_last_session,omitempty"`
	// Attention notifications: fire when an agent flips to "waiting"
	// (needs user input). Desktop uses notify-send/osascript; ntfy POSTs
	// to NtfyURL (e.g. https://ntfy.sh/my-topic) for mobile push.
	NotifyOnWaiting bool   `json:"notify_on_waiting,omitempty"`
	NotifyDesktop   bool   `json:"notify_desktop,omitempty"`
	NotifyNtfy      bool   `json:"notify_ntfy,omitempty"`
	NtfyURL         string `json:"ntfy_url,omitempty"`
	// TerminalTheme is the BASE terminal colour palette (see
	// frontend/src/lib/utils/terminalThemes.ts). Empty = the app default.
	// AgentTerminalThemes overrides it per agent type ("claude", "terminal",
	// …); a tab's own TerminalTheme overrides both. Resolution order:
	// tab → agent → base.
	// Two independent palette worlds, deliberately unaware of each other:
	//   TerminalTheme      — default for plain terminal sessions/tabs
	//   AgentDefaultTheme  — default for agent sessions/tabs
	// AgentTerminalThemes refines the agent side per agent type. A tab's own
	// palette still wins over either default.
	// TerminalFontSize is the default size in px; 0 means the built-in
	// default, so existing settings keep the size they had.
	// Two independent defaults, like the palettes: terminal tabs and agent
	// tabs each have their own, and neither falls back to the other.
	TerminalFontSize    int               `json:"terminal_font_size,omitempty"`
	AgentFontSize       int               `json:"agent_font_size,omitempty"`
	HideViewBar         bool              `json:"hide_view_bar,omitempty"`
	AgentHideViewBar    bool              `json:"agent_hide_view_bar,omitempty"`
	HideStatusBar       bool              `json:"hide_status_bar,omitempty"`
	AgentHideStatusBar  bool              `json:"agent_hide_status_bar,omitempty"`
	TerminalTheme       string            `json:"terminal_theme,omitempty"`
	AgentDefaultTheme   string            `json:"agent_default_theme,omitempty"`
	AgentTerminalThemes map[string]string `json:"agent_terminal_themes,omitempty"`
	// CustomTerminalThemes holds user-defined palettes. Each has a stable id
	// ("custom:<n>"), a display name, and xterm ITheme keys → colour strings.
	// They appear alongside the built-in schemes everywhere a palette is
	// picked. CustomTerminalTheme is the legacy single-palette field, kept so
	// an older config still loads (migrated into the list on first save).
	CustomTerminalThemes []CustomTerminalTheme `json:"custom_terminal_themes,omitempty"`
	CustomTerminalTheme  map[string]string     `json:"custom_terminal_theme,omitempty"`
	// Rebound keyboard shortcuts, by shortcut id. Only the ones the user has
	// changed are stored, so a shortcut whose default later moves follows the
	// new default rather than being pinned to the old one by an entry nobody
	// asked for. The value is the binding in the frontend's own shape; the
	// backend stores and returns it without interpreting it, since what counts
	// as a valid binding is decided where the key events are.
	ShortcutOverrides map[string]any `json:"shortcut_overrides,omitempty"`
	// Height in pixels of the diff pane shown above a view. Zero means the
	// built-in default, so a config written before this existed opens at it.
	DiffAboveHeight int `json:"diff_above_height,omitempty"`
	// Where the dictation buffer window was left, in pixels. Stored so it
	// stays where it was put across restarts; zero means the built-in default
	// placement, so a config written before this existed opens at it.
	//
	// Kept as the size and position it was given rather than as a fraction of
	// the window: it is a floating panel the user drags to a spot that suits
	// them, and a proportion of a different-sized screen is not that spot.
	// What that costs — a saved position that no longer fits — is corrected on
	// the way out, not on the way in.
	DictationBuffer *PanelGeometry `json:"dictation_buffer,omitempty"`
	// DiffSideBySide shows a file's diff as two aligned columns rather than one
	// with markers. Stored as an opt-in flag so the zero value keeps the
	// unified view a config written before this existed opened at.
	DiffSideBySide bool `json:"diff_side_by_side,omitempty"`
	// DiffHunksOnly shows just the changed hunks rather than the whole file.
	//
	// Stored inverted, because whole-file is the default and an omitempty bool
	// cannot distinguish "off" from "never set" — a plain DiffWholeFile would
	// read as false for every config written before this existed and quietly
	// flip everyone to hunks-only.
	DiffHunksOnly bool `json:"diff_hunks_only,omitempty"`
	// DiffLastFile is the file the diff had open, so leaving the tab and
	// coming back resumes rather than restarts. Keyed by session id.
	DiffLastFile map[string]string `json:"diff_last_file,omitempty"`
}

// PanelGeometry is a floating panel's remembered size and position.
type PanelGeometry struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type StorageData struct {
	SchemaVersion int           `json:"schema_version,omitempty"`
	Revision      uint64        `json:"revision,omitempty"`
	Instances     []*Instance   `json:"instances"`
	Groups        []*Group      `json:"groups,omitempty"`
	Settings      *Settings     `json:"settings,omitempty"`
	Trash         []*TrashEntry `json:"trash,omitempty"`
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
	var configPath string
	if projectID == "" {
		configPath = filepath.Join(s.configDir, "sessions.json")
	} else {
		projectDir := filepath.Join(s.configDir, "projects", projectID)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			return fmt.Errorf("failed to create project directory: %w", err)
		}
		configPath = filepath.Join(projectDir, "sessions.json")
	}
	s.projectID = projectID
	s.configPath = configPath
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
	name := "default.lock"
	if projectID != "" {
		name = projectID + ".lock"
	}
	// Ownership must live outside the project data directory. Project deletion
	// temporarily renames that directory; keeping the lock inside it created an
	// unlocked window in which another process could open data being deleted.
	return filepath.Join(s.configDir, "project-locks", name)
}

func (s *Storage) legacyLockPath(projectID string) string {
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
	// Status checks never unlink. Reclamation belongs to LockProject, where a
	// separate atomic claim serialises competing stale-lock removers.
	if locked, pid := projectLockOwner(s.getLockPath(projectID)); locked {
		return true, pid
	}
	// Recognise locks written by versions before locks moved out of the project
	// directory, so an update cannot open a project still owned by an older app.
	return projectLockOwner(s.legacyLockPath(projectID))
}

func projectLockOwner(lockPath string) (bool, int) {
	data, err := os.ReadFile(lockPath)
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		return false, 0
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, 0
	}

	if !processAlive(pid) {
		return false, 0
	}

	return true, pid
}

// ErrProjectLocked is returned by LockProject when another live instance
// already holds the project. The holder's PID is carried on the error.
type ErrProjectLocked struct {
	PID int
}

func (e *ErrProjectLocked) Error() string {
	return fmt.Sprintf("project already open in another instance (pid %d)", e.PID)
}

// LockProject acquires the project's lock file for this process. If a LIVE
// instance already holds it, it fails with *ErrProjectLocked instead of
// stealing the lock — two GUIs on the same project fight over the same tmux
// windows and rip each other's ptys out ("read /dev/ptmx: input/output
// error"). A stale lock (dead PID) is reclaimed. Any previously held lock by
// this process is released first, so switching projects is safe.
func (s *Storage) LockProject(projectID string) error {
	if !validProjectID(projectID) {
		return fmt.Errorf("invalid project ID")
	}

	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if locked, owner := projectLockOwner(s.legacyLockPath(projectID)); locked && owner != os.Getpid() {
		return &ErrProjectLocked{PID: owner}
	}

	lockPath := s.getLockPath(projectID)
	if s.lockPath == lockPath {
		if locked, pid := projectLockOwner(lockPath); locked && pid == os.Getpid() {
			return nil
		}
	}

	// A project switch must relinquish the old lock even when the target is
	// already owned elsewhere. Otherwise this read-only instance leaves the old
	// project looking live until its process exits.
	s.unlockProjectLocked()

	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create lock directory: %w", err)
	}

	pid := os.Getpid()
	prepared, err := os.CreateTemp(dir, ".project-lock-*")
	if err != nil {
		return fmt.Errorf("failed to prepare project lock: %w", err)
	}
	preparedPath := prepared.Name()
	defer os.Remove(preparedPath)
	if err := prepared.Chmod(0644); err == nil {
		_, err = prepared.WriteString(strconv.Itoa(pid))
	}
	if err == nil {
		err = prepared.Sync()
	}
	if closeErr := prepared.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("failed to prepare project lock: %w", err)
	}

	claimPath := lockPath + ".reclaim"
	for attempts := 0; attempts < 100; attempts++ {
		// Linking a fully-written private inode publishes the PID and claims the
		// destination in one atomic operation. O_EXCL followed by Write would
		// expose an empty file that another contender could mistake for stale.
		err := os.Link(preparedPath, lockPath)
		if err == nil {
			s.lockPath = lockPath
			return nil
		}
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		locked, owner := projectLockOwner(lockPath)
		if locked {
			if owner == pid {
				s.lockPath = lockPath
				return nil
			}
			return &ErrProjectLocked{PID: owner}
		}
		// Serialise stale reclamation too. Without this claim, two contenders can
		// both inspect the old inode; one then replaces it and the other's delayed
		// Remove deletes the new owner's lock.
		if err := os.Link(preparedPath, claimPath); err == nil {
			locked, owner = projectLockOwner(lockPath)
			if locked {
				_ = os.Remove(claimPath)
				if owner == pid {
					s.lockPath = lockPath
					return nil
				}
				return &ErrProjectLocked{PID: owner}
			}
			if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
				_ = os.Remove(claimPath)
				return fmt.Errorf("failed to remove stale project lock: %w", err)
			}
			err = os.Link(preparedPath, lockPath)
			_ = os.Remove(claimPath)
			if err == nil {
				s.lockPath = lockPath
				return nil
			}
			if !os.IsExist(err) {
				return fmt.Errorf("failed to create lock file: %w", err)
			}
		} else if !os.IsExist(err) {
			return fmt.Errorf("failed to claim stale project lock: %w", err)
		} else if claimLocked, claimPID := projectLockOwner(claimPath); !claimLocked && claimPID == 0 {
			// A process that died during the tiny reclamation section can leave
			// only the claim behind. It names its PID, so dead claims are safe to
			// discard and retry.
			_ = os.Remove(claimPath)
		}
		time.Sleep(2 * time.Millisecond)
	}
	return fmt.Errorf("failed to acquire project lock after stale-lock contention")
}

// UnlockProject removes the lock file
func (s *Storage) UnlockProject() {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	s.unlockProjectLocked()
}

func (s *Storage) unlockProjectLocked() {
	if s.lockPath == "" {
		return
	}
	// Only remove a lock that still names this process. If another process
	// replaced a stale/corrupt path, shutdown must never delete its ownership.
	if locked, pid := projectLockOwner(s.lockPath); locked && pid == os.Getpid() {
		_ = os.Remove(s.lockPath)
	}
	s.lockPath = ""
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
	return s.withProjectsCatalogLock(func() error {
		return s.saveProjectsLocked(projectsData)
	})
}

func (s *Storage) withProjectsCatalogLock(action func() error) error {
	return withCrossProcessFileLock(filepath.Join(s.configDir, "projects.json.lock"), action)
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
	if err := s.createProjectsBackupLocked(projectsData); err != nil {
		fmt.Fprintf(os.Stderr, "automatic projects backup failed: %v\n", err)
	}

	return nil
}

// AddProject creates a new project
func (s *Storage) AddProject(name string) (*Project, error) {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	var project *Project
	err := s.withProjectsCatalogLock(func() error {
		projectsData, err := s.loadProjectsLocked()
		if err != nil {
			return err
		}

		// Check for duplicate names against the snapshot read while holding the
		// process-wide file lock, not against a stale pre-lock catalog.
		for _, p := range projectsData.Projects {
			if p.Name == name {
				return fmt.Errorf("project with name '%s' already exists", name)
			}
		}

		project = NewProject(name)
		projectsData.Projects = append(projectsData.Projects, project)
		return s.saveProjectsLocked(projectsData)
	})
	if err != nil {
		return nil, err
	}

	return project, nil
}

// RemoveProject removes a project and its data
func (s *Storage) RemoveProject(id string) error {
	if !validProjectID(id) {
		return fmt.Errorf("invalid project ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projectID == id {
		return fmt.Errorf("cannot delete the active project")
	}
	// Own the same out-of-tree lock used by SelectProject for the complete
	// delete transaction. This closes the check/rename race: a new opener cannot
	// claim the project after our check, and an existing owner makes this fail.
	claim := &Storage{configDir: s.configDir}
	if err := claim.LockProject(id); err != nil {
		return err
	}
	defer claim.UnlockProject()

	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	return s.withProjectsCatalogLock(func() error {
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

		// Move the data aside before committing the catalog change. If the catalog
		// write fails, rename it back so an I/O error cannot leave a listed project
		// whose sessions have already been irreversibly deleted.
		projectDir := filepath.Join(s.configDir, "projects", id)
		trashRoot := filepath.Join(s.configDir, "project-trash")
		trashDir := filepath.Join(trashRoot, fmt.Sprintf("%s-%d-%d", id, os.Getpid(), time.Now().UnixNano()))
		moved := false
		if _, err := os.Stat(projectDir); err == nil {
			if err := os.MkdirAll(trashRoot, 0700); err != nil {
				return fmt.Errorf("failed to create project trash: %w", err)
			}
			if err := os.Rename(projectDir, trashDir); err != nil {
				return fmt.Errorf("failed to move project data to trash: %w", err)
			}
			moved = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect project data: %w", err)
		}

		if err := s.saveProjectsLocked(projectsData); err != nil {
			if moved {
				if rollbackErr := os.Rename(trashDir, projectDir); rollbackErr != nil {
					return fmt.Errorf("failed to save projects: %v (also failed to restore project data: %w)", err, rollbackErr)
				}
			}
			return err
		}
		if moved {
			if err := os.RemoveAll(trashDir); err != nil {
				fmt.Fprintf(os.Stderr, "failed to clean deleted project data %s: %v\n", trashDir, err)
			}
		}
		return nil
	})
}

// RenameProject renames a project
func (s *Storage) RenameProject(id, name string) error {
	if !validProjectID(id) {
		return fmt.Errorf("invalid project ID")
	}
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	return s.withProjectsCatalogLock(func() error {
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
	})
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
	storageData, err := s.loadStorageDataLocked()
	if err != nil {
		return nil, nil, nil, err
	}

	return storageData.Instances, storageData.Groups, storageData.Settings, nil
}

func (s *Storage) loadStorageDataLocked() (*StorageData, error) {
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		return &StorageData{
			SchemaVersion: recoverySchemaVersion,
			Instances:     []*Instance{},
			Groups:        []*Group{},
			Settings:      &Settings{},
			Trash:         []*TrashEntry{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var storageData StorageData
	if err := json.Unmarshal(data, &storageData); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
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
	if storageData.Instances == nil {
		storageData.Instances = []*Instance{}
	}
	if storageData.Trash == nil {
		storageData.Trash = []*TrashEntry{}
	}
	if storageData.SchemaVersion == 0 {
		storageData.SchemaVersion = recoverySchemaVersion
	}
	if storageData.SchemaVersion > recoverySchemaVersion {
		return nil, fmt.Errorf(
			"storage schema version %d is newer than supported version %d",
			storageData.SchemaVersion, recoverySchemaVersion,
		)
	}

	return &storageData, nil
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

// UpdateSettings mutates settings while holding the same lock that protects the
// active project path. This prevents a project switch between reading and
// writing settings and preserves backend-only fields.
func (s *Storage) UpdateSettings(update func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	if data.Settings == nil {
		data.Settings = &Settings{}
	}
	update(data.Settings)
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.writeStorageDataLocked(data, true)
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
	storageData, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	storageData.SchemaVersion = recoverySchemaVersion
	storageData.Revision++
	storageData.Instances = instances
	storageData.Groups = groups
	storageData.Settings = settings
	return s.writeStorageDataLocked(storageData, true)
}

func (s *Storage) writeStorageDataLocked(storageData *StorageData, createBackup bool) error {
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

	if createBackup {
		if err := s.createAutomaticBackupLocked(storageData); err != nil {
			// The primary atomic save already succeeded. Keep normal app
			// mutations available even if the optional recovery history cannot
			// be updated (for example because the backup volume is full).
			fmt.Fprintf(os.Stderr, "automatic backup failed: %v\n", err)
		}
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

// MergeResumeSessionIDsForProject atomically records detected conversation IDs
// on the latest stored instance. Sidebar polling works from an earlier
// snapshot, so replacing the entire instance here could otherwise undo a
// concurrent tab rename/create/stop or notes edit.
//
// It records a detected ID that DIFFERS from the stored one, not only one
// filling a gap. Resuming inside an agent moves the tab to another conversation
// without restarting it, so the stored ID goes stale the moment the user
// switches — and refusing to overwrite kept the tab pointing at the
// conversation they had left. Restarting it then reopened that one.
//
// Callers pass only what they actually detected. An agent whose detection is a
// filesystem guess must not reach here with a differing ID, because overwriting
// on a guess is worse than missing a switch.
func (s *Storage) MergeResumeSessionIDsForProject(projectID string, detected *Instance) error {
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
	for _, current := range instances {
		if current.ID != detected.ID {
			continue
		}

		changed := false
		if detected.Agent != AgentCodex &&
			current.Agent == detected.Agent &&
			detected.ResumeSessionID != "" &&
			detected.ResumeSessionID != current.ResumeSessionID {
			current.ResumeSessionID = detected.ResumeSessionID
			changed = true
		}

		detectedByIndex := make(map[int]FollowedWindow, len(detected.FollowedWindows))
		for _, window := range detected.FollowedWindows {
			if window.Agent != AgentCodex && window.ResumeSessionID != "" {
				detectedByIndex[window.Index] = window
			}
		}
		for idx := range current.FollowedWindows {
			window := &current.FollowedWindows[idx]
			detectedWindow, ok := detectedByIndex[window.Index]
			if !ok || detectedWindow.ResumeSessionID == window.ResumeSessionID {
				continue
			}
			if window.Agent == detectedWindow.Agent &&
				window.WorkDir == detectedWindow.WorkDir {
				window.ResumeSessionID = detectedWindow.ResumeSessionID
				changed = true
			}
		}

		if !changed {
			return nil
		}
		return s.saveAllLocked(instances, groups, settings)
	}
	return fmt.Errorf("instance not found")
}

// CaptureCodexResumeIDsForProject reloads the current instance and detects its
// live process while holding the storage lock. This prevents a stale sidebar
// snapshot from assigning an old process ID after a rapid stop/start or a
// deleted tmux index being reused by a newly created tab.
func (s *Storage) CaptureCodexResumeIDsForProject(projectID, instanceID string) (bool, error) {
	return s.captureCodexResumeIDsForProject(
		projectID,
		instanceID,
		DetectCodexSessionIDFromTmux,
		func(instance *Instance) (int, bool) { return instance.getMainWindowIndex() },
	)
}

func (s *Storage) captureCodexResumeIDsForProject(
	projectID,
	instanceID string,
	detect codexSessionDetector,
	detectMainWindow func(*Instance) (int, bool),
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	originalProject := s.projectID
	if err := s.setActiveProjectLocked(projectID); err != nil {
		return false, err
	}
	defer s.setActiveProjectLocked(originalProject)

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return false, err
	}
	for _, current := range instances {
		if current.ID != instanceID {
			continue
		}
		mainWindowIdx, mainWindowOK := 0, false
		if current.Agent == AgentCodex && current.ResumeSessionID == "" && !current.MainWindowStopped {
			mainWindowIdx, mainWindowOK = detectMainWindow(current)
		}
		if !current.captureCodexResumeIDsAtMainWindow(detect, mainWindowIdx, mainWindowOK) {
			return false, nil
		}
		if err := s.saveAllLocked(instances, groups, settings); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("instance not found")
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

// MoveGroup moves a group to a new position in the sidebar order.
//
// Order is the slice order itself rather than a numeric field on Group: the
// groups already round-trip through JSON as an array, so there is nothing to
// migrate and no way for two groups to claim the same position.
//
// newIndex is clamped, so callers can pass index-1 / index+1 for "move up" and
// "move down" without special-casing the ends of the list.
func (s *Storage) MoveGroup(id string, newIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}

	oldIndex := -1
	for i, g := range groups {
		if g.ID == id {
			oldIndex = i
			break
		}
	}
	if oldIndex < 0 {
		return fmt.Errorf("group not found")
	}

	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex > len(groups)-1 {
		newIndex = len(groups) - 1
	}
	if newIndex == oldIndex {
		return nil
	}

	moved := groups[oldIndex]
	groups = append(groups[:oldIndex], groups[oldIndex+1:]...)
	groups = append(groups[:newIndex], append([]*Group{moved}, groups[newIndex:]...)...)

	return s.saveAllLocked(instances, groups, settings)
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

// ReorderInstance changes one session's position using the latest on-disk
// snapshot under a single storage lock.
func (s *Storage) ReorderInstance(instanceID string, direction int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}
	current := -1
	for i := range instances {
		if instances[i].ID == instanceID {
			current = i
			break
		}
	}
	if current < 0 {
		return fmt.Errorf("error.sessionNotFound")
	}
	target := current + direction
	if target < 0 || target >= len(instances) {
		return nil
	}
	instances[current], instances[target] = instances[target], instances[current]
	return s.saveAllLocked(instances, groups, settings)
}

// MoveInstanceToIndex is the absolute-index variant of ReorderInstance.
func (s *Storage) MoveInstanceToIndex(instanceID string, target int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}
	current := -1
	for i := range instances {
		if instances[i].ID == instanceID {
			current = i
			break
		}
	}
	if current < 0 {
		return fmt.Errorf("error.sessionNotFound")
	}
	if target < 0 {
		target = 0
	}
	if target >= len(instances) {
		target = len(instances) - 1
	}
	if current == target {
		return nil
	}
	item := instances[current]
	instances = append(instances[:current], instances[current+1:]...)
	instances = append(instances, nil)
	copy(instances[target+1:], instances[target:])
	instances[target] = item
	return s.saveAllLocked(instances, groups, settings)
}

// SetGroupColors updates group presentation without a separate load/save
// window that could overwrite a concurrent session change.
func (s *Storage) SetGroupColors(id, color, bgColor string, fullRow bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group.ID == id {
			group.Color = color
			group.BgColor = bgColor
			group.FullRowColor = fullRow
			return s.saveAllLocked(instances, groups, settings)
		}
	}
	return fmt.Errorf("error.groupNotFound")
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
