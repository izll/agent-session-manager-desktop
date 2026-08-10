package session

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session/filters"
	"github.com/google/uuid"
	"github.com/mattn/go-runewidth"
)

// ansiRegex matches ANSI escape sequences
var (
	ansiRegex        = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cssHexColorRegex = regexp.MustCompile(`^(?:#[0-9a-fA-F]{3}|#[0-9a-fA-F]{4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8})$`)
)

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

type Status string

const (
	StatusRunning Status = "running"
	StatusPaused  Status = "paused"
	StatusStopped Status = "stopped"
)

// AgentType represents the type of AI agent
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentGemini   AgentType = "gemini"
	AgentAider    AgentType = "aider"
	AgentCodex    AgentType = "codex"
	AgentAmazonQ  AgentType = "amazonq"
	AgentOpenCode AgentType = "opencode"
	AgentCursor   AgentType = "cursor"
	AgentCustom   AgentType = "custom"
	AgentTerminal AgentType = "terminal" // Plain shell/terminal window
)

// AgentConfig contains configuration for each agent type
type AgentConfig struct {
	Command            string // Base command to run
	SupportsResume     bool   // Whether agent supports session resume
	SupportsAutoYes    bool   // Whether agent has auto-approve flag
	AutoYesFlag        string // The flag for auto-approve (e.g., "--dangerously-skip-permissions")
	ResumeFlag         string // The flag for resume (e.g., "--resume")
	ResumeIsSubcommand bool   // If true, resume is a subcommand (e.g., "codex resume") not a flag
	SupportsSessionID  bool   // Whether agent supports --session-id flag (pre-assigned session ID)
	SessionIDFlag      string // The flag for session ID (e.g., "--session-id")

	/**
	 * How this agent branches a conversation.
	 *
	 * ForkFlag is added to a resume to make it start a new conversation from
	 * the same history rather than continuing the old one. Where the agent
	 * takes a fork SUBCOMMAND instead (codex fork <id>), ForkIsSubcommand says
	 * so and ForkFlag is that subcommand's name.
	 *
	 * Empty ForkFlag means the agent cannot fork, and the UI says so rather
	 * than offering something that will fail.
	 */
	ForkFlag         string
	ForkIsSubcommand bool
}

// AgentConfigs maps agent types to their configurations
var AgentConfigs = map[AgentType]AgentConfig{
	AgentClaude: {
		Command:           "claude",
		SupportsResume:    true,
		SupportsAutoYes:   true,
		AutoYesFlag:       "--dangerously-skip-permissions",
		ResumeFlag:        "--resume",
		SupportsSessionID: true,
		SessionIDFlag:     "--session-id",
		// --fork-session alongside --resume: same history, new conversation.
		ForkFlag: "--fork-session",
	},
	AgentGemini: {
		Command:         "gemini",
		SupportsResume:  true,
		SupportsAutoYes: false,
		ResumeFlag:      "--resume",
	},
	AgentAider: {
		Command:         "aider",
		SupportsResume:  false,
		SupportsAutoYes: true,
		AutoYesFlag:     "--yes",
	},
	AgentCodex: {
		Command:         "codex",
		SupportsResume:  true,
		SupportsAutoYes: true,
		// Codex CLI removed `--full-auto`. The closest replacement (skips
		// all confirmations and runs commands without sandboxing) is
		// `--dangerously-bypass-approvals-and-sandbox`.
		AutoYesFlag:        "--dangerously-bypass-approvals-and-sandbox",
		ResumeFlag:         "resume",
		ResumeIsSubcommand: true,
		// `codex fork <id>` — a subcommand of its own rather than a flag on
		// resume, and the prompt it takes is optional, so it starts
		// interactively on the branch.
		ForkFlag:         "fork",
		ForkIsSubcommand: true,
	},
	AgentAmazonQ: {
		Command:            "q",
		SupportsResume:     true,
		SupportsAutoYes:    true,
		AutoYesFlag:        "--trust-all-tools",
		ResumeFlag:         "chat --resume",
		ResumeIsSubcommand: true,
	},
	AgentOpenCode: {
		Command:         "opencode",
		SupportsResume:  true,
		SupportsAutoYes: false,
		ResumeFlag:      "--session",
	},
	AgentCursor: {
		Command:         "cursor",
		SupportsResume:  false,
		SupportsAutoYes: false,
	},
	AgentCustom: {
		Command:         "",
		SupportsResume:  false,
		SupportsAutoYes: false,
	},
}

type Instance struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	Status          Status    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	AutoYes         bool      `json:"auto_yes"`
	HideStatusLine  bool      `json:"hide_status_line,omitempty"`  // Don't show the main window's status line in the session list
	ResumeSessionID string    `json:"resume_session_id,omitempty"` // Claude session ID to resume
	// ForkFrom names a conversation this session should BRANCH from on its
	// first start, rather than continue. Not stored: it is true of that one
	// start and nothing after it — restarting a forked session resumes the
	// branch, which is what ResumeSessionID holds by then.
	ForkFrom          string           `json:"-"`
	Color             string           `json:"color,omitempty"`               // Foreground color
	BgColor           string           `json:"bg_color,omitempty"`            // Background color
	FullRowColor      bool             `json:"full_row_color,omitempty"`      // Extend background to full row
	GroupID           string           `json:"group_id,omitempty"`            // Session group ID
	Agent             AgentType        `json:"agent,omitempty"`               // Agent type (claude, gemini, aider, custom)
	CustomCommand     string           `json:"custom_command,omitempty"`      // Custom command for AgentCustom
	ExtraArgs         string           `json:"extra_args,omitempty"`          // Extra CLI arguments appended to agent command
	Notes             string           `json:"notes,omitempty"`               // User notes/comments for this session
	FollowedWindows   []FollowedWindow `json:"followed_windows,omitempty"`    // Windows tracked as agents (window 0 is main agent)
	BaseCommitSHA     string           `json:"base_commit_sha,omitempty"`     // Git HEAD commit at session start (for diff)
	Favorite          bool             `json:"favorite,omitempty"`            // Whether session is marked as favorite
	MainWindowStopped bool             `json:"main_window_stopped,omitempty"` // Main window (0) is stopped but session still running
	TabOrder          []int            `json:"tab_order,omitempty"`           // Custom tab display order (tmux window indices); if empty, default order is used
	TerminalTheme     string           `json:"terminal_theme,omitempty"`      // Main window colour palette (empty inherits agent/global)
	TerminalFontSize  int              `json:"terminal_font_size,omitempty"`  // Main window font size in px (0 inherits the global setting)
	// HideViewBar is tri-state: 0 follows the global setting, 1 hides, 2 shows.
	// A plain bool could not express "explicitly shown" against a global hide.
	HideViewBar   int `json:"hide_view_bar,omitempty"`
	HideStatusBar int `json:"hide_status_bar,omitempty"`
	// LastWindowIndex is the tab that was open when the session was last
	// left, so reopening it lands where the user was. Advisory only: the
	// window may be gone by then, so callers must validate it.
	LastWindowIndex    int    `json:"last_window_index,omitempty"`
	TabTextColor       string `json:"tab_text_color,omitempty"`       // Main tab text color (empty uses the theme default)
	TabBackgroundColor string `json:"tab_background_color,omitempty"` // Main tab background color (empty uses the theme default)
}

// DiffStats contains git diff statistics and content
type DiffStats struct {
	Added   int    // Number of added lines
	Removed int    // Number of removed lines
	Content string // Raw diff content
	Error   error  // Error if diff failed
}

// IsEmpty returns true if there are no changes
func (d *DiffStats) IsEmpty() bool {
	return d == nil || (d.Added == 0 && d.Removed == 0 && d.Content == "")
}

// FollowedWindow represents a tmux window tracked as an agent
type FollowedWindow struct {
	Index            int       `json:"index"`
	Agent            AgentType `json:"agent"`
	Name             string    `json:"name"`                         // Tab name for display
	CustomCommand    string    `json:"custom_command"`               // For custom agents
	AutoYes          bool      `json:"auto_yes"`                     // YOLO mode for this tab
	ResumeSessionID  string    `json:"resume_session_id"`            // Resume session ID for this tab
	Notes            string    `json:"notes,omitempty"`              // User notes for this tab
	ExtraArgs        string    `json:"extra_args,omitempty"`         // Extra CLI arguments for this tab
	Stopped          bool      `json:"stopped,omitempty"`            // Tab is stopped (window killed but can resume)
	TerminalTheme    string    `json:"terminal_theme,omitempty"`     // Tab colour palette (empty inherits agent/global)
	TerminalFontSize int       `json:"terminal_font_size,omitempty"` // Tab font size in px (0 inherits the global setting)
	HideViewBar      int       `json:"hide_view_bar,omitempty"`      // 0 inherit, 1 hide, 2 show
	HideStatusBar    int       `json:"hide_status_bar,omitempty"`    // 0 inherit, 1 hide, 2 show
	TextColor        string    `json:"text_color,omitempty"`         // Tab text color (empty uses the theme default)
	BackgroundColor  string    `json:"background_color,omitempty"`   // Tab background color (empty uses the theme default)
	WorkDir          string    `json:"work_dir,omitempty"`           // Tab working directory (empty = session path)
	HideStatusLine   bool      `json:"hide_status_line,omitempty"`   // Don't show this tab's status line in the session list
}

// GetAgentConfig returns the agent configuration for this instance
func (i *Instance) GetAgentConfig() AgentConfig {
	agent := i.Agent
	if agent == "" {
		agent = AgentClaude // Default to Claude for backward compatibility
	}
	if config, ok := AgentConfigs[agent]; ok {
		return config
	}
	return AgentConfigs[AgentClaude]
}

// WindowName returns the display name for the main tmux window (agent type)
func (i *Instance) WindowName() string {
	agent := i.Agent
	if agent == "" {
		agent = AgentClaude
	}
	return string(agent)
}

// expandTilde expands ~ to user's home directory
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(homeDir, path[2:])
		}
	} else if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return homeDir
		}
	}
	return path
}

func NewInstance(name, path string, autoYes bool, agent AgentType, extraArgs string) (*Instance, error) {
	// Expand ~ to home directory
	path = expandTilde(path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", absPath)
	}

	id := generateID(name, agent)
	now := time.Now()

	return &Instance{
		ID:        id,
		Name:      name,
		Path:      absPath,
		Status:    StatusStopped,
		CreatedAt: now,
		UpdatedAt: now,
		AutoYes:   autoYes,
		Agent:     agent,
		ExtraArgs: extraArgs,
	}, nil
}

