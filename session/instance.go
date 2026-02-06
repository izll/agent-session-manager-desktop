package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"asmgr-desktop/session/filters"
	"github.com/mattn/go-runewidth"
)

// ansiRegex matches ANSI escape sequences
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

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
}

// AgentConfigs maps agent types to their configurations
var AgentConfigs = map[AgentType]AgentConfig{
	AgentClaude: {
		Command:         "claude",
		SupportsResume:  true,
		SupportsAutoYes: true,
		AutoYesFlag:     "--dangerously-skip-permissions",
		ResumeFlag:      "--resume",
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
		Command:            "codex",
		SupportsResume:     true,
		SupportsAutoYes:    true,
		AutoYesFlag:        "--full-auto",
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
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	Status          Status    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	AutoYes         bool      `json:"auto_yes"`
	ResumeSessionID string    `json:"resume_session_id,omitempty"` // Claude session ID to resume
	Color           string    `json:"color,omitempty"`             // Foreground color
	BgColor         string    `json:"bg_color,omitempty"`          // Background color
	FullRowColor    bool      `json:"full_row_color,omitempty"`    // Extend background to full row
	GroupID         string    `json:"group_id,omitempty"`          // Session group ID
	Agent           AgentType `json:"agent,omitempty"`             // Agent type (claude, gemini, aider, custom)
	CustomCommand   string    `json:"custom_command,omitempty"`    // Custom command for AgentCustom
	Notes           string           `json:"notes,omitempty"`             // User notes/comments for this session
	FollowedWindows []FollowedWindow `json:"followed_windows,omitempty"`  // Windows tracked as agents (window 0 is main agent)
	BaseCommitSHA   string           `json:"base_commit_sha,omitempty"`   // Git HEAD commit at session start (for diff)
	Favorite        bool             `json:"favorite,omitempty"`          // Whether session is marked as favorite
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
	Name            string    `json:"name"`              // Tab name for display
	CustomCommand   string    `json:"custom_command"`    // For custom agents
	AutoYes         bool      `json:"auto_yes"`          // YOLO mode for this tab
	ResumeSessionID string    `json:"resume_session_id"` // Resume session ID for this tab
	Notes           string    `json:"notes,omitempty"`   // User notes for this tab
	Stopped         bool      `json:"stopped,omitempty"` // Tab is stopped (window killed but can resume)
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

func NewInstance(name, path string, autoYes bool, agent AgentType) (*Instance, error) {
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

// CheckAgentCommand verifies that the agent command exists in PATH
func CheckAgentCommand(inst *Instance) error {
	var cmdToCheck string

	if inst.Agent == AgentCustom {
		// Extract the base command (first word) from custom command
		parts := strings.Fields(inst.CustomCommand)
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
		var agentCmd string
		var cmdToCheck string

		if i.Agent == AgentCustom {
			// Use custom command directly
			agentCmd = i.CustomCommand
			// Extract the base command (first word) to check
			parts := strings.Fields(i.CustomCommand)
			if len(parts) > 0 {
				cmdToCheck = parts[0]
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
					}
				}
			}

			agentCmd = config.Command + " " + strings.Join(args, " ")
		}

		// Check if the command exists
		if cmdToCheck != "" {
			if _, err := exec.LookPath(cmdToCheck); err != nil {
				return fmt.Errorf("command '%s' not found - is it installed?", cmdToCheck)
			}
		}

		// Create new tmux session
		cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", i.Path, agentCmd)
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

		if fw.Agent == AgentTerminal {
			// Terminal window - just create empty shell
			cmd = exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", fw.Name)
		} else {
			// Agent window - build agent command
			config := AgentConfigs[fw.Agent]
			var agentCmd string

			if fw.Agent == AgentCustom {
				agentCmd = fw.CustomCommand
			} else {
				args := []string{}
				if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
					args = append(args, config.AutoYesFlag)
				}
				agentCmd = config.Command
				if len(args) > 0 {
					agentCmd = agentCmd + " " + strings.Join(args, " ")
				}
			}

			// Create new window with agent command
			cmd = exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", fw.Name, agentCmd)
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

		// Re-add to followed windows with updated index
		i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
			Index:         newIdx,
			Agent:         fw.Agent,
			Name:          fw.Name,
			CustomCommand: fw.CustomCommand,
		})
	}

	// Switch back to window 0 (main agent)
	exec.Command("tmux", "select-window", "-t", sessionName+":0").Run()
}

