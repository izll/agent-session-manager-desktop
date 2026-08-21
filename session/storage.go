package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxCanonicalStorageBytes = 64 << 20
	maxProjectCatalogBytes   = 8 << 20
	maxPIDLockBytes          = 64
)

func readFileAtMost(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", filepath.Base(path), limit)
	}
	return data, nil
}

type Storage struct {
	mu                 sync.Mutex
	projectsMu         sync.Mutex
	lockMu             sync.Mutex
	configDir          string
	configPath         string
	projectID          string // Active project ID ("" = default)
	lockPath           string // Current out-of-tree lock file path
	legacyLockPathHeld string // Compatibility lock visible to pre-migration app versions
	readLockPath       string // Read-only viewer claim that blocks concurrent deletion
}

var temporaryProjectReaderSequence atomic.Uint64

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
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	// Upgrade directories created by older versions as 0755. The project
	// catalog contains user-chosen names and sessions/settings below this tree
	// may contain secrets; another local OS account must not traverse it.
	if err := os.Chmod(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to secure config directory: %w", err)
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
	if err := s.requireProjectExists(projectID); err != nil {
		return err
	}
	return s.setActiveProjectLocked(projectID)
}

func (s *Storage) requireProjectExists(projectID string) error {
	if projectID == "" {
		return nil
	}
	s.projectsMu.Lock()
	projectsData, err := s.loadProjectsLocked()
	s.projectsMu.Unlock()
	if err != nil {
		return err
	}
	for _, project := range projectsData.Projects {
		if project.ID == projectID {
			return nil
		}
	}
	return fmt.Errorf("project not found")
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

func (s *Storage) projectDeletionLockPath(projectID string) string {
	return s.getLockPath(projectID) + ".deleting"
}

func (s *Storage) projectReaderDir(projectID string) string {
	name := projectID
	if name == "" {
		name = "default"
	}
	return filepath.Join(s.configDir, "project-readers", name)
}

// validProjectID ensures a caller-controlled ID can never escape the projects
// directory. Existing IDs only use this conservative portable character set.
func validProjectID(projectID string) bool {
	if projectID == "" {
		return true
	}
	if projectID == "." || projectID == ".." || filepath.Base(projectID) != projectID || strings.HasSuffix(projectID, ".") {
		return false
	}
	for _, r := range projectID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	// These components are aliases for devices on Windows even with an
	// extension (for example CON.json). Reject them on every platform so a
	// catalog synced from Linux cannot become unusable or alias storage when it
	// is next opened on Windows.
	stem := projectID
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	upperStem := strings.ToUpper(stem)
	switch upperStem {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$":
		return false
	}
	if len(upperStem) == 4 && (strings.HasPrefix(upperStem, "COM") || strings.HasPrefix(upperStem, "LPT")) &&
		upperStem[3] >= '1' && upperStem[3] <= '9' {
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
	// Lock files contain one decimal PID. Treat oversized/corrupt files as stale
	// without reading them into memory: these paths survive crashes and are
	// therefore untrusted startup input (a sparse lock must not OOM the app).
	data, err := readFileAtMost(lockPath, maxPIDLockBytes)
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

// ErrProjectDeleting means a deletion transaction published its intent before
// this process could establish either exclusive ownership or a read-only
// viewer claim.
type ErrProjectDeleting struct {
	PID int
}

func (e *ErrProjectDeleting) Error() string {
	return fmt.Sprintf("project is being deleted by another instance (pid %d)", e.PID)
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

	lockPath := s.getLockPath(projectID)
	legacyPath := s.legacyLockPath(projectID)
	if s.lockPath == lockPath && s.legacyLockPathHeld == legacyPath {
		if locked, pid := projectLockOwner(lockPath); locked && pid == os.Getpid() {
			if legacyLocked, legacyPID := projectLockOwner(legacyPath); legacyLocked && legacyPID == pid {
				return nil
			}
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
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		return fmt.Errorf("failed to create legacy lock directory: %w", err)
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

	if err := claimProjectLockPath(preparedPath, lockPath, pid); err != nil {
		return err
	}
	if err := claimProjectLockPath(preparedPath, legacyPath, pid); err != nil {
		removeProjectLockIfOwned(lockPath, pid)
		return fmt.Errorf("failed to publish legacy project lock: %w", err)
	}
	s.lockPath = lockPath
	s.legacyLockPathHeld = legacyPath
	return nil
}

// LockProjectForUse acquires exclusive ownership when possible. If another
// application owns the project, it registers this process as a read-only
// viewer before returning ErrProjectLocked. The deletion marker/viewer claim
// handshake makes opening and deletion mutually exclusive across processes.
func (s *Storage) LockProjectForUse(projectID string) error {
	if !validProjectID(projectID) {
		return fmt.Errorf("invalid project ID")
	}
	if err := s.requireProjectExists(projectID); err != nil {
		return err
	}
	err := s.LockProject(projectID)
	if err == nil {
		if deleting, pid := projectLockOwner(s.projectDeletionLockPath(projectID)); deleting {
			s.UnlockProject()
			return &ErrProjectDeleting{PID: pid}
		}
		return nil
	}
	var locked *ErrProjectLocked
	if !errors.As(err, &locked) {
		return err
	}
	if deleting, pid := projectLockOwner(s.projectDeletionLockPath(projectID)); deleting {
		return &ErrProjectDeleting{PID: pid}
	}
	if claimErr := s.lockProjectReader(projectID); claimErr != nil {
		return claimErr
	}
	if deleting, pid := projectLockOwner(s.projectDeletionLockPath(projectID)); deleting {
		s.UnlockProject()
		return &ErrProjectDeleting{PID: pid}
	}
	return err
}

func (s *Storage) lockProjectReader(projectID string) error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	s.unlockProjectLocked()
	path := filepath.Join(s.projectReaderDir(projectID), strconv.Itoa(os.Getpid())+".lock")
	if err := claimPIDLockPath(path); err != nil {
		return fmt.Errorf("failed to register read-only project viewer: %w", err)
	}
	s.readLockPath = path
	return nil
}

// withTemporaryProjectReader prevents project deletion while one explicit
// cross-project snapshot is being read. Unlike lockProjectReader it does not
// replace the application's active-project ownership; the claim exists only
// for the duration of action.
func (s *Storage) withTemporaryProjectReader(projectID string, action func() error) error {
	if projectID == "" {
		return action()
	}
	if deleting, pid := projectLockOwner(s.projectDeletionLockPath(projectID)); deleting {
		return &ErrProjectDeleting{PID: pid}
	}
	path := filepath.Join(
		s.projectReaderDir(projectID),
		fmt.Sprintf("%d-%d-%d.lock", os.Getpid(), time.Now().UnixNano(), temporaryProjectReaderSequence.Add(1)),
	)
	if err := claimPIDLockPath(path); err != nil {
		return fmt.Errorf("failed to register temporary project reader: %w", err)
	}
	defer removeProjectLockIfOwned(path, os.Getpid())
	if deleting, pid := projectLockOwner(s.projectDeletionLockPath(projectID)); deleting {
		return &ErrProjectDeleting{PID: pid}
	}
	return action()
}

func claimPIDLockPath(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	prepared, err := os.CreateTemp(dir, ".pid-lock-*")
	if err != nil {
		return err
	}
	preparedPath := prepared.Name()
	defer os.Remove(preparedPath)
	pid := os.Getpid()
	if err := prepared.Chmod(0o600); err == nil {
		_, err = prepared.WriteString(strconv.Itoa(pid))
	}
	if err == nil {
		err = prepared.Sync()
	}
	if closeErr := prepared.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return claimProjectLockPath(preparedPath, path, pid)
}

func claimProjectLockPath(preparedPath, lockPath string, pid int) error {
	claimPath := lockPath + ".reclaim"
	for attempts := 0; attempts < 100; attempts++ {
		// Linking a fully-written private inode publishes the PID and claims the
		// destination in one atomic operation. O_EXCL followed by Write would
		// expose an empty file that another contender could mistake for stale.
		err := os.Link(preparedPath, lockPath)
		if err == nil {
			return nil
		}
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		locked, owner := projectLockOwner(lockPath)
		if locked {
			if owner == pid {
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

func removeProjectLockIfOwned(path string, pid int) {
	if locked, owner := projectLockOwner(path); locked && owner == pid {
		_ = os.Remove(path)
	}
}

// UnlockProject removes the lock file
func (s *Storage) UnlockProject() {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	s.unlockProjectLocked()
}

func (s *Storage) unlockProjectLocked() {
	if s.lockPath == "" && s.legacyLockPathHeld == "" && s.readLockPath == "" {
		return
	}
	// Only remove a lock that still names this process. If another process
	// replaced a stale/corrupt path, shutdown must never delete its ownership.
	pid := os.Getpid()
	removeProjectLockIfOwned(s.legacyLockPathHeld, pid)
	removeProjectLockIfOwned(s.lockPath, pid)
	removeProjectLockIfOwned(s.readLockPath, pid)
	s.lockPath = ""
	s.legacyLockPathHeld = ""
	s.readLockPath = ""
}

// LoadProjects loads the list of projects
func (s *Storage) LoadProjects() (*ProjectsData, error) {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	return s.loadProjectsLocked()
}

func (s *Storage) loadProjectsLocked() (*ProjectsData, error) {
	projectsFile := filepath.Join(s.configDir, "projects.json")
	data, err := readFileAtMost(projectsFile, maxProjectCatalogBytes)
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
	seenIDs := make(map[string]struct{}, len(projectsData.Projects))
	for index, project := range projectsData.Projects {
		if project == nil {
			return nil, fmt.Errorf("failed to validate projects file: project %d is null", index)
		}
		if !validProjectID(project.ID) || project.ID == "" {
			return nil, fmt.Errorf("failed to validate projects file: project %d has an invalid ID", index)
		}
		// Windows resolves these IDs through a case-insensitive directory and
		// lock namespace. Enforce that portable identity everywhere; otherwise a
		// catalog copied from Linux can expose two UI projects backed by the same
		// sessions.json.
		portableID := strings.ToLower(project.ID)
		if _, duplicate := seenIDs[portableID]; duplicate {
			return nil, fmt.Errorf("failed to validate projects file: duplicate project ID %q", project.ID)
		}
		seenIDs[portableID] = struct{}{}
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
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write projects temp file: %w", err)
	}
	// WriteFile preserves the mode of a stale crash artifact. Normalize before
	// rename so an old 0644 projects.json.tmp cannot republish project names.
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to secure projects temp file: %w", err)
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
	finishDeletion, err := s.beginProjectDeletion(id)
	if err != nil {
		return err
	}
	defer finishDeletion()
	// Own the same out-of-tree lock used by SelectProject for the complete
	// delete transaction. This closes the check/rename race: a new opener cannot
	// claim the project after our check, and an existing owner makes this fail.
	claim := &Storage{configDir: s.configDir}
	if err := claim.LockProject(id); err != nil {
		return err
	}
	projectDir := filepath.Join(s.configDir, "projects", id)
	defer func() {
		claim.UnlockProject()
		// LockProject creates the directory to publish the legacy sentinel. Once
		// that sentinel is gone, remove the now-empty shell as well.
		_ = os.Remove(projectDir)
	}()

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
		trashRoot := filepath.Join(s.configDir, "project-trash")
		trashDir := filepath.Join(trashRoot, fmt.Sprintf("%s-%d-%d", id, os.Getpid(), time.Now().UnixNano()))
		entries, err := os.ReadDir(projectDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect project data: %w", err)
		}
		if err := os.MkdirAll(trashDir, 0700); err != nil {
			return fmt.Errorf("failed to create project trash: %w", err)
		}
		var movedNames []string
		rollbackMoved := func() error {
			return rollbackMovedProjectEntries(projectDir, trashDir, movedNames, os.Rename)
		}
		legacyName := filepath.Base(claim.legacyLockPathHeld)
		for _, entry := range entries {
			if entry.Name() == legacyName {
				continue
			}
			if err := os.Rename(filepath.Join(projectDir, entry.Name()), filepath.Join(trashDir, entry.Name())); err != nil {
				return errors.Join(fmt.Errorf("failed to move project data to trash: %w", err), rollbackMoved())
			}
			movedNames = append(movedNames, entry.Name())
		}

		if err := s.saveProjectsLocked(projectsData); err != nil {
			if rollbackErr := rollbackMoved(); rollbackErr != nil {
				return fmt.Errorf("failed to save projects: %v (also failed to restore project data: %w)", err, rollbackErr)
			}
			return err
		}
		if err := os.RemoveAll(trashDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to clean deleted project data %s: %v\n", trashDir, err)
		}
		return nil
	})
}

func (s *Storage) beginProjectDeletion(projectID string) (func(), error) {
	marker := s.projectDeletionLockPath(projectID)
	if err := claimPIDLockPath(marker); err != nil {
		return nil, fmt.Errorf("failed to claim project deletion: %w", err)
	}
	cleanup := func() { removeProjectLockIfOwned(marker, os.Getpid()) }
	entries, err := os.ReadDir(s.projectReaderDir(projectID))
	if err != nil && !os.IsNotExist(err) {
		cleanup()
		return nil, fmt.Errorf("failed to inspect project viewers: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(s.projectReaderDir(projectID), entry.Name())
		if viewing, pid := projectLockOwner(path); viewing {
			cleanup()
			return nil, &ErrProjectLocked{PID: pid}
		}
		// The deletion marker is already visible, so an initializing reader
		// whose empty/stale claim is removed here must observe the marker on its
		// mandatory recheck and fail rather than entering the project.
		_ = os.Remove(path)
	}
	return cleanup, nil
}

func rollbackMovedProjectEntries(projectDir, trashDir string, movedNames []string, move func(string, string) error) error {
	var rollbackErr error
	for i := len(movedNames) - 1; i >= 0; i-- {
		name := movedNames[i]
		if err := move(filepath.Join(trashDir, name), filepath.Join(projectDir, name)); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("failed to restore project entry %s: %w", name, err))
		}
	}
	return rollbackErr
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
	return s.importDefaultSessions(projectID, s.saveAllLocked)
}

func (s *Storage) importDefaultSessions(projectID string, save func([]*Instance, []*Group, *Settings) error) (int, error) {
	if !validProjectID(projectID) || projectID == "" {
		return 0, fmt.Errorf("invalid project ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	originalProject := s.projectID
	defer func() { _ = s.setActiveProjectLocked(originalProject) }()

	// Load default sessions
	s.projectID = ""
	s.configPath = filepath.Join(s.configDir, "sessions.json")
	defaultInstances, defaultGroups, defaultSettings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return 0, err
	}

	if len(defaultInstances) == 0 {
		return 0, nil
	}

	// Switch to target project
	if err := s.setActiveProjectLocked(projectID); err != nil {
		return 0, err
	}

	// Load project's existing sessions
	projectInstances, projectGroups, projectSettings, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return 0, err
	}
	originalTarget, err := cloneStorageData(&StorageData{
		Instances: projectInstances, Groups: projectGroups, Settings: projectSettings,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to snapshot target before import: %w", err)
	}
	source, err := cloneStorageData(&StorageData{Instances: defaultInstances, Groups: defaultGroups})
	if err != nil {
		return 0, fmt.Errorf("failed to snapshot default sessions: %w", err)
	}

	groupByName := make(map[string]string, len(projectGroups))
	usedGroupIDs := make(map[string]bool, len(projectGroups))
	for _, group := range projectGroups {
		if group == nil {
			return 0, fmt.Errorf("target contains a null group")
		}
		groupByName[group.Name] = group.ID
		usedGroupIDs[group.ID] = true
	}
	groupRemap := make(map[string]string, len(source.Groups))
	targetChanged := false
	for _, group := range source.Groups {
		if group == nil {
			return 0, fmt.Errorf("default storage contains a null group")
		}
		sourceID := group.ID
		if sourceID == "" || strings.TrimSpace(group.Name) == "" {
			return 0, fmt.Errorf("default storage contains a group with an empty ID or name")
		}
		if existingID := groupByName[group.Name]; existingID != "" {
			groupRemap[sourceID] = existingID
			continue
		}
		newID := group.ID
		for newID == "" || usedGroupIDs[newID] {
			newID = fmt.Sprintf("grp_%d", time.Now().UnixNano())
		}
		group.ID = newID
		groupRemap[sourceID] = newID
		projectGroups = append(projectGroups, group)
		groupByName[group.Name] = newID
		usedGroupIDs[newID] = true
		targetChanged = true
	}

	existingByID := make(map[string]*Instance, len(projectInstances))
	for _, instance := range projectInstances {
		if instance == nil {
			return 0, fmt.Errorf("target contains a null session")
		}
		existingByID[instance.ID] = instance
	}
	for _, instance := range source.Instances {
		if instance == nil {
			return 0, fmt.Errorf("default storage contains a null session")
		}
		if remapped, ok := groupRemap[instance.GroupID]; ok {
			instance.GroupID = remapped
		}
		if existing := existingByID[instance.ID]; existing != nil {
			if !reflect.DeepEqual(existing, instance) {
				return 0, fmt.Errorf("target already contains a different session with ID %q", instance.ID)
			}
			continue // retry after a target-only partial commit
		}
		projectInstances = append(projectInstances, instance)
		existingByID[instance.ID] = instance
		targetChanged = true
	}

	if targetChanged {
		if err := save(projectInstances, projectGroups, projectSettings); err != nil {
			return 0, err
		}
	}

	// Clear only what was moved. Settings are preferences of the default
	// project, not session payload, and must survive this migration.
	s.projectID = ""
	s.configPath = filepath.Join(s.configDir, "sessions.json")
	if err := save([]*Instance{}, []*Group{}, defaultSettings); err != nil {
		if !targetChanged {
			return len(defaultInstances), err
		}
		_ = s.setActiveProjectLocked(projectID)
		rollbackErr := save(originalTarget.Instances, originalTarget.Groups, originalTarget.Settings)
		return len(defaultInstances), errors.Join(err, rollbackErr)
	}

	return len(defaultInstances), nil
}

// refreshInstanceStatuses updates each instance's Status by probing tmux,
// concurrently and WITHOUT holding s.mu. Called by the public Load* entry
// points after the lock is released so the per-instance `tmux has-session`
// subprocesses don't serialize the storage mutex.
func refreshInstanceStatuses(instances []*Instance) {
	refreshInstanceStatusesContext(context.Background(), instances)
}

func refreshInstanceStatusesContext(ctx context.Context, instances []*Instance) {
	refreshInstanceStatusesWith(ctx, instances, func(ctx context.Context, instance *Instance) {
		instance.UpdateStatusContext(ctx)
	})
}

const maxConcurrentStatusRefreshes = 16

func refreshInstanceStatusesWith(ctx context.Context, instances []*Instance, refresh func(context.Context, *Instance)) {
	if len(instances) == 0 {
		return
	}
	workers := min(len(instances), maxConcurrentStatusRefreshes)
	jobs := make(chan *Instance)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for instance := range jobs {
				if instance != nil {
					refresh(ctx, instance)
				}
			}
		}()
	}
	for _, instance := range instances {
		select {
		case jobs <- instance:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
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
	return s.LoadAllWithProjectSnapshotContext(context.Background())
}

// LoadAllWithProjectSnapshotContext lets lifecycle-owned readers cancel the
// concurrent tmux status probes after the persisted snapshot is loaded.
func (s *Storage) LoadAllWithProjectSnapshotContext(ctx context.Context) (string, []*Instance, []*Group, error) {
	s.mu.Lock()
	projectID := s.projectID
	instances, groups, _, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatusesContext(ctx, instances)
	}
	return projectID, instances, groups, err
}

// LoadAllWithSettings loads instances, groups, and settings
func (s *Storage) LoadAllWithSettings() ([]*Instance, []*Group, *Settings, error) {
	return s.LoadAllWithSettingsContext(context.Background())
}

// LoadAllWithSettingsContext is the cancellable form for lifecycle-owned
// readers that also need refreshed runtime statuses.
func (s *Storage) LoadAllWithSettingsContext(ctx context.Context) ([]*Instance, []*Group, *Settings, error) {
	s.mu.Lock()
	instances, groups, settings, err := s.loadAllWithSettingsLocked()
	s.mu.Unlock()
	if err == nil {
		refreshInstanceStatusesContext(ctx, instances)
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
	data, err := readFileAtMost(s.configPath, maxCanonicalStorageBytes)
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
	if err := validateCanonicalStorageSafety(&storageData); err != nil {
		return nil, fmt.Errorf("failed to validate config file: %w", err)
	}

	return &storageData, nil
}

// validateCanonicalStorageSafety rejects shapes that otherwise turn a
// recoverable corrupt file into a process panic or an ambiguous destructive
// mutation. It is intentionally narrower than restore validation: older
// stores may contain duplicate display names or followed-window metadata that
// the runtime knows how to repair, but nil objects and duplicate identities
// can never be addressed safely.
func validateCanonicalStorageSafety(data *StorageData) error {
	instanceIDs := make(map[string]struct{}, len(data.Instances))
	for index, instance := range data.Instances {
		if instance == nil {
			return fmt.Errorf("instance %d is null", index)
		}
		if strings.TrimSpace(instance.ID) == "" {
			return fmt.Errorf("instance %d has an empty ID", index)
		}
		if _, duplicate := instanceIDs[instance.ID]; duplicate {
			return fmt.Errorf("duplicate instance ID %q", instance.ID)
		}
		instanceIDs[instance.ID] = struct{}{}
	}

	groupIDs := make(map[string]struct{}, len(data.Groups))
	for index, group := range data.Groups {
		if group == nil {
			return fmt.Errorf("group %d is null", index)
		}
		if strings.TrimSpace(group.ID) == "" {
			return fmt.Errorf("group %d has an empty ID", index)
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return fmt.Errorf("duplicate group ID %q", group.ID)
		}
		groupIDs[group.ID] = struct{}{}
	}

	trashIDs := make(map[string]struct{}, len(data.Trash))
	for index, entry := range data.Trash {
		if entry == nil {
			return fmt.Errorf("trash entry %d is null", index)
		}
		if strings.TrimSpace(entry.ID) == "" {
			return fmt.Errorf("trash entry %d has an empty ID", index)
		}
		if _, duplicate := trashIDs[entry.ID]; duplicate {
			return fmt.Errorf("duplicate trash ID %q", entry.ID)
		}
		trashIDs[entry.ID] = struct{}{}
		switch entry.Kind {
		case "session":
			if entry.Session == nil {
				return fmt.Errorf("trash session %q has no session payload", entry.ID)
			}
		case "tab":
			if entry.Tab == nil || strings.TrimSpace(entry.ParentSessionID) == "" {
				return fmt.Errorf("trash tab %q has no tab payload or parent", entry.ID)
			}
		default:
			return fmt.Errorf("trash entry %q has unknown kind %q", entry.ID, entry.Kind)
		}
	}
	return nil
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
	return s.CaptureCodexResumeIDsForProjectContext(context.Background(), projectID, instanceID)
}

// CaptureCodexResumeIDsForProjectContext cancels the live tmux detector while
// retaining the same locked reload/merge transaction.
func (s *Storage) CaptureCodexResumeIDsForProjectContext(ctx context.Context, projectID, instanceID string) (bool, error) {
	return s.captureCodexResumeIDsForProject(
		projectID,
		instanceID,
		func(tmuxSession string, windowIdx int, expectedCWD string) string {
			return DetectCodexSessionIDFromTmuxContext(ctx, tmuxSession, windowIdx, expectedCWD)
		},
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
	if !validProjectID(projectID) {
		return nil, nil, fmt.Errorf("invalid project ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var instances []*Instance
	var groups []*Group
	err := s.withTemporaryProjectReader(projectID, func() error {
		// Check the catalog after publishing the reader claim. A deletion that
		// already committed is rejected without creating its directory; a
		// deletion that starts now sees the claim and must wait/fail.
		if err := s.requireProjectExists(projectID); err != nil {
			return err
		}
		originalProject, originalPath := s.projectID, s.configPath
		defer func() {
			s.projectID = originalProject
			s.configPath = originalPath
		}()
		s.projectID = projectID
		if projectID == "" {
			s.configPath = filepath.Join(s.configDir, "sessions.json")
		} else {
			s.configPath = filepath.Join(s.configDir, "projects", projectID, "sessions.json")
		}
		var loadErr error
		instances, groups, _, loadErr = s.loadAllWithSettingsLocked()
		return loadErr
	})
	return instances, groups, err
}