// sanitizeSessionName strips what a multiplexer cannot carry in a session name.
//
// The ID becomes the tmux session name, and targets are built as
// "session:window" in ~30 places — so a colon in the name silently addresses
// the wrong window. On Windows a user may well name a session after its
// directory, and "C:\Users\User\Documents\asmgr-teszt" contains both a
// colon and backslashes. Dots are replaced too: tmux reads "session:win.pane"
// and would take a trailing ".1" as a pane index.
func sanitizeSessionName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			// Everything else — spaces, colons, slashes, dots, accented
			// letters — becomes an underscore. Collapsing runs of them keeps
			// a path from turning into a wall of underscores.
			if s := b.String(); s == "" || !strings.HasSuffix(s, "_") {
				b.WriteByte('_')
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func generateID(name string, agent AgentType) string {
	sanitized := sanitizeSessionName(name)
	timestamp := time.Now().UnixNano()
	agentStr := string(agent)
	if agentStr == "" {
		agentStr = "claude"
	}
	return fmt.Sprintf("asm_%s_%s_%d", agentStr, sanitized, timestamp)
}

func (i *Instance) TmuxSessionName() string {
	return i.ID
}

// captureTargetCache caches GUI session lookups per instance+window to avoid
// running `tmux list-sessions` on every capture (called multiple times per poll cycle).
var captureTargetCache sync.Map // map[string]captureTargetEntry

type captureTargetEntry struct {
	target  string
	expires time.Time
}

const captureTargetCacheTTL = 2 * time.Second

// GetCaptureTarget returns the best tmux target for capture-pane for a given window.
// It prefers an attached GUI session (created by the WebSocket terminal) because those
// have the up-to-date pane content. Falls back to the base session if no GUI session is found.
func (i *Instance) GetCaptureTarget(windowIdx int) string {
	baseName := i.TmuxSessionName()
	cacheKey := fmt.Sprintf("%s:%d", baseName, windowIdx)

	// Check cache first
	if cached, ok := captureTargetCache.Load(cacheKey); ok {
		entry := cached.(captureTargetEntry)
		if time.Now().Before(entry.expires) {
			return entry.target
		}
	}

	baseTarget := cacheKey

	// List tmux sessions matching the GUI pattern for this window
	prefix := fmt.Sprintf("%s_gui_%d_", baseName, windowIdx)
	cmd := TmuxCommand("list-sessions", "-F", "#{session_name} #{session_attached}")
	output, err := cmd.Output()
	if err != nil {
		captureTargetCache.Store(cacheKey, captureTargetEntry{target: baseTarget, expires: time.Now().Add(captureTargetCacheTTL)})
		return baseTarget
	}

	// Find the best GUI session: prefer attached, otherwise latest (highest timestamp)
	var bestAttached, bestAny string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		bestAny = name // later entries have higher timestamps
		if parts[1] == "1" {
			bestAttached = name
		}
	}

	var result string
	if bestAttached != "" {
		result = fmt.Sprintf("%s:%d", bestAttached, windowIdx)
	} else if bestAny != "" {
		result = fmt.Sprintf("%s:%d", bestAny, windowIdx)
	} else {
		result = baseTarget
	}

	captureTargetCache.Store(cacheKey, captureTargetEntry{target: result, expires: time.Now().Add(captureTargetCacheTTL)})
	return result
}

// CheckAgentCommand verifies that the agent command exists in PATH
func CheckAgentCommand(inst *Instance) error {
	var cmdToCheck string

	// A terminal session launches no agent — tmux just opens the user's
	// shell, so there is nothing to look up in PATH.
	if inst.Agent == AgentTerminal {
		return nil
	}

	if inst.Agent == AgentCustom {
		// Extract the base command (first token) from custom command,
		// using the same quote-aware splitter the launcher uses.
		parts := customCommandArgv(inst.CustomCommand)
		if len(parts) > 0 {
			cmdToCheck = parts[0]
		}
	} else {
		config := inst.GetAgentConfig()
		cmdToCheck = config.Command
	}

	if cmdToCheck == "" {
		return fmt.Errorf("no command specified")
	}

	if _, err := exec.LookPath(cmdToCheck); err != nil {
		return fmt.Errorf("command '%s' not found - is it installed?", cmdToCheck)
	}

	return nil
}

func (i *Instance) Start() error {
	return i.StartWithResume("")
}

// startLocks serialises starts per session name.
//
// The existence check and the new-session that follows it are not atomic, and
// the multiplexer takes seconds to register a new session — long enough for a
// second start to look in, see nothing, and create a duplicate. That leaves two
// servers answering to one name: one holds the client, the other does not, and
// killing the working one appears to "fix" the broken one, which is exactly how
// this was reported.
//
// Keyed by session name rather than held on Instance, because callers can hold
// different Instance values for the same session.
var startLocks sync.Map // map[string]*sync.Mutex