func (i *Instance) Stop() error {
	if i.Status != StatusRunning {
		return nil
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %w", err)
	}

	i.Status = StatusStopped
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
func (i *Instance) NewWindowWithName(name string) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", name)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Track terminal window for restore on restart
	newIdx := i.GetCurrentWindowIndex()
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		Index: newIdx,
		Agent: AgentTerminal,
		Name:  name,
	})

	// Set remain-on-exit so window stays open when command exits (shows as stopped)
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", "on").Run()
	// Disable automatic-rename so the window keeps the user-specified name
	exec.Command("tmux", "set-option", "-t", target, "automatic-rename", "off").Run()

	return nil
}

// RespawnWindow restarts a dead window's process
func (i *Instance) RespawnWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Get the agent type for this window to build the command
	var agentCmd string
	if windowIdx == 0 {
		// Main window - use instance's agent
		config := i.GetAgentConfig()
		if i.Agent == AgentCustom {
			agentCmd = i.CustomCommand
		} else {
			args := []string{}
			if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
				args = append(args, config.AutoYesFlag)
			}
			agentCmd = config.Command
			if len(args) > 0 {
				agentCmd = agentCmd + " " + strings.Join(args, " ")
			}
		}
	} else {
		// Followed window - find the agent type
		for _, fw := range i.FollowedWindows {
			if fw.Index == windowIdx {
				if fw.Agent == AgentTerminal {
					// Terminal - just respawn shell
					agentCmd = ""
				} else if fw.Agent == AgentCustom {
					agentCmd = fw.CustomCommand
				} else {
					config := AgentConfigs[fw.Agent]
					args := []string{}
					if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
						args = append(args, config.AutoYesFlag)
					}
					agentCmd = config.Command
					if len(args) > 0 {
						agentCmd = agentCmd + " " + strings.Join(args, " ")
					}
				}
				break
			}
		}
	}

	// Respawn the pane with the command
	if agentCmd != "" {
		return exec.Command("tmux", "respawn-pane", "-k", "-t", target, agentCmd).Run()
	}
	// Empty command = default shell
	return exec.Command("tmux", "respawn-pane", "-k", "-t", target).Run()
}

// RespawnWindowWithResume restarts a window's process with a specific resume session ID
func (i *Instance) RespawnWindowWithResume(windowIdx int, resumeID string) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Get the agent type for this window to build the command
	var agentCmd string
	if windowIdx == 0 {
		// Main window - use instance's agent
		config := i.GetAgentConfig()
		if i.Agent == AgentCustom {
			agentCmd = i.CustomCommand
		} else {
			var args []string
			if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
				args = append(args, config.AutoYesFlag)
			}
			if config.SupportsResume && config.ResumeFlag != "" && resumeID != "" {
				args = append(args, config.ResumeFlag, resumeID)
			}
			agentCmd = config.Command
			if len(args) > 0 {
				agentCmd = agentCmd + " " + strings.Join(args, " ")
			}
		}
	} else {
		// Followed window - find the agent type
		for _, fw := range i.FollowedWindows {
			if fw.Index == windowIdx {
				if fw.Agent == AgentTerminal {
					// Terminal - just respawn shell
					agentCmd = ""
				} else if fw.Agent == AgentCustom {
					agentCmd = fw.CustomCommand
				} else {
					config := AgentConfigs[fw.Agent]
					var args []string
					if fw.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
						args = append(args, config.AutoYesFlag)
					}
					if config.SupportsResume && config.ResumeFlag != "" && resumeID != "" {
						args = append(args, config.ResumeFlag, resumeID)
					}
					agentCmd = config.Command
					if len(args) > 0 {
						agentCmd = agentCmd + " " + strings.Join(args, " ")
					}
				}
				break
			}
		}
	}

	// Respawn the pane with the command
	if agentCmd != "" {
		return exec.Command("tmux", "respawn-pane", "-k", "-t", target, agentCmd).Run()
	}
	// Empty command = default shell
	return exec.Command("tmux", "respawn-pane", "-k", "-t", target).Run()
}

