package session

import (
	"encoding/json"
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
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Path               string           `json:"path"`
	Status             Status           `json:"status"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	AutoYes            bool             `json:"auto_yes"`
	HideStatusLine     bool             `json:"hide_status_line,omitempty"`     // Don't show the main window's status line in the session list
	ResumeSessionID    string           `json:"resume_session_id,omitempty"`    // Claude session ID to resume
	Color              string           `json:"color,omitempty"`                // Foreground color
	BgColor            string           `json:"bg_color,omitempty"`             // Background color
	FullRowColor       bool             `json:"full_row_color,omitempty"`       // Extend background to full row
	GroupID            string           `json:"group_id,omitempty"`             // Session group ID
	Agent              AgentType        `json:"agent,omitempty"`                // Agent type (claude, gemini, aider, custom)
	CustomCommand      string           `json:"custom_command,omitempty"`       // Custom command for AgentCustom
	ExtraArgs          string           `json:"extra_args,omitempty"`           // Extra CLI arguments appended to agent command
	Notes              string           `json:"notes,omitempty"`                // User notes/comments for this session
	FollowedWindows    []FollowedWindow `json:"followed_windows,omitempty"`     // Windows tracked as agents (window 0 is main agent)
	BaseCommitSHA      string           `json:"base_commit_sha,omitempty"`      // Git HEAD commit at session start (for diff)
	Favorite           bool             `json:"favorite,omitempty"`             // Whether session is marked as favorite
	MainWindowStopped  bool             `json:"main_window_stopped,omitempty"`  // Main window (0) is stopped but session still running
	TabOrder           []int            `json:"tab_order,omitempty"`            // Custom tab display order (tmux window indices); if empty, default order is used
	TabTextColor       string           `json:"tab_text_color,omitempty"`       // Main tab text color (empty uses the theme default)
	TabBackgroundColor string           `json:"tab_background_color,omitempty"` // Main tab background color (empty uses the theme default)
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
	Index           int       `json:"index"`
	Agent           AgentType `json:"agent"`
	Name            string    `json:"name"`                       // Tab name for display
	CustomCommand   string    `json:"custom_command"`             // For custom agents
	AutoYes         bool      `json:"auto_yes"`                   // YOLO mode for this tab
	ResumeSessionID string    `json:"resume_session_id"`          // Resume session ID for this tab
	Notes           string    `json:"notes,omitempty"`            // User notes for this tab
	ExtraArgs       string    `json:"extra_args,omitempty"`       // Extra CLI arguments for this tab
	Stopped         bool      `json:"stopped,omitempty"`          // Tab is stopped (window killed but can resume)
	TextColor       string    `json:"text_color,omitempty"`       // Tab text color (empty uses the theme default)
	BackgroundColor string    `json:"background_color,omitempty"` // Tab background color (empty uses the theme default)
	WorkDir         string    `json:"work_dir,omitempty"`         // Tab working directory (empty = session path)
	HideStatusLine  bool      `json:"hide_status_line,omitempty"` // Don't show this tab's status line in the session list
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

func generateID(name string, agent AgentType) string {
	sanitized := strings.ToLower(name)
	sanitized = strings.ReplaceAll(sanitized, " ", "_")
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
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{session_attached}")
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

func (i *Instance) StartWithResume(resumeID string) error {
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

	// Check if tmux session already exists
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	sessionExists := checkCmd.Run() == nil

	if !sessionExists {
		// Build command based on agent type
		config := i.GetAgentConfig()
		var argv []string // tmux command in argv form (no shell layer)
		var cmdToCheck string

		if i.Agent == AgentCustom {
			// Use custom command directly, split into argv tokens.
			argv = customCommandArgv(i.CustomCommand)
			if len(argv) > 0 {
				cmdToCheck = argv[0]
			}
		} else {
			cmdToCheck = config.Command
			args := []string{}

			// Handle resume subcommands (codex resume, q chat --resume) vs flags (claude --resume)
			if config.SupportsResume && config.ResumeIsSubcommand {
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
		log.Printf("[StartWithResume] final argv: tmux new-session -d -s %s -c %s -- %v", sessionName, i.Path, argv)
		tmuxArgs := append([]string{"new-session", "-d", "-s", sessionName, "-c", i.Path}, argv...)
		cmd := exec.Command("tmux", tmuxArgs...)
		// Pin a sane TERM for the session's child processes. Launched from a
		// desktop menu / KRunner the app inherits TERM=dumb (or empty), which
		// would propagate into the agent running inside tmux.
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}

		// Wait for session to be ready
		for j := 0; j < 20; j++ {
			checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
			if checkCmd.Run() == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Keep windows alive when their process exits (shows as dead pane)
		exec.Command("tmux", "set-option", "-t", sessionName, "remain-on-exit", "on").Run()

		// Configure tmux session for better scrolling
		exec.Command("tmux", "set-option", "-t", sessionName, "history-limit", "50000").Run()
		exec.Command("tmux", "set-option", "-t", sessionName, "mouse", "on").Run()

		// Hide tmux status bar (not needed in GUI, wastes a row)
		exec.Command("tmux", "set-option", "-t", sessionName, "status", "off").Run()

		// Use latest client size and aggressive resize for proper terminal following
		exec.Command("tmux", "set-option", "-t", sessionName, "window-size", "latest").Run()
		exec.Command("tmux", "set-option", "-t", sessionName, "aggressive-resize", "on").Run()

		// Enable xterm keys for Shift+PageUp/Down support
		exec.Command("tmux", "set-option", "-t", sessionName, "-g", "xterm-keys", "on").Run()

		// Set terminal overrides for better key support
		exec.Command("tmux", "set-option", "-t", sessionName, "-ga", "terminal-overrides", ",xterm*:smcup@:rmcup@").Run()

		// Bind Shift+PageUp/Down for scrolling in copy mode (conditional - only in asmgr-* sessions)
		exec.Command("tmux", "bind-key", "-T", "root", "S-PageUp", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "copy-mode -eu", "").Run()
		exec.Command("tmux", "bind-key", "-T", "root", "S-PageDown", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys PageDown", "").Run()
		exec.Command("tmux", "bind-key", "-T", "copy-mode-vi", "S-PageUp", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys -X page-up", "").Run()
		exec.Command("tmux", "bind-key", "-T", "copy-mode-vi", "S-PageDown", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", "send-keys -X page-down", "").Run()

		// Bind Ctrl+Y for yolo mode toggle (conditional - only in asmgr-* sessions)
		exec.Command("tmux", "bind-key", "-n", "C-y", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", `run-shell 'asmgr yolo "$(tmux display-message -p "#{session_name}")" "$(tmux display-message -p "#{window_index}")" 2>/dev/null'`, "").Run()

		// Ctrl+q will be set up with resize in UpdateDetachBinding

		// Set window 0 name to agent type (session name is shown in status bar)
		exec.Command("tmux", "rename-window", "-t", sessionName+":0", i.WindowName()).Run()

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

	return nil
}

// saveBaseCommit saves the current git HEAD commit SHA for diff tracking
func (i *Instance) saveBaseCommit() {
	// Only save if not already set (preserve original base on restart)
	if i.BaseCommitSHA != "" {
		return
	}

	// Check if path is a git repo and get HEAD commit
	cmd := exec.Command("git", "-C", i.Path, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// Not a git repo or error - no diff available
		return
	}

	i.BaseCommitSHA = strings.TrimSpace(string(output))
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

		if fw.Agent == AgentTerminal {
			// Terminal window - just create empty shell
			cmd = exec.Command("tmux", "new-window", "-t", sessionName, "-c", tabDir, "-n", fw.Name)
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
			tmuxArgs := append([]string{"new-window", "-t", sessionName, "-c", tabDir, "-n", fw.Name}, argv...)
			cmd = exec.Command("tmux", tmuxArgs...)
		}

		if err := cmd.Run(); err != nil {
			continue // Skip failed windows
		}

		// Get the new window index
		newIdx := i.GetCurrentWindowIndex()

		// Set remain-on-exit so window stays open when command exits (shows as stopped)
		target := fmt.Sprintf("%s:%d", sessionName, newIdx)
		exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
		// Disable automatic-rename so the window keeps the user-specified name
		exec.Command("tmux", "set-option", "-t", target, "automatic-rename", "off").Run()

		// Re-add to followed windows with updated index (preserve all fields)
		i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
			Index:           newIdx,
			Agent:           fw.Agent,
			Name:            fw.Name,
			CustomCommand:   fw.CustomCommand,
			AutoYes:         fw.AutoYes,
			ResumeSessionID: resumeID,
			Notes:           fw.Notes,
			ExtraArgs:       fw.ExtraArgs,
			TextColor:       fw.TextColor,
			BackgroundColor: fw.BackgroundColor,
		})
	}

	// Clear TabOrder since window indices changed after restart
	i.TabOrder = nil

	// Switch back to window 0 (main agent)
	exec.Command("tmux", "select-window", "-t", sessionName+":0").Run()
}

func (i *Instance) Stop() error {
	if i.Status != StatusRunning {
		return nil
	}

	sessionName := i.TmuxSessionName()

	// Kill all linked GUI sessions first (they share the same tmux session group).
	// Format: <sessionName>_gui_<N>_<timestamp>
	out, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if out != nil {
		prefix := sessionName + "_gui_"
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasPrefix(line, prefix) {
				exec.Command("tmux", "kill-session", "-t", line).Run()
			}
		}
	}

	// Kill the base tmux session
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		// If the base session is already gone (killed by group cascade), that's OK
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
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
	cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
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
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path)
	return cmd.Run()
}

// NewWindowWithName creates a new tmux window with a specific name
func (i *Instance) NewWindowWithName(name string, workDir string) error {
	if workDir == "" {
		workDir = i.Path
	}
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", workDir, "-n", name)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Track terminal window for restore on restart
	newIdx := i.GetCurrentWindowIndex()
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
	exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
	// Disable automatic-rename so the window keeps the user-specified name
	exec.Command("tmux", "set-option", "-t", target, "automatic-rename", "off").Run()

	return nil
}

// StopWindow stops the agent in a specific tmux window.
// For window 0: if there are active followed windows, only kills the main agent
// process (keeps session alive). Otherwise kills the entire tmux session.
// For followed windows: kills the tmux window and marks the tab as stopped.
func (i *Instance) StopWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()

	if windowIdx == 0 {
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
		target := fmt.Sprintf("%s:0", sessionName)
		// Keep the window alive as a dead pane
		exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
		// Kill the agent and replace with an immediately-exiting command
		if err := exec.Command("tmux", "respawn-pane", "-k", "-t", target, "exit 0").Run(); err != nil {
			return fmt.Errorf("failed to stop main window: %w", err)
		}

		i.MainWindowStopped = true
		return nil
	}

	// Followed window: stop the process but keep the window (dead pane)
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
	if err := exec.Command("tmux", "respawn-pane", "-k", "-t", target, "exit 0").Run(); err != nil {
		return fmt.Errorf("failed to stop window %s: %w", target, err)
	}

	// Mark the followed window as stopped
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			i.FollowedWindows[idx].Stopped = true
			break
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
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	if windowIdx == 0 {
		// Main window: restart the main agent
		config := AgentConfigs[i.Agent]
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
		if err := exec.Command("tmux", tmuxArgs...).Run(); err != nil {
			return fmt.Errorf("failed to restart main window: %w", err)
		}
		i.MainWindowStopped = false
		return nil
	}

	// Followed window: find the agent config and restart
	var fw *FollowedWindow
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			fw = &i.FollowedWindows[idx]
			break
		}
	}
	if fw == nil {
		log.Printf("[RestartWindow] window %d not found in followedWindows (count=%d)", windowIdx, len(i.FollowedWindows))
		for _, w := range i.FollowedWindows {
			log.Printf("[RestartWindow]   fw: index=%d agent=%s name=%q resumeID=%q stopped=%v", w.Index, w.Agent, w.Name, w.ResumeSessionID, w.Stopped)
		}
		return fmt.Errorf("window %d not found in followed windows", windowIdx)
	}

	log.Printf("[RestartWindow] found fw: index=%d agent=%s name=%q resumeID=%q stopped=%v extraArgs=%q", fw.Index, fw.Agent, fw.Name, fw.ResumeSessionID, fw.Stopped, fw.ExtraArgs)

	var argv []string
	if fw.Agent == AgentTerminal {
		// Use $SHELL or fallback to bash — respawn-pane without a command
		// re-runs the pane's original start command, which is "exit 0" for stopped tabs
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		argv = []string{shell}
	} else if fw.Agent == AgentCustom {
		argv = customCommandArgv(fw.CustomCommand)
	} else {
		config := AgentConfigs[fw.Agent]
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
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		argv = []string{shell}
	}
	log.Printf("[RestartWindow] followed win final argv: tmux respawn-pane -k -t %s -- %v", target, argv)
	tmuxArgs := append([]string{"respawn-pane", "-k", "-t", target}, argv...)
	if err := exec.Command("tmux", tmuxArgs...).Run(); err != nil {
		return fmt.Errorf("failed to restart window %d: %w", windowIdx, err)
	}

	fw.Stopped = false
	return nil
}

func (i *Instance) RestartWindow(windowIdx int) error {
	return i.RestartWindowWithResume(windowIdx, "")
}

// DeleteWindow removes a followed window. If the session is running and the
// window is not already stopped, it kills the tmux window first.
func (i *Instance) DeleteWindow(windowIdx int) error {
	if windowIdx == 0 {
		return fmt.Errorf("cannot delete main agent window")
	}

	// Kill the tmux window if session is running
	if i.Status == StatusRunning {
		sessionName := i.TmuxSessionName()
		target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
		exec.Command("tmux", "kill-window", "-t", target).Run()
	}

	// Find and remove from FollowedWindows (if tracked)
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

// CloseWindow closes a tmux window by index and removes it from FollowedWindows
func (i *Instance) CloseWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	// Don't allow closing window 0 (main agent)
	if windowIdx == 0 {
		return fmt.Errorf("cannot close main agent window")
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Kill the tmux window
	cmd := exec.Command("tmux", "kill-window", "-t", target)
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
	cmd := exec.Command("tmux", "list-windows", "-t", sessionName)
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
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{window_index}")
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
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{window_name}")
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
	cmd := exec.Command("tmux", "select-window", "-t", fmt.Sprintf("%s:%d", sessionName, index))
	return cmd.Run()
}

// NextWindow switches to the next tmux window
func (i *Instance) NextWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "next-window", "-t", sessionName)
	return cmd.Run()
}

// PrevWindow switches to the previous tmux window
func (i *Instance) PrevWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "previous-window", "-t", sessionName)
	return cmd.Run()
}

// RenameCurrentWindow renames the current tmux window
func (i *Instance) RenameCurrentWindow(name string) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "rename-window", "-t", sessionName, name)
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
	cmd := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_index}:#{window_name}:#{window_active}:#{pane_dead}")
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
	tmuxArgs := append([]string{"new-window", "-t", sessionName, "-c", workDir, "-n", name}, argv...)
	cmd := exec.Command("tmux", tmuxArgs...)
	if err := cmd.Run(); err != nil {
		return -1, err
	}

	// Get the new window index
	newIdx := i.GetCurrentWindowIndex()

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
	exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
	// Disable automatic-rename so the window keeps the user-specified name
	exec.Command("tmux", "set-option", "-t", target, "automatic-rename", "off").Run()

	return newIdx, nil
}

// ForkSession creates a fork of the current Claude session using --fork-session
// Returns the new session ID
func (i *Instance) ForkSession() (string, error) {
	if i.Agent != AgentClaude {
		return "", fmt.Errorf("fork is only supported for Claude sessions")
	}

	// Get current session ID
	sessionID := i.ResumeSessionID
	if sessionID == "" {
		return "", fmt.Errorf("no session ID to fork - session may not have started yet")
	}

	// Run claude with --fork-session to get new session ID
	// This doesn't actually run the agent, just creates the fork and returns the ID
	cmd := exec.Command("claude", "--resume", sessionID, "--fork-session", "--output-format", "json", "-p", ".")
	cmd.Dir = i.Path

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fork session: %w", err)
	}

	// Parse JSON output to get new session ID
	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("failed to parse fork output: %w", err)
	}

	if result.SessionID == "" {
		return "", fmt.Errorf("fork returned empty session ID")
	}

	return result.SessionID, nil
}

// NewForkedTab creates a new tab with a forked Claude session
func (i *Instance) NewForkedTab(name string, sessionID string) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()

	// Build claude command with resume
	config := AgentConfigs[AgentClaude]
	args := []string{}

	// Add auto-yes flag if the main session has it enabled
	if i.AutoYes && config.AutoYesFlag != "" {
		args = append(args, config.AutoYesFlag)
	}

	// Add resume flag with forked session ID
	args = append(args, config.ResumeFlag, sessionID)

	argv := buildAgentArgv(config.Command, args, "")

	// Create new window with forked agent (argv form, no shell layer).
	tmuxArgs := append([]string{"new-window", "-t", sessionName, "-c", i.Path, "-n", name}, argv...)
	cmd := exec.Command("tmux", tmuxArgs...)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Get the new window index
	newIdx := i.GetCurrentWindowIndex()

	// Add to followed windows with fork info
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		Index:           newIdx,
		Agent:           AgentClaude,
		Name:            name,
		ResumeSessionID: sessionID,
		Notes:           "Forked session",
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
	exec.Command("tmux", "set-option", "-t", target, "automatic-rename", "off").Run()

	return nil
}

func (i *Instance) IsAlive() bool {
	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	return cmd.Run() == nil
}

// ResizePane resizes the tmux pane to the specified dimensions
func (i *Instance) ResizePane(width, height int) error {
	if !i.IsAlive() {
		return nil
	}
	sessionName := i.TmuxSessionName()
	return exec.Command("tmux", "resize-window", "-t", sessionName, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height)).Run()
}

// UpdateDetachBinding updates Ctrl+Q to resize to preview size before detaching
func (i *Instance) UpdateDetachBinding(previewWidth, previewHeight int) {
	if !i.IsAlive() {
		return
	}
	// Bind Ctrl+Q: conditional - only in asmgr-* sessions, with resize before detach
	// Use if-shell for the condition check, then run-shell for the actual commands
	resizeAndDetach := fmt.Sprintf("run-shell 'tmux resize-window -x %d -y %d 2>/dev/null; tmux detach-client'", previewWidth, previewHeight)
	exec.Command("tmux", "bind-key", "-n", "C-q", "if-shell", "tmux display -p '#{session_name}' | grep -q '^asm_'", resizeAndDetach, "").Run()
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
	cmd := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-e", "-J", "-S", startLine)
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
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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

	target := i.GetCaptureTarget(windowIdx)
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, keys)
	return cmd.Run()
}

// SendKeysToWindow sends a tmux key name to a specific window of this session.
func (i *Instance) SendKeysToWindow(windowIdx int, keys string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	return exec.Command("tmux", "send-keys", "-t", target, keys).Run()
}

// SendText sends text literally (not interpreted as key names)
func (i *Instance) SendText(text string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()
	// Use -l flag to send text literally without interpreting key names
	cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
	return cmd.Run()
}

// SendPrompt sends a prompt text followed by Enter key
func (i *Instance) SendPrompt(text string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()

	if strings.Contains(text, "\n") {
		// Multi-line text: use tmux's paste buffer with bracketed paste mode.
		// Without this, each newline would be interpreted as Enter by the terminal,
		// causing the prompt to be submitted line-by-line instead of as one block.
		if err := exec.Command("tmux", "set-buffer", "--", text).Run(); err != nil {
			return fmt.Errorf("failed to set tmux buffer: %w", err)
		}
		if err := exec.Command("tmux", "paste-buffer", "-p", "-t", sessionName).Run(); err != nil {
			// Fallback: paste without -p if not supported
			if err2 := exec.Command("tmux", "paste-buffer", "-t", sessionName).Run(); err2 != nil {
				return fmt.Errorf("failed to paste buffer: %w", err2)
			}
		}
	} else {
		// Single-line text: use send-keys -l for simplicity
		cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	// Wait for the text to be fully processed by the terminal/agent
	time.Sleep(100 * time.Millisecond)

	// Dismiss any autocomplete/suggestion popup (e.g. Claude Code)
	// Escape closes suggestions without affecting the pasted text
	exec.Command("tmux", "send-keys", "-t", sessionName, "Escape").Run()
	time.Sleep(50 * time.Millisecond)

	// Then send Enter separately to submit the prompt
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "Enter")
	return cmd.Run()
}

// IsMainWindowDead checks if the main window (0) pane is dead in tmux
func (i *Instance) IsMainWindowDead() bool {
	if !i.IsAlive() {
		return false
	}
	target := fmt.Sprintf("%s:0", i.TmuxSessionName())
	cmd := exec.Command("tmux", "list-panes", "-t", target, "-F", "#{pane_dead}")
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
func (i *Instance) getDiff(baseRef string) *DiffStats {
	stats := &DiffStats{}

	if !i.isGitRepo() {
		stats.Error = fmt.Errorf("not a git repository")
		return stats
	}

	// Use a private temporary index so intent-to-add can make untracked files
	// visible without mutating the user's real staging area.
	tmpIndex, err := os.CreateTemp("", "asmgr-git-index-*")
	if err != nil {
		stats.Error = fmt.Errorf("failed to create temporary git index: %w", err)
		return stats
	}
	tmpIndexPath := tmpIndex.Name()
	tmpIndex.Close()
	os.Remove(tmpIndexPath) // Git expects a missing or valid index, not an empty file.
	defer os.Remove(tmpIndexPath)

	gitEnv := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndexPath)
	readTree := exec.Command("git", "-C", i.Path, "read-tree", "HEAD")
	readTree.Env = gitEnv
	if err := readTree.Run(); err != nil {
		// An unborn repository has no HEAD yet; start from an empty index.
		readEmpty := exec.Command("git", "-C", i.Path, "read-tree", "--empty")
		readEmpty.Env = gitEnv
		if emptyErr := readEmpty.Run(); emptyErr != nil {
			stats.Error = fmt.Errorf("failed to prepare temporary git index: %w", err)
			return stats
		}
	}
	intentToAdd := exec.Command("git", "-C", i.Path, "add", "-N", ".")
	intentToAdd.Env = gitEnv
	if err := intentToAdd.Run(); err != nil {
		stats.Error = fmt.Errorf("failed to include untracked files in diff: %w", err)
		return stats
	}

	// Build git diff command
	args := []string{"-C", i.Path, "--no-pager", "diff"}
	if baseRef != "" {
		args = append(args, baseRef)
	}

	cmd := exec.Command("git", args...)
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
	cmd := exec.Command("git", "-C", i.Path, "rev-parse", "--git-dir")
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

// GetMainWindowIndex returns the index of the first tmux window for this session.
// Window index may not be 0 if windows were reordered or renumbered.
func (i *Instance) GetMainWindowIndex() int {
	if i.Status != StatusRunning {
		return 0
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_index}")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return 0
	}

	var firstIdx int
	fmt.Sscanf(lines[0], "%d", &firstIdx)
	return firstIdx
}