func lockSessionStart(name string) func() {
	v, _ := startLocks.LoadOrStore(name, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (i *Instance) StartWithResume(resumeID string) error {
	// Nothing below can work without the multiplexer, and every command that
	// tries fails on its own terms — "exec: no such file", repeated once per
	// call. Said plainly once, before any of them run, and with how to install
	// it, since a user meeting this has no other way to find out.
	if err := CheckMultiplexer(); err != nil {
		return err
	}

	// Held for the whole start: releasing after the existence check would
	// reopen the very window this closes.
	unlock := lockSessionStart(i.TmuxSessionName())
	defer unlock()

	log.Printf("[StartWithResume] session=%s agent=%s resumeID=%q saved_ResumeSessionID=%q", i.ID, i.Agent, resumeID, i.ResumeSessionID)

	// If the conversation is currently held by a Claude background agent
	// (Ctrl+B / --bg), `claude --resume` would refuse to start — free it
	// first so the tab actually comes back.
	if i.Agent == AgentClaude {
		if id := resumeID; id != "" {
			ReleaseClaudeBackgroundAgent(id)
		} else if i.ResumeSessionID != "" {
			ReleaseClaudeBackgroundAgent(i.ResumeSessionID)
		}
	}

	// Update status based on actual tmux session state
	// This handles cases where session was killed externally
	i.UpdateStatus()

	if i.Status == StatusRunning {
		return fmt.Errorf("instance already running")
	}

	sessionName := i.TmuxSessionName()

	// Check if tmux session already exists.
	//
	// A single negative answer is not enough when this session was started
	// moments ago: psmux forks a server per session and takes a noticeable time
	// to answer for it, so a start that overlaps a previous one would see
	// nothing and create a SECOND server under the same name. Two servers for
	// one name is not self-correcting — one holds the client and the other does
	// not, and only killing one by hand resolves it.
	//
	// The wait is therefore spent only where that race is possible: right after
	// a recent start of this same session. A cold start pays nothing.
	sessionExists := TmuxCommand("has-session", "-t", sessionName).Run() == nil
	if !sessionExists && recentlyStarted(sessionName) {
		for attempt := 0; attempt < 5 && !sessionExists; attempt++ {
			time.Sleep(300 * time.Millisecond)
			sessionExists = TmuxCommand("has-session", "-t", sessionName).Run() == nil
		}
		if sessionExists {
			log.Printf("[StartWithResume] %s appeared after a slow registration; not starting a second one", sessionName)
		}
	}

	if !sessionExists {
		// Build command based on agent type
		config := i.GetAgentConfig()
		var argv []string // tmux command in argv form (no shell layer)
		var cmdToCheck string

		if i.Agent == AgentTerminal {
			// Plain shell session — no agent to launch. Leaving argv empty
			// makes tmux start the user's default shell, exactly like a
			// terminal TAB does (see restoreFollowedWindows).
			argv = nil
		} else if i.Agent == AgentCustom {
			// Use custom command directly, split into argv tokens.
			argv = customCommandArgv(i.CustomCommand)
			if len(argv) > 0 {
				cmdToCheck = argv[0]
			}
		} else {
			cmdToCheck = config.Command
			args := []string{}

			// A fork branches on this one start: the agent loads the source
			// conversation and carries on in a new one. Nothing runs
			// beforehand, so it costs no turn and no waiting.
			if i.ForkFrom != "" && config.ForkFlag != "" {
				if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
				args = appendForkArgs(config, args, i.ForkFrom)
				// Where the agent lets us name the branch, do — otherwise the
				// session has nothing to resume from until a poll finds what the
				// agent chose for itself.
				if config.SupportsSessionID && config.SessionIDFlag != "" {
					newID := uuid.New().String()
					args = append(args, config.SessionIDFlag, newID)
					i.ResumeSessionID = newID
				} else {
					i.ResumeSessionID = ""
				}
				i.ForkFrom = ""
			} else if config.SupportsResume && config.ResumeIsSubcommand {
				// Resume is a subcommand - put it first, then flags, then session ID
				if resumeID != "" || i.ResumeSessionID != "" {
					// Add resume subcommand
					args = append(args, config.ResumeFlag)

					// Add auto-yes flag after subcommand if supported
					if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
						args = append(args, config.AutoYesFlag)
					}

					// Add session ID
					if resumeID != "" {
						args = append(args, resumeID)
						i.ResumeSessionID = resumeID
					} else if i.ResumeSessionID != "" {
						args = append(args, i.ResumeSessionID)
					}
				} else {
					// No resume - just add auto-yes flag if needed
					if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
						args = append(args, config.AutoYesFlag)
					}
				}
			} else {
				// Resume is a flag - add auto-yes first, then resume flag
				// Add auto-yes flag if supported and enabled
				if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}

				// Add resume flag if supported and specified
				if config.SupportsResume && config.ResumeFlag != "" {
					if resumeID != "" {
						args = append(args, config.ResumeFlag, resumeID)
						i.ResumeSessionID = resumeID
					} else if i.ResumeSessionID != "" {
						args = append(args, config.ResumeFlag, i.ResumeSessionID)
					} else if config.SupportsSessionID && config.SessionIDFlag != "" {
						// New session with pre-assigned session ID (like VS Code extension)
						newID := uuid.New().String()
						args = append(args, config.SessionIDFlag, newID)
						i.ResumeSessionID = newID
					}
				}
			}

			argv = buildAgentArgv(config.Command, args, i.ExtraArgs)
		}

		// Check if the command exists
		if cmdToCheck != "" {
			if _, err := exec.LookPath(cmdToCheck); err != nil {
				return fmt.Errorf("command '%s' not found - is it installed?", cmdToCheck)
			}
		}

		// Create new tmux session. Pass the agent command as SEPARATE argv
		// elements so tmux execs it directly instead of via `sh -c` — this
		// is what makes ExtraArgs/CustomCommand shell-metachars inert.
		// The binary is named from TmuxBinary rather than written in: on Windows it
		// is psmux, and a log that always said "tmux" sent debugging down the
		// wrong path entirely.
		log.Printf("[StartWithResume] final argv: %s new-session -d -s %s -c %s -- %v", TmuxBinary(), sessionName, i.Path, argv)
		tmuxArgs := append([]string{"new-session", "-d", "-s", sessionName, "-c", i.Path}, argv...)
		cmd := TmuxCommand(tmuxArgs...)
		// Pin a sane TERM for the session's child processes. Launched from a
		// desktop menu / KRunner the app inherits TERM=dumb (or empty), which
		// would propagate into the agent running inside tmux.
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		// Recorded before Run: the mark is what tells a subsequent start to
		// wait for a slow registration rather than create a duplicate, and the
		// window it guards opens the moment the command is issued.
		markStarted(sessionName)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}

		// Wait for session to be ready
		for j := 0; j < 20; j++ {
			checkCmd := TmuxCommand("has-session", "-t", sessionName)
			if checkCmd.Run() == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Keep windows alive when their process exits (shows as dead pane)
		TmuxCommand("set-option", "-t", sessionName, "remain-on-exit", "on").Run()

		// Configure tmux session for better scrolling
		TmuxCommand("set-option", "-t", sessionName, "history-limit", "50000").Run()
		TmuxCommand("set-option", "-t", sessionName, "mouse", "on").Run()

		// Hide tmux status bar (not needed in GUI, wastes a row)
		TmuxCommand("set-option", "-t", sessionName, "status", "off").Run()

		// Use latest client size and aggressive resize for proper terminal following
		TmuxCommand("set-option", "-t", sessionName, "window-size", "latest").Run()
		TmuxCommand("set-option", "-t", sessionName, "aggressive-resize", "on").Run()

		// Enable xterm keys for Shift+PageUp/Down support
		TmuxCommand("set-option", "-t", sessionName, "-g", "xterm-keys", "on").Run()

		// Set terminal overrides for better key support
		TmuxCommand("set-option", "-t", sessionName, "-ga", "terminal-overrides", ",xterm*:smcup@:rmcup@").Run()

		// Bind Shift+PageUp/Down for scrolling in copy mode (conditional - only in asmgr-* sessions)
		TmuxCommand("bind-key", "-T", "root", "S-PageUp", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "copy-mode -eu", "").Run()
		TmuxCommand("bind-key", "-T", "root", "S-PageDown", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys PageDown", "").Run()
		TmuxCommand("bind-key", "-T", "copy-mode-vi", "S-PageUp", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys -X page-up", "").Run()
		TmuxCommand("bind-key", "-T", "copy-mode-vi", "S-PageDown", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys -X page-down", "").Run()

		// Bind Ctrl+Y for yolo mode toggle (conditional - only in asmgr-* sessions)
		TmuxCommand("bind-key", "-n", "C-y", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", `run-shell 'asmgr yolo "$(tmux display-message -p "#{session_name}")" "$(tmux display-message -p "#{window_index}")" 2>/dev/null'`, "").Run()

		// Ctrl+q will be set up with resize in UpdateDetachBinding

		// Persist the main window identity on the tmux window object itself.
		// Unlike its numeric index, this marker survives move-window/renumbering.
		if mainWindowIdx, ok := soleTmuxWindowIndex(sessionName); ok {
			mainTarget := fmt.Sprintf("%s:%d", sessionName, mainWindowIdx)
			TmuxCommand("set-option", "-w", "-t", mainTarget, "@asmgr_main", "1").Run()
			TmuxCommand("rename-window", "-t", mainTarget, i.WindowName()).Run()
		}

		// Check if session is still alive after a short delay (detect immediate exit)
		time.Sleep(300 * time.Millisecond)
		if !i.IsAlive() {
			// Session died immediately - try to get output for error message
			return fmt.Errorf("session exited immediately - check if login or API key is required")
		}
	}

	i.Status = StatusRunning
	i.MainWindowStopped = false
	i.UpdatedAt = time.Now()

	// Save git HEAD commit for diff tracking (if in a git repo)
	i.saveBaseCommit()

	// Restore followed windows (tabs) if any
	i.restoreFollowedWindows()

	// A fresh Codex process usually has its rollout open by this point. Save
	// the generated ID in the same storage update as the start operation;
	// sidebar polling and stop/shutdown capture remain retries for slower starts.
	i.CaptureCodexResumeIDs()

	return nil
}

// saveBaseCommit saves the current git HEAD commit SHA for diff tracking
func (i *Instance) saveBaseCommit() {
	// Only save if not already set (preserve original base on restart)
	if i.BaseCommitSHA != "" {
		return
	}

	// Check if path is a git repo and get HEAD commit
	cmd := GitCommand("-C", i.Path, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// Not a git repo or error - no diff available
		return
	}

	i.BaseCommitSHA = strings.TrimSpace(string(output))
}

func newTmuxWindowCommand(sessionName, tabDir, name string, detached bool, argv []string) *exec.Cmd {
	tmuxArgs := []string{"new-window"}
	if detached {
		tmuxArgs = append(tmuxArgs, "-d")
	}
	tmuxArgs = append(tmuxArgs, "-P", "-F", "#{window_index}", "-t", sessionName, "-c", tabDir, "-n", name)
	tmuxArgs = append(tmuxArgs, argv...)
	return TmuxCommand(tmuxArgs...)
}

func parseTmuxWindowIndex(output []byte) (int, error) {
	var index int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &index); err != nil {
		return 0, err
	}
	return index, nil
}

func tmuxWindowExists(sessionName string, windowIdx int) bool {
	output, err := TmuxCommand("list-windows", "-t", sessionName, "-F", "#{window_index}").Output()
	if err != nil {
		return false
	}
	return tmuxWindowIndexListed(output, windowIdx)
}

func tmuxWindowIndexListed(output []byte, windowIdx int) bool {
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		var listedIdx int
		if _, err := fmt.Sscanf(line, "%d", &listedIdx); err == nil && listedIdx == windowIdx {
			return true
		}
	}
	return false
}

// restoreFollowedWindows recreates agent tabs after session restart
func (i *Instance) restoreFollowedWindows() {
	if len(i.FollowedWindows) == 0 {
		return
	}

	sessionName := i.TmuxSessionName()

	// Store old followed windows and clear the list (will be repopulated)
	oldWindows := i.FollowedWindows
	i.FollowedWindows = nil

	for _, fw := range oldWindows {
		var cmd *exec.Cmd
		resumeID := fw.ResumeSessionID
		tabDir := fw.WorkDir
		if tabDir == "" {
			tabDir = i.Path
		}
		if fw.Agent == AgentClaude && resumeID != "" {
			ReleaseClaudeBackgroundAgent(resumeID)
		}

		// Drop the saved resume ID if it no longer exists on disk so the
		// tab boots fresh instead of dying with "No conversation found".
		if resumeID != "" && !ResumeIDExists(fw.Agent, resumeID) {
			log.Printf("[restoreFollowedWindows] resume ID %q gone for agent=%s tab=%q — starting fresh", resumeID, fw.Agent, fw.Name)
			resumeID = ""
			fw.ResumeSessionID = ""
		}

		if fw.Stopped {
			// A restored trash item is deliberately brought back as a stopped
			// placeholder. Never launch an agent merely because its parent
			// session was started; the user can explicitly start this tab.
			cmd = newTmuxWindowCommand(sessionName, tabDir, fw.Name, true, nil)
		} else if fw.Agent == AgentTerminal {
			// Terminal window - just create empty shell
			cmd = newTmuxWindowCommand(sessionName, tabDir, fw.Name, false, nil)
		} else {
			// Agent window - build agent command (argv form, no shell)
			config := AgentConfigs[fw.Agent]
			var argv []string

			if fw.Agent == AgentCustom {
				argv = customCommandArgv(fw.CustomCommand)
			} else {
				args := []string{}
				autoYes := fw.AutoYes || i.AutoYes

				// Handle resume subcommands (codex resume, q chat --resume) vs flags (claude --resume)
				if config.SupportsResume && config.ResumeIsSubcommand {
					if resumeID != "" {
						args = append(args, config.ResumeFlag)
						if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
							args = append(args, config.AutoYesFlag)
						}
						args = append(args, resumeID)
					} else {
						if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
							args = append(args, config.AutoYesFlag)
						}
					}
				} else {
					if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
						args = append(args, config.AutoYesFlag)
					}
					if resumeID != "" && config.SupportsResume && config.ResumeFlag != "" {
						args = append(args, config.ResumeFlag, resumeID)
					} else if resumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" {
						resumeID = uuid.New().String()
						args = append(args, config.SessionIDFlag, resumeID)
						log.Printf("[restoreFollowedWindows] generated session-id=%s for tab %q agent=%s", resumeID, fw.Name, fw.Agent)
					}
				}
				argv = buildAgentArgv(config.Command, args, fw.ExtraArgs)
			}

			// Create new window with the agent command as separate argv
			// elements (tmux execs directly, no `sh -c`).
			cmd = newTmuxWindowCommand(sessionName, tabDir, fw.Name, false, argv)
		}

		output, err := cmd.Output()
		if err != nil {
			continue // Skip failed windows
		}

		// Read the index of the window that was just created. This must come
		// from `new-window -P`: stopped placeholders are created detached, so
		// querying the active window would return the main agent's index.
		newIdx, err := parseTmuxWindowIndex(output)
		if err != nil {
			log.Printf("[restoreFollowedWindows] invalid new-window index %q for tab %q: %v", strings.TrimSpace(string(output)), fw.Name, err)
			continue
		}

		// Set remain-on-exit so window stays open when command exits (shows as stopped)
		target := fmt.Sprintf("%s:%d", sessionName, newIdx)
		TmuxCommand("set-option", "-t", target, "remain-on-exit", "on").Run()
		// Disable automatic-rename so the window keeps the user-specified name
		TmuxCommand("set-option", "-t", target, "automatic-rename", "off").Run()
		if fw.Stopped {
			_ = TmuxCommand("respawn-pane", "-k", "-t", target, "exit 0").Run()
		}

		// Re-add to followed windows with updated index (preserve all fields)
		restored := fw
		restored.Index = newIdx
		restored.ResumeSessionID = resumeID
		i.FollowedWindows = append(i.FollowedWindows, restored)
	}

	// Clear TabOrder since window indices changed after restart
	i.TabOrder = nil

	// Switch back to the main agent window.
	if mainWindowIdx, ok := i.getMainWindowIndex(); ok {
		TmuxCommand("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, mainWindowIdx)).Run()
	}
}

func (i *Instance) Stop() error {
	// Codex only exposes its generated conversation ID after the process has
	// started. Capture it while the panes and their open rollout files still
	// exist, before killing the tmux session.
	i.CaptureCodexResumeIDs()

	// Same reason: once the session is killed, where each terminal tab had been
	// navigated to is gone with it.
	i.CaptureTerminalWorkingDirs()

	if i.Status != StatusRunning {
		return nil
	}

	sessionName := i.TmuxSessionName()

	// Kill all linked GUI sessions first (they share the same tmux session group).
	// Format: <sessionName>_gui_<N>_<timestamp>
	out, _ := TmuxCommand("list-sessions", "-F", "#{session_name}").Output()
	if out != nil {
		prefix := sessionName + "_gui_"
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasPrefix(line, prefix) {
				TmuxCommand("kill-session", "-t", line).Run()
			}
		}
	}

	// Kill the base tmux session
	cmd := TmuxCommand("kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		// If the base session is already gone (killed by group cascade), that's OK
		checkCmd := TmuxCommand("has-session", "-t", sessionName)
		if checkCmd.Run() == nil {
			return fmt.Errorf("failed to kill tmux session: %w", err)
		}
	}

	i.Status = StatusStopped
	i.MainWindowStopped = false
	for idx := range i.FollowedWindows {
		i.FollowedWindows[idx].Stopped = false
	}
	i.UpdatedAt = time.Now()

	return nil
}