// StopWindow stops the agent in a tmux window by sending Ctrl+C twice
// The window stays open so user can switch to it and respawn with Enter
func (i *Instance) StopWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Send Ctrl+C twice to stop the agent (Claude needs 2x)
	exec.Command("tmux", "send-keys", "-t", target, "C-c").Run()
	time.Sleep(100 * time.Millisecond)
	exec.Command("tmux", "send-keys", "-t", target, "C-c").Run()

	return nil
}

// ResumeStoppedTab resumes a stopped tab by creating a new window and starting the agent
func (i *Instance) ResumeStoppedTab(fwIndex int) (int, error) {
	if i.Status != StatusRunning {
		return 0, fmt.Errorf("instance not running")
	}

	// Find the stopped FollowedWindow
	var fw *FollowedWindow
	var fwIdx int
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == fwIndex && i.FollowedWindows[idx].Stopped {
			fw = &i.FollowedWindows[idx]
			fwIdx = idx
			break
		}
	}
	if fw == nil {
		return 0, fmt.Errorf("stopped tab not found")
	}

	sessionName := i.TmuxSessionName()

	// Create a new tmux window with the tab name
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", fw.Name, "-P", "-F", "#{window_index}")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to create window: %w", err)
	}

	// Parse the new window index
	newIdx := 0
	fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &newIdx)

	// Build the command to run
	agentCmd := i.buildAgentCommand(fw.Agent, fw.CustomCommand, fw.AutoYes, fw.ResumeSessionID)

	// Send the command to the new window
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	exec.Command("tmux", "send-keys", "-t", target, agentCmd, "Enter").Run()

	// Update the FollowedWindow
	i.FollowedWindows[fwIdx].Index = newIdx
	i.FollowedWindows[fwIdx].Stopped = false

	return newIdx, nil
}

// buildAgentCommand builds the command string to run an agent
func (i *Instance) buildAgentCommand(agent AgentType, customCmd string, autoYes bool, resumeID string) string {
	if agent == AgentCustom && customCmd != "" {
		return customCmd
	}

	if agent == AgentTerminal {
		return "" // Terminal just opens a shell
	}

	config, ok := AgentConfigs[agent]
	if !ok {
		config = AgentConfigs[AgentClaude] // Default to Claude
	}

	cmd := config.Command

	// Add resume flag if applicable
	if resumeID != "" && config.SupportsResume {
		if config.ResumeIsSubcommand {
			cmd = cmd + " " + config.ResumeFlag + " " + resumeID
		} else {
			cmd = cmd + " " + config.ResumeFlag + " " + resumeID
		}
	}

	// Add auto-yes flag if applicable
	if autoYes && config.SupportsAutoYes {
		cmd = cmd + " " + config.AutoYesFlag
	}

	return cmd
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
	Index    int
	Name     string
	Active   bool
	Followed bool      // Whether this window is tracked as an agent
	Agent    AgentType // Agent type if followed
	Dead     bool      // Whether the window's pane has exited (command finished)
}