func (i *Instance) Attach() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("attach-session", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// NewWindow creates a new tmux window in the session's directory
func (i *Instance) NewWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("new-window", "-t", sessionName, "-c", i.Path)
	return cmd.Run()
}

// NewWindowWithName creates a new tmux window with a specific name
func (i *Instance) NewWindowWithName(name string, workDir string) (int, error) {
	if workDir == "" {
		workDir = i.Path
	}
	if i.Status != StatusRunning {
		return -1, fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	output, err := newTmuxWindowCommand(sessionName, workDir, name, false, nil).Output()
	if err != nil {
		return -1, err
	}

	// Never infer the new index from the active client. A linked GUI tmux
	// client can keep a different window selected, which previously produced
	// duplicate metadata indices and made Codex tabs restart as terminals.
	newIdx, err := parseTmuxWindowIndex(output)
	if err != nil {
		return -1, fmt.Errorf("invalid new terminal window index: %w", err)
	}
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		WorkDir: func() string {
			if workDir != i.Path {
				return workDir
			}
			return ""
		}(),
		Index: newIdx,
		Agent: AgentTerminal,
		Name:  name,
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits (shows as stopped)
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	TmuxCommand("set-option", "-t", target, "remain-on-exit", "on").Run()
	// Disable automatic-rename so the window keeps the user-specified name
	TmuxCommand("set-option", "-t", target, "automatic-rename", "off").Run()

	return newIdx, nil
}

// StopWindow stops the agent in a specific tmux window.
// For window 0: if there are active followed windows, only kills the main agent
// process (keeps session alive). Otherwise kills the entire tmux session.
// For followed windows: kills the tmux window and marks the tab as stopped.
func (i *Instance) StopWindow(windowIdx int) error {
	// Capture before respawn-pane terminates the agent process.
	i.CaptureCodexResumeIDs()

	// And before the pane is gone, so a terminal tab restarts where it was
	// left rather than back at the session root.
	i.CaptureTerminalWorkingDir(windowIdx)

	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	mainWindowIdx, ok := i.getMainWindowIndex()
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", sessionName)
	}
	if !tmuxWindowExists(sessionName, windowIdx) {
		return fmt.Errorf("tmux window %s:%d not found", sessionName, windowIdx)
	}

	if windowIdx == mainWindowIdx {
		// Check if there are active (non-stopped) followed windows
		hasActiveFollowed := false
		for _, fw := range i.FollowedWindows {
			if !fw.Stopped {
				hasActiveFollowed = true
				break
			}
		}

		if !hasActiveFollowed {
			// No active followed windows - kill entire session
			return i.Stop()
		}

		// Has active followed windows - stop just the main agent process
		target := fmt.Sprintf("%s:%d", sessionName, mainWindowIdx)
		// Keep the window alive as a dead pane
		TmuxCommand("set-option", "-t", target, "remain-on-exit", "on").Run()
		// Kill the agent and replace with an immediately-exiting command
		if err := TmuxCommand("respawn-pane", "-k", "-t", target, "exit 0").Run(); err != nil {
			return fmt.Errorf("failed to stop main window: %w", err)
		}

		i.MainWindowStopped = true
		return nil
	}

	// Followed window: stop the process but keep the window (dead pane)
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
	if err := TmuxCommand("respawn-pane", "-k", "-t", target, "exit 0").Run(); err != nil {
		return fmt.Errorf("failed to stop window %s: %w", target, err)
	}

	// Mark the followed window as stopped
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			i.FollowedWindows[idx].Stopped = true
		}
	}

	return nil
}

// RestartWindow restarts a stopped window (dead pane) by respawning the agent process.
func (i *Instance) RestartWindowWithResume(windowIdx int, resumeID string) error {
	log.Printf("[RestartWindow] session=%s windowIdx=%d resumeID=%q saved_ResumeSessionID=%q agent=%s", i.ID, windowIdx, resumeID, i.ResumeSessionID, i.Agent)

	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	mainWindowIdx, ok := i.getMainWindowIndex()
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", sessionName)
	}
	if !tmuxWindowExists(sessionName, windowIdx) {
		return fmt.Errorf("tmux window %s:%d not found", sessionName, windowIdx)
	}
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	if windowIdx == mainWindowIdx {
		// Terminal session: no agent to restart, just bring the shell back.
		// respawn-pane without a command would re-run the pane's original
		// start command ("exit 0" for a stopped window), so pass the shell
		// explicitly — same as a terminal TAB restart below.
		if i.Agent == AgentTerminal {
			shell := defaultShell()
			target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
			// The session's main window has nowhere to record a directory of
			// its own — only followed tabs carry WorkDir — so it comes back at
			// the session path, which is also where it started. Passing it
			// explicitly matters because respawn-pane would otherwise reuse
			// wherever the dead pane happened to be left.
			args := []string{"respawn-pane", "-k"}
			args = append(args, restartDirArgs(i.Path)...)
			args = append(args, "-t", target, shell)
			if err := TmuxCommand(args...).Run(); err != nil {
				return fmt.Errorf("failed to restart terminal window: %w", err)
			}
			i.MainWindowStopped = false
			return nil
		}

		// Main window: restart the main agent
		config, ok := AgentConfigs[i.Agent]
		if !ok || config.Command == "" {
			return fmt.Errorf("cannot restart main window: unsupported agent %q", i.Agent)
		}
		args := []string{}
		// Use provided resume ID or saved one
		if resumeID == "" {
			resumeID = i.ResumeSessionID
		}
		if i.Agent == AgentClaude && resumeID != "" {
			ReleaseClaudeBackgroundAgent(resumeID)
		}

		// Handle resume subcommands (codex resume, q chat --resume) vs flags (claude --resume)
		if config.SupportsResume && config.ResumeIsSubcommand {
			if resumeID != "" {
				// Add resume subcommand first
				args = append(args, config.ResumeFlag)
				// Add auto-yes flag after subcommand if supported
				if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
				// Add session ID
				args = append(args, resumeID)
				i.ResumeSessionID = resumeID
			} else {
				// No resume - just add auto-yes flag if needed
				if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
			}
		} else {
			if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
				args = append(args, config.AutoYesFlag)
			}
			if resumeID != "" && config.SupportsResume && config.ResumeFlag != "" {
				args = append(args, config.ResumeFlag, resumeID)
				i.ResumeSessionID = resumeID
			} else if resumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" {
				// No resume ID — generate a fresh --session-id so the agent doesn't
				// prompt for resume and we can track the session for future restarts
				newID := uuid.New().String()
				args = append(args, config.SessionIDFlag, newID)
				i.ResumeSessionID = newID
				log.Printf("[RestartWindow] generated new session-id=%s for main window of session=%s", newID, i.ID)
			}
		}
		argv := buildAgentArgv(config.Command, args, i.ExtraArgs)
		log.Printf("[RestartWindow] win0 instance.ExtraArgs=%q final argv: %v", i.ExtraArgs, argv)
		tmuxArgs := append([]string{"respawn-pane", "-k", "-t", target}, argv...)
		if err := TmuxCommand(tmuxArgs...).Run(); err != nil {
			return fmt.Errorf("failed to restart main window: %w", err)
		}
		i.MainWindowStopped = false
		i.CaptureCodexResumeIDs()
		return nil
	}

	// Followed window: find the agent config and restart
	fwSliceIdx, collapseDuplicates, err := selectFollowedWindowForRestart(i.FollowedWindows, windowIdx)
	if err != nil {
		return err
	}
	if fwSliceIdx < 0 {
		log.Printf("[RestartWindow] window %d not found in followedWindows (count=%d)", windowIdx, len(i.FollowedWindows))
		for _, w := range i.FollowedWindows {
			log.Printf("[RestartWindow]   fw: index=%d agent=%s name=%q resumeID=%q stopped=%v", w.Index, w.Agent, w.Name, w.ResumeSessionID, w.Stopped)
		}
		return fmt.Errorf("window %d not found in followed windows", windowIdx)
	}
	fw := &i.FollowedWindows[fwSliceIdx]

	log.Printf("[RestartWindow] found fw: index=%d agent=%s name=%q resumeID=%q stopped=%v extraArgs=%q", fw.Index, fw.Agent, fw.Name, fw.ResumeSessionID, fw.Stopped, fw.ExtraArgs)

	var argv []string
	if fw.Agent == AgentTerminal {
		// respawn-pane without a command re-runs the pane's original start
		// command, which is "exit 0" for a stopped tab.
		argv = []string{defaultShell()}
	} else if fw.Agent == AgentCustom {
		argv = customCommandArgv(fw.CustomCommand)
	} else {
		config, ok := AgentConfigs[fw.Agent]
		if !ok || config.Command == "" {
			return fmt.Errorf("cannot restart window %d: unsupported agent %q", windowIdx, fw.Agent)
		}
		args := []string{}
		autoYes := fw.AutoYes || i.AutoYes
		// Use provided resume ID, or saved one from the tab
		tabResumeID := resumeID
		if tabResumeID == "" {
			tabResumeID = fw.ResumeSessionID
		}
		if fw.Agent == AgentClaude && tabResumeID != "" {
			ReleaseClaudeBackgroundAgent(tabResumeID)
		}

		// Handle resume subcommands (codex resume, q chat --resume) vs flags (claude --resume)
		if config.SupportsResume && config.ResumeIsSubcommand {
			if tabResumeID != "" {
				args = append(args, config.ResumeFlag)
				if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
				args = append(args, tabResumeID)
				fw.ResumeSessionID = tabResumeID
			} else {
				if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
			}
		} else {
			if autoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
				args = append(args, config.AutoYesFlag)
			}
			if tabResumeID != "" && config.SupportsResume && config.ResumeFlag != "" {
				args = append(args, config.ResumeFlag, tabResumeID)
				fw.ResumeSessionID = tabResumeID
			} else if tabResumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" {
				newID := uuid.New().String()
				args = append(args, config.SessionIDFlag, newID)
				fw.ResumeSessionID = newID
				log.Printf("[RestartWindow] generated new session-id=%s for tab %s/%d", newID, i.ID, fw.Index)
			}
		}
		argv = buildAgentArgv(config.Command, args, fw.ExtraArgs)
	}

	// Ensure we always have an explicit command — respawn-pane without one
	// re-runs the pane's original start command ("exit 0" for stopped tabs)
	if len(argv) == 0 {
		argv = []string{defaultShell()}
	}
	log.Printf("[RestartWindow] followed win final argv: tmux respawn-pane -k -t %s -- %v", target, argv)
	tmuxArgs := []string{"respawn-pane", "-k"}
	// A terminal tab restarts where it was left; an agent tab keeps whatever
	// directory it was configured with, since that is part of what identifies
	// the conversation it resumes.
	if fw.Agent == AgentTerminal {
		tmuxArgs = append(tmuxArgs, restartDirArgs(fw.WorkDir)...)
	}
	tmuxArgs = append(tmuxArgs, "-t", target)
	tmuxArgs = append(tmuxArgs, argv...)
	if err := TmuxCommand(tmuxArgs...).Run(); err != nil {
		return fmt.Errorf("failed to restart window %d: %w", windowIdx, err)
	}

	fw.Stopped = false
	if collapseDuplicates {
		selected := *fw
		compacted := make([]FollowedWindow, 0, len(i.FollowedWindows))
		inserted := false
		for _, window := range i.FollowedWindows {
			if window.Index != windowIdx {
				compacted = append(compacted, window)
				continue
			}
			if !inserted {
				compacted = append(compacted, selected)
				inserted = true
			}
		}
		i.FollowedWindows = compacted
		log.Printf("[RestartWindow] repaired duplicate metadata for session=%s window=%d agent=%s", i.ID, windowIdx, selected.Agent)
	}
	i.CaptureCodexResumeIDs()
	return nil
}

func (i *Instance) RestartWindow(windowIdx int) error {
	return i.RestartWindowWithResume(windowIdx, "")
}

func selectFollowedWindowForRestart(windows []FollowedWindow, windowIdx int) (sliceIdx int, collapseDuplicates bool, err error) {
	var matches []int
	for idx := range windows {
		if windows[idx].Index == windowIdx {
			matches = append(matches, idx)
		}
	}
	if len(matches) == 0 {
		return -1, false, nil
	}
	if len(matches) == 1 {
		return matches[0], false, nil
	}

	// Older versions could store the active Terminal tab's index for a newly
	// created agent tab. If every non-terminal duplicate agrees on one agent
	// type, that descriptor is the only plausible restart command.
	var preferredAgent AgentType
	for _, idx := range matches {
		agent := windows[idx].Agent
		if agent == AgentTerminal {
			continue
		}
		if preferredAgent == "" {
			preferredAgent = agent
			continue
		}
		if agent != preferredAgent {
			return -1, false, fmt.Errorf(
				"window %d has conflicting duplicate agent metadata (%s and %s)",
				windowIdx,
				preferredAgent,
				agent,
			)
		}
	}
	if preferredAgent != "" {
		for _, idx := range matches {
			if windows[idx].Agent == preferredAgent {
				return idx, true, nil
			}
		}
	}
	return matches[0], true, nil
}

// DeleteWindow removes a followed window. If the session is running and the
// window is not already stopped, it kills the tmux window first.
func (i *Instance) DeleteWindow(windowIdx int) error {
	mainWindowIdx := 0
	if i.Status == StatusRunning {
		var ok bool
		mainWindowIdx, ok = i.getMainWindowIndex()
		if !ok {
			return fmt.Errorf("cannot identify main tmux window for session %s", i.TmuxSessionName())
		}
	}
	if windowIdx == mainWindowIdx {
		return fmt.Errorf("cannot delete main agent window")
	}

	// Capture before kill-window removes the process that owns the rollout FD.
	i.CaptureCodexResumeIDs()

	// Kill the tmux window if session is running
	if i.Status == StatusRunning {
		sessionName := i.TmuxSessionName()
		target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
		// tmux silently falls back to the current window for a missing numeric
		// target, so check exact membership before kill-window. Otherwise stale
		// metadata could delete the main agent.
		if tmuxWindowExists(sessionName, windowIdx) {
			killErr := TmuxCommand("kill-window", "-t", target).Run()
			if tmuxWindowExists(sessionName, windowIdx) {
				if killErr != nil {
					return fmt.Errorf("failed to delete live tmux window %s: %w", target, killErr)
				}
				return fmt.Errorf("tmux window %s is still alive after deletion", target)
			}
			if killErr != nil {
				log.Printf("[DeleteWindow] tmux reported an error after window %s was removed: %v", target, killErr)
			}
		}
	}

	// A tmux index identifies exactly one real window. Remove every matching
	// descriptor so older duplicate-index corruption cannot leave phantom tabs
	// behind after the real window is deleted.
	filtered := i.FollowedWindows[:0]
	for _, window := range i.FollowedWindows {
		if window.Index != windowIdx {
			filtered = append(filtered, window)
		}
	}
	i.FollowedWindows = filtered

	// Clear TabOrder since window indices changed
	i.TabOrder = nil

	return nil
}

// CloseWindow closes a tmux window by index and removes it from FollowedWindows
func (i *Instance) CloseWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	// Don't allow closing the main agent window.
	mainWindowIdx, ok := i.getMainWindowIndex()
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", i.TmuxSessionName())
	}
	if windowIdx == mainWindowIdx {
		return fmt.Errorf("cannot close main agent window")
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Kill the tmux window
	cmd := TmuxCommand("kill-window", "-t", target)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close window: %w", err)
	}

	// Remove from FollowedWindows
	for idx, fw := range i.FollowedWindows {
		if fw.Index == windowIdx {
			i.FollowedWindows = append(i.FollowedWindows[:idx], i.FollowedWindows[idx+1:]...)
			break
		}
	}

	// Clear TabOrder since window indices changed
	i.TabOrder = nil

	return nil
}

// GetWindowCount returns the number of tmux windows in the session
func (i *Instance) GetWindowCount() int {
	if i.Status != StatusRunning {
		return 0
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("list-windows", "-t", sessionName)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Count lines (each line is a window)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// GetCurrentWindowIndex returns the current (active) window index (0-based)
func (i *Instance) GetCurrentWindowIndex() int {
	if i.Status != StatusRunning {
		return 0
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("display-message", "-t", sessionName, "-p", "#{window_index}")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	var idx int
	fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &idx)
	return idx
}

// GetCurrentWindowName returns the name of the currently active window
func (i *Instance) GetCurrentWindowName() string {
	if i.Status != StatusRunning {
		return ""
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("display-message", "-t", sessionName, "-p", "#{window_name}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// SelectWindow switches to the specified window index
func (i *Instance) SelectWindow(index int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, index))
	return cmd.Run()
}

// NextWindow switches to the next tmux window
func (i *Instance) NextWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("next-window", "-t", sessionName)
	return cmd.Run()
}

// PrevWindow switches to the previous tmux window
func (i *Instance) PrevWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("previous-window", "-t", sessionName)
	return cmd.Run()
}

// RenameCurrentWindow renames the current tmux window
func (i *Instance) RenameCurrentWindow(name string) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("rename-window", "-t", sessionName, name)
	return cmd.Run()
}

// WindowInfo contains information about a tmux window
type WindowInfo struct {
	Index           int
	Name            string
	Active          bool
	Followed        bool      // Whether this window is tracked as an agent
	Agent           AgentType // Agent type if followed
	Dead            bool      // Whether the window's pane has exited (command finished)
	TextColor       string    // Tab text color (empty uses the theme default)
	BackgroundColor string    // Tab background color (empty uses the theme default)
}

// IsWindowFollowed checks if a window index is being tracked as an agent
func (i *Instance) IsWindowFollowed(index int) bool {
	return i.isWindowFollowed(index, i.GetMainWindowIndex())
}

func (i *Instance) isWindowFollowed(index, mainWindowIdx int) bool {
	// The first tmux window is always the main agent. Its index may be non-zero
	// when tmux base-index/renumbering is configured.
	if index == mainWindowIdx {
		return true
	}
	for _, fw := range i.FollowedWindows {
		if fw.Index == index {
			return true
		}
	}
	return false
}

// GetFollowedWindow returns the FollowedWindow for a given index, or nil if not followed
func (i *Instance) GetFollowedWindow(index int) *FollowedWindow {
	return i.getFollowedWindow(index, i.GetMainWindowIndex())
}

func (i *Instance) getFollowedWindow(index, mainWindowIdx int) *FollowedWindow {
	if index == mainWindowIdx {
		return &FollowedWindow{
			Index:           mainWindowIdx,
			Agent:           i.Agent,
			Name:            i.Name,
			AutoYes:         i.AutoYes,
			ResumeSessionID: i.ResumeSessionID,
			Notes:           i.Notes,
			TextColor:       i.TabTextColor,
			BackgroundColor: i.TabBackgroundColor,
		}
	}
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == index {
			return &i.FollowedWindows[idx]
		}
	}
	return nil
}

// ToggleWindowFollow toggles the follow status of a window
func (i *Instance) ToggleWindowFollow(index int) bool {
	// Can't unfollow the main window.
	if index == i.GetMainWindowIndex() {
		return true
	}

	// Check if already followed
	for idx, fw := range i.FollowedWindows {
		if fw.Index == index {
			// Remove from followed
			i.FollowedWindows = append(i.FollowedWindows[:idx], i.FollowedWindows[idx+1:]...)
			return false
		}
	}

	// Add to followed with default agent (same as main)
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		Index: index,
		Agent: i.Agent,
		Name:  "",
	})
	return true
}

// GetTabOrder returns the current tab display order as tmux window indices.
// If no custom order is set, returns the default order: [mainWindowIdx, followedWindows...].
func (i *Instance) GetTabOrder() []int {
	if len(i.TabOrder) > 0 {
		return i.TabOrder
	}
	// Default order: main window first, then followed windows in order
	mainIdx := i.GetMainWindowIndex()
	order := []int{mainIdx}
	for _, fw := range i.FollowedWindows {
		order = append(order, fw.Index)
	}
	return order
}

// SetTabColors stores presentation colors for one tracked tab. An empty color
// clears the override. Text color additionally accepts "auto" so the frontend
// can choose a contrasting color for the configured background.
func (i *Instance) SetTabColors(windowIdx int, textColor, backgroundColor string) error {
	return i.setTabColors(windowIdx, i.GetMainWindowIndex(), textColor, backgroundColor)
}

func (i *Instance) setTabColors(windowIdx, mainWindowIdx int, textColor, backgroundColor string) error {
	if !validTabColor(textColor, true) {
		return fmt.Errorf("invalid tab text color")
	}
	if !validTabColor(backgroundColor, false) {
		return fmt.Errorf("invalid tab background color")
	}

	if windowIdx == mainWindowIdx {
		i.TabTextColor = textColor
		i.TabBackgroundColor = backgroundColor
		return nil
	}

	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			i.FollowedWindows[idx].TextColor = textColor
			i.FollowedWindows[idx].BackgroundColor = backgroundColor
			return nil
		}
	}

	return fmt.Errorf("error.windowNotFound")
}