// IsWindowFollowed checks if a window index is being tracked as an agent
func (i *Instance) IsWindowFollowed(index int) bool {
	// Window 0 is always followed (main agent)
	if index == 0 {
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
	// Window 0 is the main agent
	if index == 0 {
		return &FollowedWindow{Index: 0, Agent: i.Agent, Name: i.Name}
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
	// Can't unfollow window 0
	if index == 0 {
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

// GetAllFollowedAgents returns info about all followed agents (including main window 0)
func (i *Instance) GetAllFollowedAgents() []FollowedWindow {
	result := []FollowedWindow{
		{Index: 0, Agent: i.Agent, Name: i.Name},
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
			followed := i.IsWindowFollowed(idx)
			if followed {
				if fw := i.GetFollowedWindow(idx); fw != nil {
					agent = fw.Agent
				}
			}

			windows = append(windows, WindowInfo{
				Index:    idx,
				Name:     parts[1],
				Active:   parts[2] == "1",
				Followed: followed,
				Agent:    agent,
				Dead:     parts[3] == "1",
			})
		}
	}
	return windows
}

// NewAgentWindow creates a new tmux window running the specified agent
func (i *Instance) NewAgentWindow(name string, agent AgentType, customCmd string) (int, error) {
	if i.Status != StatusRunning {
		return -1, fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()

	// Build agent command based on agent type
	config := AgentConfigs[agent]
	var agentCmd string

	if agent == AgentCustom {
		agentCmd = customCmd
	} else {
		args := []string{}
		// Use instance's AutoYes setting for the new agent too
		if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" {
			args = append(args, config.AutoYesFlag)
		}
		agentCmd = config.Command
		if len(args) > 0 {
			agentCmd = agentCmd + " " + strings.Join(args, " ")
		}
	}

	// Create new window with agent command
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", name, agentCmd)
	if err := cmd.Run(); err != nil {
		return -1, err
	}

	// Get the new window index
	newIdx := i.GetCurrentWindowIndex()

	// Add to followed windows with agent info
	i.FollowedWindows = append(i.FollowedWindows, FollowedWindow{
		Index:         newIdx,
		Agent:         agent,
		Name:          name,
		CustomCommand: customCmd,
	})

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

	agentCmd := config.Command + " " + strings.Join(args, " ")

	// Create new window with forked agent
	cmd := exec.Command("tmux", "new-window", "-t", sessionName, "-c", i.Path, "-n", name, agentCmd)
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

	sessionName := i.TmuxSessionName()
	// Always capture from window 0 (the agent window), not the currently active window
	// This ensures we always show the agent's status even when user is on another tab
	target := sessionName + ":0"
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
	for j := len(lines) - 1; j >= 0; j-- {
		line := lines[j]
		// Strip ANSI codes for checking
		cleanLine := strings.TrimSpace(StripANSI(line))
		// Skip empty lines
		if cleanLine == "" {
			continue
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

	return "..."
}

// GetLastLineForWindow returns the last meaningful line from a specific window
func (i *Instance) GetLastLineForWindow(windowIdx int, agent AgentType) string {
	if !i.IsAlive() {
		return "stopped"
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
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
	for j := len(lines) - 1; j >= 0; j-- {
		line := lines[j]
		cleanLine := strings.TrimSpace(StripANSI(line))
		if cleanLine == "" {
			continue
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

	// First send text literally with -l flag to avoid key interpretation
	cmd := exec.Command("tmux", "send-keys", "-l", "-t", sessionName, text)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Small delay to ensure text is processed before Enter
	time.Sleep(50 * time.Millisecond)

	// Then send Enter separately
	cmd = exec.Command("tmux", "send-keys", "-t", sessionName, "Enter")
	return cmd.Run()
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

	// Stage untracked files with intent-to-add for diff visibility
	i.stageUntrackedFiles()

	// Build git diff command
	args := []string{"-C", i.Path, "--no-pager", "diff"}
	if baseRef != "" {
		args = append(args, baseRef)
	}

	cmd := exec.Command("git", args...)
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

// stageUntrackedFiles adds untracked files with --intent-to-add for diff visibility
func (i *Instance) stageUntrackedFiles() {
	exec.Command("git", "-C", i.Path, "add", "-N", ".").Run()
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