func validTabColor(color string, allowAuto bool) bool {
	return color == "" || (allowAuto && color == "auto") || cssHexColorRegex.MatchString(color)
}

// ReorderTabs moves a tab from one display position to another.
// fromPos and toPos are indices into the tab display order (0-based, including main window).
func (i *Instance) ReorderTabs(fromPos, toPos int) error {
	order := i.GetTabOrder()
	if fromPos < 0 || fromPos >= len(order) {
		return fmt.Errorf("invalid from position")
	}
	if toPos < 0 || toPos >= len(order) {
		return fmt.Errorf("invalid to position")
	}
	if fromPos == toPos {
		return nil
	}
	// Move element
	item := order[fromPos]
	order = append(order[:fromPos], order[fromPos+1:]...)
	order = append(order[:toPos], append([]int{item}, order[toPos:]...)...)
	i.TabOrder = order
	return nil
}

// GetAllFollowedAgents returns info about all followed agents (including main window 0)
func (i *Instance) GetAllFollowedAgents() []FollowedWindow {
	result := []FollowedWindow{
		{
			Index:           0,
			Agent:           i.Agent,
			Name:            i.Name,
			TextColor:       i.TabTextColor,
			BackgroundColor: i.TabBackgroundColor,
		},
	}
	result = append(result, i.FollowedWindows...)
	return result
}

// GetWindowList returns information about all windows in the session
func (i *Instance) GetWindowList() []WindowInfo {
	if i.Status != StatusRunning {
		return nil
	}

	sessionName := i.TmuxSessionName()
	// Format: index:name:active_flag:pane_dead
	cmd := TmuxCommand("list-windows", "-t", sessionName, "-F", "#{window_index}:#{window_name}:#{window_active}:#{pane_dead}")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var windows []WindowInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	mainWindowIdx := 0
	if len(lines) > 0 {
		fmt.Sscanf(strings.SplitN(lines[0], ":", 2)[0], "%d", &mainWindowIdx)
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 4 {
			var idx int
			fmt.Sscanf(parts[0], "%d", &idx)

			// Get agent type if followed
			var agent AgentType
			var textColor, backgroundColor string
			followed := i.isWindowFollowed(idx, mainWindowIdx)
			if followed {
				if fw := i.getFollowedWindow(idx, mainWindowIdx); fw != nil {
					agent = fw.Agent
					textColor = fw.TextColor
					backgroundColor = fw.BackgroundColor
				}
			}

			windows = append(windows, WindowInfo{
				Index:           idx,
				Name:            parts[1],
				Active:          parts[2] == "1",
				Followed:        followed,
				Agent:           agent,
				Dead:            parts[3] == "1",
				TextColor:       textColor,
				BackgroundColor: backgroundColor,
			})
		}
	}
	return windows
}

// NewAgentWindow creates a new tmux window running the specified agent
func (i *Instance) NewAgentWindow(name string, agent AgentType, customCmd string, extraArgs string, workDir string) (int, error) {
	if workDir == "" {
		workDir = i.Path
	}
	if i.Status != StatusRunning {
		return -1, fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()

	// Build agent command based on agent type (argv form, no shell)
	config := AgentConfigs[agent]
	var argv []string
	var generatedSessionID string

	if agent == AgentCustom {
		argv = customCommandArgv(customCmd)
	} else {
		if config.Command == "" {
			return -1, fmt.Errorf("unsupported agent %q", agent)
		}
		args := []string{}
		// Use instance's AutoYes setting for the new agent too
		if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
			args = append(args, config.AutoYesFlag)
		}
		// For agents supporting --session-id, pre-assign a session ID
		if config.SupportsSessionID && config.SessionIDFlag != "" {
			generatedSessionID = uuid.New().String()
			args = append(args, config.SessionIDFlag, generatedSessionID)
		}
		argv = buildAgentArgv(config.Command, args, extraArgs)
	}

	// Create new window with the agent command as separate argv elements
	// (tmux execs directly, no `sh -c`).
	output, err := newTmuxWindowCommand(sessionName, workDir, name, false, argv).Output()
	if err != nil {
		return -1, err
	}

	newIdx, err := parseTmuxWindowIndex(output)
	if err != nil {
		return -1, fmt.Errorf("invalid new agent window index: %w", err)
	}

	// Add to followed windows with agent info
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		WorkDir: func() string {
			if workDir != i.Path {
				return workDir
			}
			return ""
		}(),
		Index:           newIdx,
		Agent:           agent,
		Name:            name,
		CustomCommand:   customCmd,
		ExtraArgs:       extraArgs,
		ResumeSessionID: generatedSessionID,
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits (shows as stopped)
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	TmuxCommand("set-option", "-t", target, "remain-on-exit", "on").Run()
	// Disable automatic-rename so the window keeps the user-specified name
	TmuxCommand("set-option", "-t", target, "automatic-rename", "off").Run()

	i.CaptureCodexResumeIDs()

	return newIdx, nil
}

/**
 * ForkSession names the conversation a fork should branch from.
 *
 * It runs nothing. The branch is made by the agent when the forked tab or
 * session starts, with the fork flag alongside the resume — see
 * appendForkArgs.
 *
 * This used to run `claude --resume <id> --fork-session -p "."` and read the
 * new id back out of its JSON. That replayed the entire conversation before
 * answering, which on a long one is minutes of an apparently frozen dialog,
 * and the `-p` spent a real turn to get there — a comment recorded that an
 * absent or empty prompt was refused, so it looked unavoidable. It is not: the
 * agent is going to be started with --resume anyway, and adding the fork flag
 * to THAT start branches the conversation for free. The conversation was
 * otherwise being loaded twice, once to fork and once to run.
 *
 * windowIdx names the tab to branch. Reading the main window's conversation
 * instead, as this once did, silently branched something else entirely — under
 * a name the user had chosen for the tab in front of them.
 */
func (i *Instance) ForkSession(windowIdx int) (string, error) {
	agent, sessionID := i.conversationInWindow(windowIdx)
	config, ok := AgentConfigs[agent]
	if !ok || config.ForkFlag == "" {
		return "", fmt.Errorf("%s cannot fork a conversation", agent)
	}
	if sessionID == "" {
		return "", fmt.Errorf("no session ID to fork - session may not have started yet")
	}
	log.Printf("[Fork] session=%s window=%d agent=%s branching from %s",
		i.ID, windowIdx, agent, sessionID)
	return sessionID, nil
}

/**
 * The arguments that turn a resume into a fork.
 *
 * Two shapes, because the agents differ: Claude takes a flag beside its resume
 * (`--resume <id> --fork-session`), Codex a subcommand of its own
 * (`codex fork <id>`). Both start interactively on the branch, and neither
 * needs a prompt — which is what makes forking instant.
 *
 * The new conversation's id comes from the agent. Claude accepts one we choose
 * (--session-id) and Codex assigns its own, so the caller stores what it can
 * and the poll picks up the rest.
 */
func appendForkArgs(config AgentConfig, args []string, sourceID string) []string {
	if config.ForkIsSubcommand {
		return append(args, config.ForkFlag, sourceID)
	}
	return append(args, config.ResumeFlag, sourceID, config.ForkFlag)
}

// conversationInWindow reports which agent a window runs and which conversation
// it is on. A tab can run a different agent from the session's main window, and
// carries its own conversation id.
func (i *Instance) conversationInWindow(windowIdx int) (AgentType, string) {
	if windowIdx == i.GetMainWindowIndex() {
		return i.Agent, i.ResumeSessionID
	}
	for _, fw := range i.FollowedWindows {
		if fw.Index == windowIdx {
			agent := fw.Agent
			if agent == "" {
				agent = i.Agent
			}
			return agent, fw.ResumeSessionID
		}
	}
	// No such window: answer for the session itself rather than inventing one.
	return i.Agent, i.ResumeSessionID
}

// NewForkedTab creates a new tab with a forked Claude session
// NewForkedTab creates the tab and reports which window index it landed on, so
// the caller can switch to it — a branch you have to go and find is a branch
// you half-made.
func (i *Instance) NewForkedTab(name string, sessionID string) (int, error) {
	if i.Status != StatusRunning {
		return 0, fmt.Errorf("instance not running")
	}

	// The same two guards every other Claude resume applies.
	//
	// A conversation held by a background agent (Ctrl+B / --bg) makes
	// `claude --resume` refuse to start, so a fork of one produced a tab that
	// died on launch. And an id that is not a safe shape has no business
	// reaching a command line, however it got here.
	if !IsSafeResumeID(sessionID) {
		return 0, fmt.Errorf("forked session id has an unexpected shape: %q", sessionID)
	}
	ReleaseClaudeBackgroundAgent(sessionID)

	sessionName := i.TmuxSessionName()

	config := AgentConfigs[i.Agent]
	if config.ForkFlag == "" {
		return 0, fmt.Errorf("%s cannot fork a conversation", i.Agent)
	}
	args := []string{}

	// Add auto-yes flag if the main session has it enabled
	if i.AutoYes && config.AutoYesFlag != "" {
		args = append(args, config.AutoYesFlag)
	}

	// The branch is made HERE, by the agent, as it starts: the resume carries
	// the fork flag rather than a separate run having produced a new id first.
	// That earlier run replayed the whole conversation and spent a turn to do
	// it, and this start would then have loaded the same conversation again.
	args = appendForkArgs(config, args, sessionID)

	// Claude lets us name the new conversation, which is worth doing: without
	// it the branch's id is only discoverable by watching the agent afterwards,
	// and until then the tab has nothing to resume from. Codex assigns its own,
	// and CaptureCodexResumeIDs picks it up.
	forkedID := ""
	if config.SupportsSessionID && config.SessionIDFlag != "" {
		forkedID = uuid.New().String()
		args = append(args, config.SessionIDFlag, forkedID)
	}

	// Carry the session's extra arguments, as every other way of starting a
	// Claude tab does. A fork is the same conversation with the same setup, so
	// dropping them here gave the branch a differently-configured agent —
	// ForkToNewSession passes them, and this did not.
	argv := buildAgentArgv(config.Command, args, i.ExtraArgs)

	// Create new window with forked agent (argv form, no shell layer).
	output, err := newTmuxWindowCommand(sessionName, i.Path, name, false, argv).Output()
	if err != nil {
		return 0, err
	}

	newIdx, err := parseTmuxWindowIndex(output)
	if err != nil {
		return 0, fmt.Errorf("invalid forked window index: %w", err)
	}

	// The tab remembers the BRANCH, not what it was branched from. Storing the
	// source would send a restart back to the original conversation — the fork
	// would exist only until the tab was next resumed.
	//
	// Empty where the agent names its own branch (Codex); CaptureCodexResumeIDs
	// fills it in once the agent has settled.
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		Index:           newIdx,
		Agent:           i.Agent,
		Name:            name,
		ResumeSessionID: forkedID,
		Notes:           "Forked session",
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	TmuxCommand("set-option", "-t", target, "remain-on-exit", "on").Run()
	TmuxCommand("set-option", "-t", target, "automatic-rename", "off").Run()

	// Codex names its own branch, so the id has to be read back off the running
	// process — as every other way of starting a Codex tab does. Without it the
	// forked tab has nothing to resume from.
	i.CaptureCodexResumeIDs()

	return newIdx, nil
}

func (i *Instance) IsAlive() bool {
	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("has-session", "-t", sessionName)
	return cmd.Run() == nil
}

// ResizePane resizes the tmux pane to the specified dimensions
func (i *Instance) ResizePane(width, height int) error {
	if !i.IsAlive() {
		return nil
	}
	sessionName := i.TmuxSessionName()
	return TmuxCommand("resize-window", "-t", sessionName, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height)).Run()
}

// UpdateDetachBinding updates Ctrl+Q to resize to preview size before detaching
func (i *Instance) UpdateDetachBinding(previewWidth, previewHeight int) {
	if !i.IsAlive() {
		return
	}
	// Bind Ctrl+Q: conditional - only in asmgr-* sessions, with resize before detach
	// Use if-shell for the condition check, then run-shell for the actual commands
	resizeAndDetach := fmt.Sprintf("run-shell 'tmux resize-window -x %d -y %d 2>/dev/null; tmux detach-client'", previewWidth, previewHeight)
	TmuxCommand("bind-key", "-n", "C-q", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", resizeAndDetach, "").Run()
}

func (i *Instance) GetPreview(lines int) (string, error) {
	if !i.IsAlive() {
		return "(session not running)", nil
	}

	sessionName := i.TmuxSessionName()
	// Capture from the currently active window (follows tab switching)
	// Capture pane with scrollback history (-S for start line, -E for end)
	// -S -lines means start from 'lines' back in history
	// -e preserves colors, -J joins wrapped lines
	startLine := fmt.Sprintf("-%d", lines)
	cmd := TmuxCommand("capture-pane", "-t", sessionName, "-p", "-e", "-J", "-S", startLine)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture pane: %w", err)
	}

	// Post-process to remove extra spaces after wide characters (emojis)
	// This is needed because tmux -J flag adds padding after wide chars
	result := removeWideCharPadding(string(output))
	return strings.TrimRight(result, "\n"), nil
}

// removeWideCharPadding removes extra spaces after wide characters (emojis)
// that tmux -J flag adds when capturing panes
func removeWideCharPadding(s string) string {
	runes := []rune(s)
	var result []rune
	i := 0

	for i < len(runes) {
		// Check for ANSI escape sequence - preserve them
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			start := i
			i += 2
			// Find end of ANSI sequence
			for i < len(runes) && !((runes[i] >= 'A' && runes[i] <= 'Z') || (runes[i] >= 'a' && runes[i] <= 'z')) {
				i++
			}
			if i < len(runes) {
				i++ // include final letter
			}
			// Copy ANSI sequence
			result = append(result, runes[start:i]...)
			continue
		}

		// Normal character
		currentRune := runes[i]
		result = append(result, currentRune)
		i++

		// If this is a wide character (width 2) and next char is space, skip the space
		if i < len(runes) && runes[i] == ' ' {
			// Check if previous character was wide using runewidth
			if runewidth.RuneWidth(currentRune) == 2 {
				i++ // Skip the space after wide character
			}
		}
	}

	return string(result)
}

// GetLastLine returns the last non-empty line of output (for status display)
func (i *Instance) GetLastLine() string {
	if !i.IsAlive() {
		return "stopped"
	}

	target := i.GetCaptureTarget(0)
	// Capture last 50 lines with colors (-e flag preserves ANSI escape sequences)
	// -J flag joins wrapped lines (prevents terminal width wrapping issues)
	cmd := TmuxCommand("capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return "..."
	}

	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")

	agentName := string(i.Agent)
	if agentName == "" {
		agentName = "claude"
	}

	// Claude Code special handling: detect input area between horizontal lines
	if agentName == "claude" {
		result := GetClaudeStatusLine(lines, StripANSI)
		if result != "" {
			return result
		}
	}

	// Find last meaningful line (for other agents or fallback)
	agentFilters := filters.LoadFilters()
	var lastNonEmpty string // fallback: last non-empty line (e.g., status bar)
	for j := len(lines) - 1; j >= 0; j-- {
		line := lines[j]
		// Strip ANSI codes for checking
		cleanLine := strings.TrimSpace(StripANSI(line))
		// Skip empty lines
		if cleanLine == "" {
			continue
		}

		// Remember the first (from bottom) non-empty line as fallback
		if lastNonEmpty == "" {
			lastNonEmpty = cleanLine
		}

		if config, ok := agentFilters[agentName]; ok {
			skip, content := filters.ApplyFilter(config, cleanLine)
			if skip {
				continue
			}
			if content != "" {
				return content
			}
		}

		// Found actual content - return with colors
		return line
	}

	// All lines were filtered out - use last non-empty line (status bar) as fallback
	if lastNonEmpty != "" {
		return lastNonEmpty
	}

	return "..."
}

// StatusInfo holds both the status line and spinner text from a single tmux capture.
type StatusInfo struct {
	StatusLine  string
	SpinnerText string
}

// GetStatusInfo captures the tmux pane once and extracts both statusLine and spinnerText.
// Uses the main window (index 0) for backward compatibility.
func (i *Instance) GetStatusInfo() StatusInfo {
	agent := i.Agent
	if agent == "" {
		agent = AgentClaude
	}
	return i.GetStatusInfoForWindow(i.GetMainWindowIndex(), agent)
}

// GetStatusInfoForWindow captures a specific tmux window and extracts both statusLine and spinnerText.
func (i *Instance) GetStatusInfoForWindow(windowIdx int, agent AgentType) StatusInfo {
	result := StatusInfo{}
	if !i.IsAlive() {
		result.StatusLine = "stopped"
		return result
	}

	// A plain shell has no agent status to report — its last pane line is
	// just a prompt or whatever command the user last ran, which is noise in
	// the session list. Activity detection already skips terminals; do the
	// same for the status line.
	if agent == AgentTerminal {
		return result
	}

	target := i.GetCaptureTarget(windowIdx)
	cmd := TmuxCommand("capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		result.StatusLine = "..."
		return result
	}

	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	agentName := string(agent)
	if agentName == "" {
		agentName = "claude"
	}

	// Extract spinner text
	result.SpinnerText = ExtractSpinnerText(lines, agentName, StripANSI)

	// Extract status line
	if agentName == "claude" {
		r := GetClaudeStatusLine(lines, StripANSI)
		if r != "" {
			result.StatusLine = r
			return result
		}
	}

	// Find last meaningful line (for other agents or fallback)
	agentFilters := filters.LoadFilters()
	var lastNonEmpty string // fallback: last non-empty line (e.g., status bar)
	for j := len(lines) - 1; j >= 0; j-- {
		line := lines[j]
		cleanLine := strings.TrimSpace(StripANSI(line))
		if cleanLine == "" {
			continue
		}
		// Remember the first (from bottom) non-empty line as fallback
		if lastNonEmpty == "" {
			lastNonEmpty = cleanLine
		}
		if config, ok := agentFilters[agentName]; ok {
			skip, content := filters.ApplyFilter(config, cleanLine)
			if skip {
				continue
			}
			if content != "" {
				result.StatusLine = content
				return result
			}
		}
		result.StatusLine = line
		return result
	}

	// All lines were filtered out - use last non-empty line (status bar) as fallback
	if lastNonEmpty != "" {
		result.StatusLine = lastNonEmpty
		return result
	}

	result.StatusLine = "..."
	return result
}

// GetLastLineForWindow returns the last meaningful line from a specific window
func (i *Instance) GetLastLineForWindow(windowIdx int, agent AgentType) string {
	if !i.IsAlive() {
		return "stopped"
	}

	target := i.GetCaptureTarget(windowIdx)
	cmd := TmuxCommand("capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return "..."
	}

	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")

	agentName := string(agent)
	if agentName == "" {
		agentName = "claude"
	}

	// Claude Code special handling
	if agentName == "claude" {
		result := GetClaudeStatusLine(lines, StripANSI)
		if result != "" {
			return result
		}
	}

	// Find last meaningful line
	agentFilters := filters.LoadFilters()
	var lastNonEmpty string // fallback: last non-empty line (e.g., status bar)
	for j := len(lines) - 1; j >= 0; j-- {
		line := lines[j]
		cleanLine := strings.TrimSpace(StripANSI(line))
		if cleanLine == "" {
			continue
		}

		// Remember the first (from bottom) non-empty line as fallback
		if lastNonEmpty == "" {
			lastNonEmpty = cleanLine
		}

		if config, ok := agentFilters[agentName]; ok {
			skip, content := filters.ApplyFilter(config, cleanLine)
			if skip {
				continue
			}
			if content != "" {
				return content
			}
		}

		return line
	}

	// All lines were filtered out - use last non-empty line (status bar) as fallback
	if lastNonEmpty != "" {
		return lastNonEmpty
	}

	return "..."
}

func (i *Instance) SendKeys(keys string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("send-keys", "-t", sessionName, keys)
	return cmd.Run()
}

// SendKeysToWindow sends a tmux key name to a specific window of this session.
func (i *Instance) SendKeysToWindow(windowIdx int, keys string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	return TmuxCommand("send-keys", "-t", target, keys).Run()
}

// SendText sends text literally (not interpreted as key names)
func (i *Instance) SendText(text string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()
	// Use -l flag to send text literally without interpreting key names
	cmd := TmuxCommand("send-keys", "-l", "-t", sessionName, text)
	return cmd.Run()
}

// SendTextToWindow types text into a specific window, optionally pressing
// Enter afterwards. Sent with -l so the text is taken literally: a saved
// command containing "C-c" or "Enter" is text, not a key name.
func (i *Instance) SendTextToWindow(windowIdx int, text string, pressEnter bool) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	if err := TmuxCommand("send-keys", "-l", "-t", target, text).Run(); err != nil {
		return fmt.Errorf("could not send the command: %w", err)
	}
	if !pressEnter {
		return nil
	}
	// Separate call: Enter is a key name, so it must not carry -l.
	return TmuxCommand("send-keys", "-t", target, "Enter").Run()
}

// SendPrompt sends a prompt text followed by Enter key
func (i *Instance) SendPrompt(text string) error {
	return i.SendPromptToWindow(text, -1)
}

// SendPromptToWindow sends text to one window of the session. A negative index
// means the session's active window, which is what SendPrompt has always used.
//
// Naming the window matters once a session has more than one: the target is
// otherwise just the session, and the multiplexer resolves that to whichever
// window is active — so dictated text landed in a different tab than the one
// being looked at, which reads as the text never being sent at all.
func (i *Instance) SendPromptToWindow(text string, windowIdx int) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()
	if windowIdx >= 0 {
		sessionName = fmt.Sprintf("%s:%d", sessionName, windowIdx)
	}

	if strings.Contains(text, "\n") {
		// Multi-line text: use tmux's paste buffer with bracketed paste mode.
		// Without this, each newline would be interpreted as Enter by the terminal,
		// causing the prompt to be submitted line-by-line instead of as one block.
		if err := TmuxCommand("set-buffer", "--", text).Run(); err != nil {
			return fmt.Errorf("failed to set tmux buffer: %w", err)
		}
		if err := TmuxCommand("paste-buffer", "-p", "-t", sessionName).Run(); err != nil {
			// Fallback: paste without -p if not supported
			if err2 := TmuxCommand("paste-buffer", "-t", sessionName).Run(); err2 != nil {
				return fmt.Errorf("failed to paste buffer: %w", err2)
			}
		}
	} else {
		// Single-line text: use send-keys -l for simplicity
		cmd := TmuxCommand("send-keys", "-l", "-t", sessionName, text)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	// Wait for the text to be fully processed by the terminal/agent
	time.Sleep(100 * time.Millisecond)

	// No Escape here. It was sent to dismiss an autocomplete popup before
	// submitting, on the assumption that it "closes suggestions without
	// affecting the pasted text" — but Escape is input, and what it does is
	// decided by whatever is reading the pane, not by us. Claude Code takes it
	// as "clear the composer", so the text just pasted was discarded and Enter
	// submitted nothing. The same mistake as the redraw keystroke that used to
	// put a stray "/clear" into that composer.
	//
	// A suggestion popup left open is harmless: Enter submits the line either
	// way, and a wrong guess about what a keystroke means is not.
	cmd := TmuxCommand("send-keys", "-t", sessionName, "Enter")
	return cmd.Run()
}

// IsMainWindowDead checks if the main window (0) pane is dead in tmux
func (i *Instance) IsMainWindowDead() bool {
	if !i.IsAlive() {
		return false
	}
	target := fmt.Sprintf("%s:0", i.TmuxSessionName())
	cmd := TmuxCommand("list-panes", "-t", target, "-F", "#{pane_dead}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

func (i *Instance) UpdateStatus() {
	if i.IsAlive() {
		i.Status = StatusRunning
	} else {
		i.Status = StatusStopped
	}
}

// Git diff functions

// GetSessionDiff returns diff since session start (BaseCommitSHA)
func (i *Instance) GetSessionDiff() *DiffStats {
	if i.BaseCommitSHA == "" {
		return &DiffStats{Error: fmt.Errorf("no base commit (not a git repo or session started before tracking)")}
	}
	return i.getDiff(i.BaseCommitSHA)
}

// GetFullDiff returns all uncommitted changes (staged + unstaged)
func (i *Instance) GetFullDiff() *DiffStats {
	return i.getDiff("")
}

// getDiff executes git diff and parses the result
// diffIndexEnv prepares a private git index so a diff can include untracked
// files without touching the user's staging area.
//
// Returns the environment to run git with and a cleanup to call when done. Both
// the whole-tree diff and the per-file one need this, so it lives here rather
// than being repeated: the intent-to-add is what makes new files appear at all.
func (i *Instance) diffIndexEnv() ([]string, func(), error) {
	if !i.isGitRepo() {
		return nil, nil, fmt.Errorf("not a git repository")
	}

	tmpIndex, err := os.CreateTemp("", "asmgr-git-index-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temporary git index: %w", err)
	}
	tmpIndexPath := tmpIndex.Name()
	tmpIndex.Close()
	os.Remove(tmpIndexPath) // Git expects a missing or valid index, not an empty file.
	cleanup := func() { os.Remove(tmpIndexPath) }

	gitEnv := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndexPath)
	readTree, cancelReadTree := GitCommandTimed("-C", i.Path, "read-tree", "HEAD")
	defer cancelReadTree()
	readTree.Env = gitEnv
	if err := readTree.Run(); err != nil {
		// An unborn repository has no HEAD yet; start from an empty index.
		readEmpty, cancelReadEmpty := GitCommandTimed("-C", i.Path, "read-tree", "--empty")
		defer cancelReadEmpty()
		readEmpty.Env = gitEnv
		if emptyErr := readEmpty.Run(); emptyErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to prepare temporary git index: %w", err)
		}
	}

	intentToAdd, cancelIntent := GitCommandTimed("-C", i.Path, "add", "-N", ".")
	defer cancelIntent()
	intentToAdd.Env = gitEnv
	if err := intentToAdd.Run(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to include untracked files in diff: %w", err)
	}
	return gitEnv, cleanup, nil
}

func (i *Instance) getDiff(baseRef string) *DiffStats {
	stats := &DiffStats{}

	gitEnv, cleanup, err := i.diffIndexEnv()
	if err != nil {
		stats.Error = err
		return stats
	}
	defer cleanup()

	args := []string{"-C", i.Path, "--no-pager", "diff"}
	if baseRef != "" {
		args = append(args, baseRef)
	}

	cmd, cancelDiff := GitCommandTimed(args...)
	defer cancelDiff()
	cmd.Env = gitEnv
	output, err := cmd.Output()
	if err != nil {
		stats.Error = fmt.Errorf("git diff failed: %w", err)
		return stats
	}

	stats.Content = string(output)
	stats.Added, stats.Removed = i.countDiffLines(stats.Content)

	return stats
}

// isGitRepo checks if the instance path is a git repository
func (i *Instance) isGitRepo() bool {
	cmd, cancel := GitCommandTimed("-C", i.Path, "rev-parse", "--git-dir")
	defer cancel()
	return cmd.Run() == nil
}

// countDiffLines counts added and removed lines in diff content
func (i *Instance) countDiffLines(content string) (added, removed int) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		switch {
		case line[0] == '+' && !strings.HasPrefix(line, "+++"):
			added++
		case line[0] == '-' && !strings.HasPrefix(line, "---"):
			removed++
		}
	}
	return
}

// ResetBaseCommit clears the base commit SHA (useful for "reset diff" feature)
func (i *Instance) ResetBaseCommit() {
	i.BaseCommitSHA = ""
	i.saveBaseCommit()
}

// GetMainWindowIndex returns the main agent window's current tmux index.
// A marker stored on the tmux window survives move-window and renumbering.
func (i *Instance) GetMainWindowIndex() int {
	index, ok := i.getMainWindowIndex()
	if !ok {
		return 0
	}
	return index
}

func (i *Instance) getMainWindowIndex() (int, bool) {
	if i.Status != StatusRunning {
		return 0, false
	}

	sessionName := i.TmuxSessionName()
	cmd := TmuxCommand("list-windows", "-t", sessionName, "-F", "#{window_index}\t#{@asmgr_main}")
	output, err := cmd.Output()
	if err != nil {
		return 0, false
	}

	index, ok := identifyMainWindowIndex(output, i.FollowedWindows)
	if !ok {
		return 0, false
	}
	// Backfill the marker for sessions created by older asmgr versions.
	//
	// Skipped where window options are not actually per-window. psmux stores a
	// -w user option globally: setting @asmgr_probe on window 1 alone made
	// windows 0, 1 and 2 all report its value. Writing the marker there tags
	// EVERY window as the main one, and identifyMainWindowIndex then refuses to
	// choose — correctly, since killing the wrong window takes the agent with
	// it, but the result is that deleting a tab stops working entirely.
	//
	// Nothing is lost by not writing it: identification falls back to "the one
	// window that is not a followed tab", which needs no marker.
	if PerWindowOptionsSupported() && !bytes.Contains(output, []byte("\t1")) {
		target := fmt.Sprintf("%s:%d", sessionName, index)
		_ = TmuxCommand("set-option", "-w", "-t", target, "@asmgr_main", "1").Run()
	}
	return index, true
}

func identifyMainWindowIndex(output []byte, followedWindows []FollowedWindow) (int, bool) {
	var live []int
	var marked []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		var index int
		if _, err := fmt.Sscanf(parts[0], "%d", &index); err != nil {
			continue
		}
		live = append(live, index)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == "1" {
			marked = append(marked, index)
		}
	}
	if len(marked) == 1 {
		return marked[0], true
	}
	// Every window marked means the marker carries no information: psmux stores
	// a -w user option globally, so one write tags the whole session, and
	// sessions created before that was understood are stuck that way — the value
	// cannot be unset or overwritten back. Treat it as absent and fall through
	// to identifying the window by what it is.
	//
	// A PARTIAL set of marks is different: that is a session where marking did
	// work and then went wrong, and guessing between them could kill the agent's
	// own window. That still fails closed.
	if len(marked) > 1 && len(marked) != len(live) {
		return 0, false
	}

	followed := make(map[int]struct{}, len(followedWindows))
	for _, window := range followedWindows {
		followed[window.Index] = struct{}{}
	}
	var candidates []int
	for _, index := range live {
		if _, isFollowed := followed[index]; !isFollowed {
			candidates = append(candidates, index)
		}
	}
	if len(candidates) != 1 {
		return 0, false
	}
	return candidates[0], true
}

func soleTmuxWindowIndex(sessionName string) (int, bool) {
	output, err := TmuxCommand("list-windows", "-t", sessionName, "-F", "#{window_index}").Output()
	if err != nil {
		return 0, false
	}
	var indices []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		var index int
		if _, err := fmt.Sscanf(line, "%d", &index); err == nil {
			indices = append(indices, index)
		}
	}
	if len(indices) != 1 {
		return 0, false
	}
	return indices[0], true
}

// Session starts are recorded so a start that overlaps another can tell the
// difference between "this session does not exist" and "it was created a
// moment ago and the multiplexer has not caught up".
//
// Without this the second start creates a duplicate server under the same
// name, which does not resolve itself: one server holds the terminal client
// and the other does not, so the session appears frozen until one is killed.
var recentStarts sync.Map // map[string]time.Time

// startSettleWindow is how long after a start another start should wait for
// the session to appear instead of assuming it is absent. Comfortably longer
// than the registration delay measured on psmux, and it only ever delays the
// rarer case of restarting a session that really did go away.
const startSettleWindow = 10 * time.Second

func markStarted(sessionName string) {
	recentStarts.Store(sessionName, time.Now())
}

func recentlyStarted(sessionName string) bool {
	v, ok := recentStarts.Load(sessionName)
	if !ok {
		return false
	}
	started, ok := v.(time.Time)
	if !ok {
		return false
	}
	if time.Since(started) > startSettleWindow {
		recentStarts.Delete(sessionName) // keep the map from growing forever
		return false
	}
	return true
}
