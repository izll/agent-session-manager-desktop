package session

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	AgentClaude AgentType = "claude"
	// Antigravity is the harness Google moved Gemini CLI into: the models are
	// the same, the binary is not. Listed before Gemini because it is the one
	// still served to consumer accounts — Gemini CLI stopped serving those on
	// 18 June 2026 and continues only on enterprise licences and paid keys.
	AgentAntigravity AgentType = "antigravity"
	AgentGemini      AgentType = "gemini"
	AgentAider       AgentType = "aider"
	AgentCodex       AgentType = "codex"
	AgentAmazonQ     AgentType = "amazonq"
	AgentOpenCode    AgentType = "opencode"
	AgentCursor      AgentType = "cursor"
	AgentCustom      AgentType = "custom"
	AgentTerminal    AgentType = "terminal" // Plain shell/terminal window
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

	// InstallURL is the agent's own installation page.
	//
	// Opened in a browser when the command is missing, rather than printing a
	// shell one-liner: the instructions differ by platform and change without
	// notice, and a stale command copied from here would be worse than none.
	// Empty for the pseudo-agents, which install nothing.
	InstallURL string
}

// AgentConfigs maps agent types to their configurations
var AgentConfigs = map[AgentType]AgentConfig{
	AgentClaude: {
		Command:           "claude",
		InstallURL:        "https://code.claude.com/docs/en/overview",
		SupportsResume:    true,
		SupportsAutoYes:   true,
		AutoYesFlag:       "--dangerously-skip-permissions",
		ResumeFlag:        "--resume",
		SupportsSessionID: true,
		SessionIDFlag:     "--session-id",
		// --fork-session alongside --resume: same history, new conversation.
		ForkFlag: "--fork-session",
	},
	AgentAntigravity: {
		// Taken from `agy --help` on 1.2.3, not from the documentation.
		//
		// --conversation <id> resumes a named conversation; --continue takes no
		// id and reopens whichever was most recent. ResumeFlag must be the
		// former: every resume here names the conversation the tab was on, and
		// passing that id after --continue left it as a bare argument, which
		// agy reads as a prompt — "unexpected argument <uuid>", and the agent
		// exited before it drew anything. There is still no --session-id to
		// pre-assign one with, so SupportsSessionID stays false; the id comes
		// from the presence lock the running agent holds.
		Command:         "agy",
		InstallURL:      "https://antigravity.google/docs/cli/install/",
		SupportsResume:  true,
		SupportsAutoYes: true,
		AutoYesFlag:     "--dangerously-skip-permissions",
		ResumeFlag:      "--conversation",
	},
	AgentGemini: {
		Command:         "gemini",
		InstallURL:      "https://github.com/google-gemini/gemini-cli",
		SupportsResume:  true,
		SupportsAutoYes: false,
		ResumeFlag:      "--resume",
	},
	AgentAider: {
		Command:         "aider",
		InstallURL:      "https://aider.chat/docs/install.html",
		SupportsResume:  false,
		SupportsAutoYes: true,
		AutoYesFlag:     "--yes",
	},
	AgentCodex: {
		Command:         "codex",
		InstallURL:      "https://github.com/openai/codex",
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
		InstallURL:         "https://docs.aws.amazon.com/amazonq/latest/qdeveloper-ug/command-line-installing.html",
		SupportsResume:     true,
		SupportsAutoYes:    true,
		AutoYesFlag:        "--trust-all-tools",
		ResumeFlag:         "chat --resume",
		ResumeIsSubcommand: true,
	},
	AgentOpenCode: {
		Command:         "opencode",
		InstallURL:      "https://opencode.ai/docs/",
		SupportsResume:  true,
		SupportsAutoYes: false,
		ResumeFlag:      "--session",
	},
	AgentCursor: {
		// cursor-agent, not cursor: the latter is the GUI editor, and starting
		// it here opened a window instead of an agent.
		Command:         "cursor-agent",
		InstallURL:      "https://cursor.com/docs/cli/installation",
		SupportsResume:  true,
		SupportsAutoYes: true,
		// --force allows commands unless explicitly denied; there is no
		// separate skip-all-permissions flag.
		AutoYesFlag: "--force",
		ResumeFlag:  "--resume",
	},
	AgentCustom: {
		Command:         "",
		SupportsResume:  false,
		SupportsAutoYes: false,
	},
}

type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// LastActiveAt is when an agent in this session was last seen working.
	//
	// Distinct from UpdatedAt, which moves only when the session is started or
	// stopped: sorting by that puts a session started this morning and busy
	// ever since below one started five minutes ago and idle the whole time,
	// which is the opposite of what "most recent activity" means.
	// omitempty does nothing for time.Time — a struct is never "empty" — so the
	// zero value is written out as year 1 and read back as a real timestamp.
	// Harmless here only because every comparison goes through
	// lastActivityTime, which treats it as "nothing observed".
	LastActiveAt    time.Time `json:"last_active_at,omitempty"`
	AutoYes         bool      `json:"auto_yes"`
	HideStatusLine  bool      `json:"hide_status_line,omitempty"`  // Don't show the main window's status line in the session list
	ResumeSessionID string    `json:"resume_session_id,omitempty"` // Claude session ID to resume
	// ServerID names the remote machine this session runs on; empty is this
	// computer. Per session rather than per project on purpose: the same
	// project is often worked on locally and on a server, and a sessions file
	// written before remote support existed loads unchanged, every entry local.
	ServerID string `json:"server_id,omitempty"`
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
	MainWindowName    string           `json:"main_window_name,omitempty"`    // User-defined main tmux tab name

	// gitDir is where git commands for this instance run.
	//
	// Path unless BrowseRoot overrides it — a tab can be opened in a directory
	// of its own, and the diff showed (and REVERTED into) the session's
	// repository whichever tab you were on. Reverting is the dangerous half:
	// it writes files.
	//
	// A method rather than a field so no git call can be added later that
	// forgets to consult it.

	// BrowseRoot redirects the file browser to a directory other than Path.
	//
	// Set per call by the caller that knows which TAB is being browsed — a tab
	// can be opened in its own directory, and the files view showed the session's
	// tree whichever tab you were on. Deliberately not persisted: it describes
	// one request, not the session.
	BrowseRoot       string `json:"-"`
	TabOrder         []int  `json:"tab_order,omitempty"`          // Custom tab display order (tmux window indices); if empty, default order is used
	TerminalTheme    string `json:"terminal_theme,omitempty"`     // Main window colour palette (empty inherits agent/global)
	TerminalFontSize int    `json:"terminal_font_size,omitempty"` // Main window font size in px (0 inherits the global setting)
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
	// ServerID names the machine this tab runs on, empty meaning the session's
	// own machine.
	//
	// A tab can live somewhere other than its session: the work it does there
	// — reading a database, watching a log, deploying — has nothing to do with
	// the files the session is open on, and asking the user to keep a second
	// session around just to reach the server put that work a window away from
	// the work it belongs to.
	//
	// Empty for every tab that existed before this, which reads as "wherever
	// the session runs" — exactly the old behaviour, with no migration.
	ServerID string `json:"server_id,omitempty"`
}

// Window indexes stay unique across the machines a session spans.
//
// The index is how everything addresses a tab — the UI, the terminal socket,
// the status poller, the quick-jump list — and changing that identity would
// touch every one of them. So instead of making the index ambiguous and
// carrying a machine alongside it everywhere, the indexes themselves are kept
// from colliding: tabs on a server are created at an index no local tab will
// be given, with `new-window -t name:N`, which asks the multiplexer for a
// specific slot.
//
// remoteWindowIndexBase is where a server's tabs start. tmux hands out low
// numbers from 0, and a session with dozens of local tabs is not a thing that
// happens, so the two ranges cannot meet in practice.
const remoteWindowIndexBase = 100

// remoteWindowIndexSpan is how much room each server gets inside that range,
// so two servers cannot collide with each other either.
const remoteWindowIndexSpan = 100

// serverForWindow says which machine a window index refers to.
//
// The UI addresses tabs by index, because that is what a multiplexer window
// is. The index is unique across machines (see remoteWindowIndexBase), so this
// is a lookup rather than a guess.
//
// A tab that is not in the followed list is the session's own main window,
// which always runs where the session does.
// hasRemoteTab reports whether any tab sits on a machine of its own.
func (i *Instance) hasRemoteTab() bool {
	for _, window := range i.FollowedWindows {
		if window.ServerID != "" && window.ServerID != i.ServerID {
			return true
		}
	}
	return false
}

// ServerForWindow is serverForWindow for callers outside this package — the
// terminal attach, which has to open its channel to the machine holding the
// tab rather than the one holding the session.
func (i *Instance) ServerForWindow(windowIdx int) string {
	return i.serverForWindow(windowIdx)
}

func (i *Instance) serverForWindow(windowIdx int) string {
	for _, window := range i.FollowedWindows {
		if window.Index == windowIdx {
			return window.RunsOn(i.ServerID)
		}
	}
	return i.ServerID
}

// RunsOn names the machine this tab's commands go to.
//
// A tab with no server of its own runs wherever its session runs, which is
// what every tab did before tabs could be placed individually.
func (fw FollowedWindow) RunsOn(sessionServerID string) string {
	if fw.ServerID != "" {
		return fw.ServerID
	}
	return sessionServerID
}

// ensureAgentOnServer checks that a tab's command exists where it will run.
//
// Without this the tab is created, the command is not found, the pane exits at
// once and the multiplexer replaces whatever the shell printed with the words
// "Pane is dead" — so the one line that explains the failure ("claude: command
// not found") is gone by the time anyone looks. Asked beforehand, the failure
// can say what is wrong and what to do about it.
//
// Only for a command we know the name of: a custom command may be a shell
// construct rather than a program, and a terminal tab runs the login shell.
func (i *Instance) ensureAgentOnServer(serverID, command string) error {
	if serverID == "" || command == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()

	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		// No way to ask; let the start proceed rather than refuse on a guess.
		return nil
	}
	_, _, exitCode, err := shell.RunShell(ctx, "", "command", "-v", command)
	if err != nil {
		// The server could not be asked. Same reasoning: a failure to check is
		// not a failure of the check.
		return nil
	}
	if exitCode != 0 {
		// Look for it in the usual places before giving up, so the message can
		// name the directory instead of describing the problem in general. The
		// user is looking at this error, not at the server manager, and being
		// told a path they can paste beats being told a setting exists.
		// Reported as a translation key with its values, so the message the
		// user reads is in their language. The convention — "error.<name>"
		// with values after a pipe — is the one the storage layer already
		// uses; the frontend resolves it.
		if found := i.findAgentOffPath(ctx, shell, command); found != "" {
			return fmt.Errorf("error.agentFoundOffPath|%s|%s", command, found)
		}
		return fmt.Errorf("error.agentNotOnServerPath|%s", command)
	}
	return nil
}

// resumeIDExistsOnServer reports whether a conversation exists on the machine
// a tab runs on.
//
// The local check reads this computer's ~/.claude, which is the wrong disk for
// a tab on a server: a conversation started here does not exist there, and one
// started there is invisible here. Passed to the agent anyway, it answers "no
// conversation found" and the tab is left showing that instead of working.
//
// Unknown agents and unreadable servers return true, matching the local
// behaviour: when the check cannot be made, the agent is allowed to try.
func (i *Instance) resumeIDExistsOnServer(serverID string, agent AgentType, resumeID string) bool {
	if serverID == "" || resumeID == "" {
		return true
	}
	if !IsSafeResumeID(resumeID) {
		return false
	}

	// Only the agents whose storage layout is known. Anything else is left to
	// the agent to decide, as it is locally.
	var probe string
	switch agent {
	case AgentClaude:
		probe = fmt.Sprintf(`ls "$HOME/.claude/projects"/*/%s.jsonl >/dev/null 2>&1`, resumeID)
	case AgentCodex:
		probe = fmt.Sprintf(`ls "$HOME/.codex/sessions"/*%s* >/dev/null 2>&1`, resumeID)
	default:
		return true
	}

	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()

	_, _, exitCode, err := shell.RunShell(ctx, "", "sh", "-c", probe)
	if err != nil {
		return true
	}
	return exitCode == 0
}

// autoYesRefusedAsRoot reports whether an agent will refuse its auto-yes flag
// because the server logs in as root.
//
// Claude and Cursor both reject --dangerously-skip-permissions under root, by
// design: the flag turns off the confirmations that stop an agent doing
// damage, and root is where that damage is unbounded. The refusal is printed
// and the agent exits, which in a tab reads as a pane that died for no reason.
//
// Knowing it in advance lets the tab start without the flag, with the reason
// said once, rather than not start at all.
func (i *Instance) autoYesRefusedAsRoot(serverID string, config AgentConfig) bool {
	if serverID == "" || config.AutoYesFlag == "" {
		return false
	}
	// Only the agents that actually refuse. Others accept the flag as root,
	// and dropping it silently would take away something the user asked for.
	if !strings.Contains(config.AutoYesFlag, "dangerously-skip-permissions") {
		return false
	}

	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()

	stdout, _, exitCode, err := shell.RunShell(ctx, "", "id", "-u")
	if err != nil || exitCode != 0 {
		return false
	}
	return strings.TrimSpace(string(stdout)) == "0"
}

// findAgentOffPath looks for a command in the directories an agent commonly
// lands in when a non-interactive shell cannot see it.
//
// Returns the directory, or empty when the command is genuinely absent.
func (i *Instance) findAgentOffPath(ctx context.Context, shell ShellExecutor, command string) string {
	for _, dir := range agentSearchDirectories {
		stdout, _, exitCode, err := shell.RunShell(ctx, "",
			"sh", "-c", fmt.Sprintf("[ -x %s/%s ] && echo %s", dir, command, dir))
		if err != nil || exitCode != 0 {
			continue
		}
		if found := strings.TrimSpace(string(stdout)); found != "" {
			return found
		}
	}
	return ""
}

// agentSearchDirectories mirrors the list the connection test uses, so the two
// cannot disagree about where an agent might be.
var agentSearchDirectories = []string{
	"$HOME/.local/bin",
	"$HOME/bin",
	"$HOME/.npm-global/bin",
	"/usr/local/bin",
	"/opt/homebrew/bin",
}

// ensureRemoteSessionFor makes sure this session has a multiplexer session on
// a server before a tab is put there.
//
// A session that runs here has no session on the server at all until its first
// tab goes there, so the first one creates it. The same name is used, which is
// unambiguous because it is a different machine's multiplexer.
//
// The placeholder window it is created with is left alone: killing it would
// end the session, and tabs are added beside it.
func (i *Instance) ensureRemoteSessionFor(serverID, workDir string) error {
	sessionName := i.TmuxSessionName()
	if err := i.tmuxRunOn(serverID, "has-session", "-t", sessionName); err == nil {
		return nil
	}

	// Detached, and holding nothing but a shell: the tabs are what the user
	// asked for, and this window only keeps the session alive.
	if err := i.tmuxRunOn(serverID, "new-session", "-d", "-s", sessionName, "-c", workDir); err != nil {
		return fmt.Errorf("error.couldNotStartRemoteSession")
	}

	// remain-on-exit for the whole session, set before any tab exists.
	//
	// Per window it is set just after the window is created, which is too late
	// for the case that matters most: an agent that is not on the server's
	// PATH dies instantly, and the window disappears before the option can be
	// applied. The tab then has nothing to attach to, and the terminal shows
	// an empty placeholder instead of the error the pane was holding.
	//
	// Set with -g, as the default for windows yet to be created. remain-on-exit
	// is a window option: applied to the session it reaches the windows that
	// already exist, and a tab created afterwards is born without it —
	// measured on tmux 2.6, where the failing window vanished exactly as
	// before. The global default is what a new window inherits.
	_ = i.tmuxRunOn(serverID, "set-option", "-t", sessionName, "-g", "remain-on-exit", "on")

	i.tagRemoteSession(serverID, sessionName)
	return nil
}

// Session options carrying who a multiplexer session on a server belongs to.
//
// A server is shared: the same machine can hold sessions from this computer,
// from another of the user's machines, and ones someone started by hand. They
// all look alike in `tmux ls` — a name and a window count — so anything that
// wants to say "this one is yours, that one is not" has to ask the session
// itself.
//
// Stored as tmux user options rather than in a file on the server: they live
// and die with the session, they need no cleanup, and `list-sessions -F` reads
// them for every session in one command. A file would have to be kept in step
// with sessions it does not own, and would outlive them.
const (
	sessionOwnerOption   = "@asmgr_owner"
	sessionProjectOption = "@asmgr_project"
	sessionPathOption    = "@asmgr_path"
	sessionAgentOption   = "@asmgr_agent"
)

// tagRemoteSession records who this session belongs to.
//
// Best-effort: a multiplexer that rejects a user option still runs the
// session, and the tagging is for telling sessions apart afterwards, not for
// running them.
func (i *Instance) tagRemoteSession(serverID, sessionName string) {
	for option, value := range map[string]string{
		sessionOwnerOption:   MachineIdentity(),
		sessionProjectOption: i.Name,
		sessionPathOption:    i.Path,
		sessionAgentOption:   string(i.Agent),
	} {
		if value == "" {
			continue
		}
		_ = i.tmuxRunOn(serverID, "set-option", "-t", sessionName, option, value)
	}
}

// machineIdentity is resolved once: the hostname does not change while the
// app runs, and asking the system for it on every session creation would be
// work for nothing.
var machineIdentity struct {
	sync.Once
	value string
}

// MachineIdentity names this computer, for marking what it owns on a server.
//
// The hostname, which is what a user recognises when looking at a list of
// sessions and deciding which are theirs. Empty when it cannot be read, in
// which case the session is simply left untagged rather than tagged with
// something misleading.
func MachineIdentity() string {
	machineIdentity.Do(func() {
		name, err := os.Hostname()
		if err != nil {
			return
		}
		machineIdentity.value = strings.TrimSpace(name)
	})
	return machineIdentity.value
}

// nextRemoteWindowIndex picks a free index for a new tab on a server.
//
// Each server gets its own band inside the remote range, so two servers cannot
// collide; within a band the next free slot is taken. Returns the index to ask
// the multiplexer for.
func (i *Instance) nextRemoteWindowIndex(serverID string) int {
	base := remoteWindowIndexBase + remoteWindowIndexSpan*i.serverBand(serverID)

	used := make(map[int]bool, len(i.FollowedWindows))
	for _, window := range i.FollowedWindows {
		used[window.Index] = true
	}
	for candidate := base; candidate < base+remoteWindowIndexSpan; candidate++ {
		if !used[candidate] {
			return candidate
		}
	}
	// A band with a hundred tabs in it is not a situation to design for, but
	// silently reusing an index would corrupt the list. Past the end, keep
	// counting: the result is still unique among the tabs we hold.
	highest := base
	for _, window := range i.FollowedWindows {
		if window.Index > highest {
			highest = window.Index
		}
	}
	return highest + 1
}

// serverBand gives each server a stable position in the remote index range,
// in the order the session first used them.
func (i *Instance) serverBand(serverID string) int {
	seen := make([]string, 0, 4)
	for _, window := range i.FollowedWindows {
		remote := window.ServerID
		if remote == "" {
			continue
		}
		known := false
		for _, existing := range seen {
			if existing == remote {
				known = true
				break
			}
		}
		if !known {
			seen = append(seen, remote)
		}
	}
	for band, existing := range seen {
		if existing == serverID {
			return band
		}
	}
	return len(seen)
}

// sameMachine reports whether two tab descriptors run in the same place.
//
// The tmux window index only identifies a window within one multiplexer, so
// two tabs on different machines can both be window 2 without being the same
// tab. Everything that used to match on the index alone has to ask this as
// well, or deleting a tab on a server takes the local tab of the same number
// with it.
func (fw FollowedWindow) sameMachine(sessionServerID, otherServerID string) bool {
	return fw.RunsOn(sessionServerID) == otherServerID
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
	if i.MainWindowName != "" {
		return i.MainWindowName
	}
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
	return generateUniqueID(name, agent, nil)
}

// generateUniqueID builds a session ID that is not in taken. The ID becomes the
// tmux session name, so a collision means two sessions driving one tmux
// session. The clock does not prevent it: on Windows its resolution is coarse
// enough that consecutive readings are identical, so retrying on a collision
// without advancing the stamp would spin forever.
func generateUniqueID(name string, agent AgentType, taken map[string]bool) string {
	sanitized := sanitizeSessionName(name)
	agentStr := string(agent)
	if agentStr == "" {
		agentStr = "claude"
	}
	stamp := nowUnixNano()
	for {
		id := fmt.Sprintf("asm_%s_%s_%d", agentStr, sanitized, stamp)
		if !taken[id] {
			return id
		}
		stamp++
	}
}

func (i *Instance) TmuxSessionName() string {
	return i.ID
}

// captureTargetCache caches GUI session lookups per instance+window to avoid
// running `tmux list-sessions` on every capture (called multiple times per poll cycle).
var captureTargetCache sync.Map // map[string]captureTargetEntry

var captureTargetCachePrune struct {
	sync.Mutex
	last time.Time
}

type captureTargetEntry struct {
	target  string
	expires time.Time
}

const captureTargetCacheTTL = 2 * time.Second

func pruneCaptureTargetCache(now time.Time) {
	captureTargetCachePrune.Lock()
	if !captureTargetCachePrune.last.IsZero() && now.Sub(captureTargetCachePrune.last) < captureTargetCacheTTL {
		captureTargetCachePrune.Unlock()
		return
	}
	captureTargetCachePrune.last = now
	captureTargetCachePrune.Unlock()
	captureTargetCache.Range(func(key, value interface{}) bool {
		entry, ok := value.(captureTargetEntry)
		if !ok || !now.Before(entry.expires) {
			captureTargetCache.Delete(key)
		}
		return true
	})
}

// GetCaptureTarget returns the best tmux target for capture-pane for a given window.
// It prefers an attached GUI session (created by the WebSocket terminal) because those
// have the up-to-date pane content. Falls back to the base session if no GUI session is found.
func (i *Instance) GetCaptureTarget(windowIdx int) string {
	return i.GetCaptureTargetContext(context.Background(), windowIdx)
}

// GetCaptureTargetContext is GetCaptureTarget with cancellation for background
// pollers. Every multiplexer probe also has the package-wide command timeout.
func (i *Instance) GetCaptureTargetContext(ctx context.Context, windowIdx int) string {
	baseName := i.TmuxSessionName()
	cacheKey := fmt.Sprintf("%s:%d", baseName, windowIdx)
	now := time.Now()
	pruneCaptureTargetCache(now)

	// Check cache first
	if cached, ok := captureTargetCache.Load(cacheKey); ok {
		entry := cached.(captureTargetEntry)
		if now.Before(entry.expires) {
			return entry.target
		}
	}

	baseTarget := cacheKey

	// A tab on a server is captured from its own window directly.
	//
	// The mirror sessions this looks for are a local device: they are created
	// by the terminal handler on this computer, and a remote tab is attached
	// over SSH with no mirror at all. Searching for one on the server finds
	// nothing and costs a round trip per poll.
	if i.serverForWindow(windowIdx) != "" {
		return baseTarget
	}

	// List tmux sessions matching the GUI pattern for this window
	prefix := fmt.Sprintf("%s_gui_%d_", baseName, windowIdx)
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.tmuxOutputContext(commandCtx, "list-sessions", "-F", "#{session_name} #{session_attached}")
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
var startLocks = struct {
	sync.Mutex
	entries map[string]*sessionStartLock
}{entries: make(map[string]*sessionStartLock)}

type sessionStartLock struct {
	mu   sync.Mutex
	refs int
}

func lockSessionStart(name string) func() {
	startLocks.Lock()
	entry := startLocks.entries[name]
	if entry == nil {
		entry = &sessionStartLock{}
		startLocks.entries[name] = entry
	}
	entry.refs++
	startLocks.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		startLocks.Lock()
		entry.refs--
		if entry.refs == 0 && startLocks.entries[name] == entry {
			delete(startLocks.entries, name)
		}
		startLocks.Unlock()
	}
}

// allWindows means "start every tab", the ordinary case.
//
// A named constant rather than a bare -1 because the value travels through
// three functions before it is read, and at the far end "-1" says nothing.
const allWindows = -1

// StartOnlyWindow starts the session but brings back just one tab running.
//
// A stopped session has no multiplexer session to respawn a pane into, so the
// "start this tab" offer could not be served by RestartWindow: it answered
// "instance not running" and the dialog's own option failed every time. What
// it means instead is a full start where everything except the chosen tab —
// the session's own agent included — comes back parked.
func (i *Instance) StartOnlyWindow(windowIdx int) error {
	return i.startWithResume("", windowIdx)
}

func (i *Instance) StartWithResume(resumeID string) error {
	return i.startWithResume(resumeID, allWindows)
}

// scrollbackLines is how much history a pane keeps.
//
// Well above tmux's default of 2000 and the 10000 a stock configuration tends
// to set: an agent working through a long task can fill that in one run, and
// the point of scrolling back is to see what it did.
const scrollbackLines = "50000"

func (i *Instance) startWithResume(resumeID string, onlyWindowIdx int) error {
	// Nothing below can work without the multiplexer, and every command that
	// tries fails on its own terms — "exec: no such file", repeated once per
	// call. Said plainly once, before any of them run, and with how to install
	// it, since a user meeting this has no other way to find out.
	//
	// This computer's multiplexer, so only for a session that runs here. A
	// session on a server uses the multiplexer THERE — which the connection
	// test checks, and which is the whole point of running it remotely — and
	// refusing to start it because this machine has no tmux would be refusing
	// over something that is not used.
	if !i.IsRemote() {
		if err := CheckMultiplexer(); err != nil {
			return err
		}
	}

	// Held for the whole start: releasing after the existence check would
	// reopen the very window this closes.
	unlock := lockSessionStart(i.TmuxSessionName())
	defer unlock()

	log.Printf("[StartWithResume] session=%s agent=%s requested_resume=%t saved_resume=%t", i.ID, i.Agent, resumeID != "", i.ResumeSessionID != "")
	effectiveResumeID := resumeID
	if effectiveResumeID == "" {
		effectiveResumeID = i.ResumeSessionID
	}
	if effectiveResumeID != "" && !ResumeIDExistsForDir(i.Agent, effectiveResumeID, i.Path) {
		// An empty argument normally falls back to ResumeSessionID below. Clear
		// both so corrupt or stale persisted input cannot bypass validation.
		log.Printf("[StartWithResume] saved conversation unavailable; starting fresh for session=%s", i.ID)
		resumeID = ""
		i.ResumeSessionID = ""
	}

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
	sessionExists := i.tmuxRun("has-session", "-t", sessionName) == nil
	if !sessionExists && recentlyStarted(sessionName) {
		for attempt := 0; attempt < 5 && !sessionExists; attempt++ {
			time.Sleep(300 * time.Millisecond)
			sessionExists = i.tmuxRun("has-session", "-t", sessionName) == nil
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
				if config.SupportsSessionID && config.SessionIDFlag != "" &&
					!ExtraArgsSetConversation(i.ExtraArgs) {
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
					} else if config.SupportsSessionID && config.SessionIDFlag != "" &&
						!ExtraArgsSetConversation(i.ExtraArgs) {
						// New session with pre-assigned session ID (like VS Code extension)
						newID := uuid.New().String()
						args = append(args, config.SessionIDFlag, newID)
						i.ResumeSessionID = newID
					}
				}
			}

			argv = buildAgentArgv(config.Command, args, i.ExtraArgs)
		}

		// Check if the command exists.
		//
		// Only for a session that runs here: the PATH being searched is this
		// computer's, and a remote session's agent lives on the server. The
		// connection test reports which agents are installed there, which is
		// where that answer belongs.
		if cmdToCheck != "" && !i.IsRemote() {
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
		log.Printf("[StartWithResume] launching session=%s agent=%s argc=%d", sessionName, i.Agent, len(argv))
		// Scrollback, set BEFORE the session exists.
		//
		// A window takes its history limit when it is created, and never looks
		// again — so setting it on the session afterwards, as this did, reached
		// every window except the one the agent runs in. Measured: the option
		// applied to a live session leaves window 0 at the default 10000 while
		// a window made afterwards gets the new value.
		//
		// Set on the multiplexer server so the session's first window is born
		// with it. Failure is ignored: a smaller scrollback is a worse terminal,
		// not a broken one.
		_ = i.tmuxRun("set-option", "-g", "history-limit", scrollbackLines)

		tmuxArgs := append([]string{"new-session", "-d", "-s", sessionName, "-c", i.Path}, argv...)
		// Recorded before the command is issued: the mark is what tells a
		// subsequent start to wait for a slow registration rather than create a
		// duplicate, and the window it guards opens the moment we ask.
		markStarted(sessionName)

		if i.IsRemote() {
			// The helper runs remote commands through a login shell, which sets
			// TERM for itself; there is no local environment to pass along.
			if err := i.tmuxRun(tmuxArgs...); err != nil {
				return fmt.Errorf("failed to create tmux session on %s: %w",
					i.exec().Describe(), err)
			}
		} else {
			cmd := TmuxCommand(tmuxArgs...)
			// Pin a sane TERM for the session's child processes. Launched from a
			// desktop menu / KRunner the app inherits TERM=dumb (or empty), which
			// would propagate into the agent running inside tmux.
			cmd.Env = append(os.Environ(), "TERM=xterm-256color")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to create tmux session: %w", err)
			}
		}

		// Wait for session to be ready
		for j := 0; j < 20; j++ {
			if i.tmuxRun("has-session", "-t", sessionName) == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Keep windows alive when their process exits, so a tab whose shell has
		// been closed shows as dead rather than vanishing.
		//
		// Set here AND on every window as it is created, because a window
		// opened later does not inherit this one.
		//
		// Measured on tmux 3.4: setting it session-wide and then opening a
		// window leaves that window without it, and a shell exiting there takes
		// the whole window with it — which is what made Ctrl+D close a terminal
		// tab outright instead of leaving it dead.
		//
		// -w is explicit rather than necessary: tmux routes a window option to
		// the window even without it. Kept because it says which scope is meant,
		// and a reader should not have to know tmux's routing rules to tell.
		i.tmuxRun("set-option", "-w", "-t", sessionName, "remain-on-exit", "on")

		// Also on the session, for the windows it will gain later: the global
		// default above covers the first one, this covers tabs added after the
		// server-wide value may have been changed by something else.
		i.tmuxRun("set-option", "-t", sessionName, "history-limit", scrollbackLines)
		i.tmuxRun("set-option", "-t", sessionName, "mouse", "on")

		// Hide tmux status bar (not needed in GUI, wastes a row)
		i.tmuxRun("set-option", "-t", sessionName, "status", "off")

		// Use latest client size and aggressive resize for proper terminal following
		i.tmuxRun("set-option", "-t", sessionName, "window-size", "latest")
		i.tmuxRun("set-option", "-t", sessionName, "aggressive-resize", "on")

		// xterm-keys used to be set here "for Shift+PageUp/Down support". It was
		// doing nothing on two counts: tmux removed the option in 3.3 (it is not
		// in the 3.4 man page, and setting it is accepted in silence), and -t
		// alongside -g is ignored anyway — the global scope wins. Shift+PageUp
		// works through the root-table bindings just below, which is what
		// actually implements it.
		//
		// -g here is deliberate and unavoidable: terminal-overrides is a server
		// option, so this DOES affect other tmux sessions on the same server.
		// -ga appends rather than replaces, which is what keeps that tolerable.
		i.tmuxRun("set-option", "-ga", "terminal-overrides", ",xterm*:smcup@:rmcup@")

		// Bind Shift+PageUp/Down for scrolling in copy mode (conditional - only in
		// asmgr-* sessions). The condition is a native tmux format, not an
		// `if-shell` pipeline: the Windows multiplexer is psmux and a native install
		// has neither a `tmux` executable nor grep/POSIX shell syntax.
		i.tmuxRun(asmgrSessionBinding("root", "S-PageUp", "copy-mode -eu")...)
		i.tmuxRun(asmgrSessionBinding("root", "S-PageDown", "send-keys PageDown")...)
		i.tmuxRun(asmgrSessionBinding("copy-mode-vi", "S-PageUp", "send-keys -X page-up")...)
		i.tmuxRun(asmgrSessionBinding("copy-mode-vi", "S-PageDown", "send-keys -X page-down")...)

		// Bind Ctrl+Y for yolo mode toggle (conditional - only in asmgr-* sessions)
		// tmux/psmux expands the two formats before invoking the external CLI, so
		// this command also needs no shell-specific command substitution.
		i.tmuxRun(asmgrSessionBinding("", "C-y", `run-shell "asmgr yolo \"#{session_name}\" \"#{window_index}\""`)...)

		// Ctrl+q will be set up with resize in UpdateDetachBinding

		// Persist the main window identity on the tmux window object itself.
		// Unlike its numeric index, this marker survives move-window/renumbering.
		if mainWindowIdx, ok := soleTmuxWindowIndex(sessionName); ok {
			mainTarget := fmt.Sprintf("%s:%d", sessionName, mainWindowIdx)
			i.tmuxRun("set-option", "-w", "-t", mainTarget, "@asmgr_main", "1")
			i.tmuxRun("rename-window", "-t", mainTarget, i.WindowName())
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
	i.restoreFollowedWindows(onlyWindowIdx)

	// "Only this tab" for a followed window means the session's own agent must
	// not run either. The window has to exist first — it is what the session
	// was created around — so it is started and then stopped, the same way
	// StopWindow leaves a main window behind as a dead pane.
	if onlyWindowIdx != allWindows && !i.isMainWindowIndex(onlyWindowIdx) {
		if err := i.stopMainWindowAfterStart(); err != nil {
			log.Printf("[StartWithResume] session=%s could not park the main window: %v", i.ID, err)
		}
	}

	// A fresh Codex process usually has its rollout open by this point. Save
	// the generated ID in the same storage update as the start operation;
	// sidebar polling and stop/shutdown capture remain retries for slower starts.
	i.CaptureCodexResumeIDs()

	return nil
}

// isMainWindowIndex reports whether windowIdx is the session's own window
// rather than one of its tabs.
//
// Answered from the stored tabs, not from tmux: this runs during a start, when
// restoreFollowedWindows has just renumbered the windows, and the question is
// about the index the caller asked for — which came from the UI before any of
// that happened.
func (i *Instance) isMainWindowIndex(windowIdx int) bool {
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			return false
		}
	}
	return true
}

// stopMainWindowAfterStart parks the session's own agent, leaving its window
// behind as a dead pane.
//
// The same two commands StopWindow uses for a main window: the session cannot
// be created without its first window, so "start only this tab" has to start
// it and then stop it rather than skip it.
// parkMainWindowContext stops the session's own agent and leaves its window
// behind as a dead pane.
//
// The session cannot exist without its first window, so this is what "the
// agent is not running" looks like for the main one: the window stays, its
// process does not. Both callers — stopping the main tab, and starting a
// session with only another tab live — need exactly this, and a second copy of
// the two commands is how the two drift apart.
func (i *Instance) parkMainWindowContext(ctx context.Context, sessionName string, mainWindowIdx int) error {
	target := fmt.Sprintf("%s:%d", sessionName, mainWindowIdx)
	// Keep the window alive as a dead pane
	if err := i.tmuxRunContext(ctx, "set-option", "-w", "-t", target, "remain-on-exit", "on"); err != nil {
		return fmt.Errorf("failed to prepare main window for stop: %w", err)
	}
	// Kill the agent and replace with an immediately-exiting command
	if err := i.tmuxRunContext(ctx, respawnPaneArgs(nil, target, "exit", "0")...); err != nil {
		return fmt.Errorf("failed to stop main window: %w", err)
	}
	i.MainWindowStopped = true
	return nil
}

func (i *Instance) stopMainWindowAfterStart() error {
	mainWindowIdx, ok := i.getMainWindowIndex()
	if !ok {
		// Without knowing which window is the main one, stopping the wrong one
		// would take a tab down. Leaving the agent running is the safer miss.
		return fmt.Errorf("cannot identify main tmux window for session %s", i.TmuxSessionName())
	}
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.parkMainWindowContext(ctx, i.TmuxSessionName(), mainWindowIdx)
}

// saveBaseCommit saves the current git HEAD commit SHA for diff tracking
func (i *Instance) saveBaseCommit() {
	// Only save if not already set (preserve original base on restart)
	if i.BaseCommitSHA != "" {
		return
	}

	// Check if path is a git repo and get HEAD commit
	cmd := GitCommand("-C", i.gitDir(), "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// Not a git repo or error - no diff available
		return
	}

	i.BaseCommitSHA = strings.TrimSpace(string(output))
}

// respawnPaneArgs builds a respawn-pane command line that actually runs what it
// is given.
//
// The command must follow a "--" separator. tmux accepts it either way, but
// psmux — the Windows multiplexer — documents respawn-pane as "restart the
// pane's shell" and silently drops a command passed without it: the pane came
// back as a bare PowerShell prompt, whatever agent was asked for. That is why
// starting a whole session worked while restarting one of its tabs did not —
// a session's tabs are created with new-window, which takes its command
// directly, and only a restart goes through respawn-pane.
//
// The same omission broke stopping a tab: "exit 0" was dropped too, so instead
// of a dead pane the tab got a fresh shell and went on looking alive.
func respawnPaneArgs(extraFlags []string, target string, command ...string) []string {
	args := make([]string, 0, 6+len(extraFlags)+len(command))
	args = append(args, "respawn-pane", "-k")
	args = append(args, extraFlags...)
	args = append(args, "-t", target, "--")
	return append(args, command...)
}

// newTmuxWindowArgs builds the argument list for creating a window.
//
// Split from running it so a session on a server can send the same arguments
// through its own executor: the arguments are identical either way, and only
// the destination differs.
func newTmuxWindowArgs(sessionName, tabDir, name string, detached bool, argv []string) []string {
	tmuxArgs := []string{"new-window"}
	if detached {
		tmuxArgs = append(tmuxArgs, "-d")
	}
	tmuxArgs = append(tmuxArgs, "-P", "-F", "#{window_index}", "-t", sessionName, "-c", tabDir, "-n", name)
	return append(tmuxArgs, argv...)
}

// newWindowOutput creates a window wherever this session lives and returns the
// index tmux assigned it.
func (i *Instance) newWindowOutput(sessionName, tabDir, name string, detached bool, argv []string) ([]byte, error) {
	return i.tmuxOutput(newTmuxWindowArgs(sessionName, tabDir, name, detached, argv)...)
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
	return tmuxWindowExistsContext(context.Background(), sessionName, windowIdx)
}

func tmuxWindowExistsContext(ctx context.Context, sessionName string, windowIdx int) bool {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := TmuxCommandContext(commandCtx, "list-windows", "-t", sessionName, "-F", "#{window_index}").Output()
	if err != nil {
		return false
	}
	return tmuxWindowIndexListed(output, windowIdx)
}

// windowExistsContext asks the machine a tab lives on whether its window is
// still there.
//
// The package-level tmuxWindowExistsContext always asks this computer, which
// is right for a session that runs here and wrong for a tab on a server: the
// window genuinely is not here, so every stop, restart and delete of such a
// tab would refuse with "not found".
func (i *Instance) windowExistsContext(ctx context.Context, sessionName string, windowIdx int) bool {
	serverID := i.serverForWindow(windowIdx)
	if serverID == "" {
		return tmuxWindowExistsContext(ctx, sessionName, windowIdx)
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.execOn(serverID).Output(commandCtx,
		"list-windows", "-t", sessionName, "-F", "#{window_index}")
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
// restoreFollowedWindows recreates the session's tabs.
//
// onlyWindowIdx names the one tab that should come back running; every other
// is brought back as a stopped placeholder. Pass allWindows to start them all,
// which is what an ordinary session start does.
func (i *Instance) restoreFollowedWindows(onlyWindowIdx int) {
	if len(i.FollowedWindows) == 0 {
		return
	}

	sessionName := i.TmuxSessionName()

	// Where the chosen tab ended up, for the final select-window.
	chosenNewIdx := 0
	chosenFound := false

	// Store old followed windows and clear the list (will be repopulated)
	oldWindows := i.FollowedWindows
	i.FollowedWindows = nil

	for _, fw := range oldWindows {
		// "Start only this tab" is expressed by marking the others stopped
		// before the loop decides what to launch: the stopped branch below
		// already knows how to bring a tab back as a dead pane, and reusing it
		// keeps one description of what a stopped tab looks like.
		originalIdx := fw.Index
		if onlyWindowIdx != allWindows && originalIdx != onlyWindowIdx {
			fw.Stopped = true
		}
		var windowArgs []string
		resumeID := fw.ResumeSessionID
		tabDir := fw.WorkDir
		if tabDir == "" {
			tabDir = i.Path
		}
		// Drop the saved resume ID if it no longer exists on disk so the
		// tab boots fresh instead of dying with "No conversation found".
		// Asked on the machine the tab runs on. The local check reads this
		// computer's disk, which for a tab on a server is the wrong one in
		// both directions — and a conversation that does not exist there is
		// exactly what makes the agent answer "no conversation found".
		tabServerID := fw.RunsOn(i.ServerID)
		conversationAvailable := func(id string) bool {
			if tabServerID != "" {
				return i.resumeIDExistsOnServer(tabServerID, fw.Agent, id)
			}
			return ResumeIDExistsForDir(fw.Agent, id, tabDir)
		}
		if resumeID != "" && !conversationAvailable(resumeID) {
			log.Printf("[restoreFollowedWindows] saved conversation unavailable for agent=%s tab=%q — starting fresh", fw.Agent, fw.Name)
			resumeID = ""
			fw.ResumeSessionID = ""
		}
		if fw.Agent == AgentClaude && resumeID != "" {
			ReleaseClaudeBackgroundAgent(resumeID)
		}

		if fw.Stopped {
			// A restored trash item is deliberately brought back as a stopped
			// placeholder. Never launch an agent merely because its parent
			// session was started; the user can explicitly start this tab.
			windowArgs = newTmuxWindowArgs(sessionName, tabDir, fw.Name, true, nil)
		} else if fw.Agent == AgentTerminal {
			// Terminal window - just create empty shell
			windowArgs = newTmuxWindowArgs(sessionName, tabDir, fw.Name, false, nil)
		} else {
			// Agent window - build agent command (argv form, no shell)
			config := AgentConfigs[fw.Agent]
			var argv []string

			if fw.Agent == AgentCustom {
				argv = customCommandArgv(fw.CustomCommand)
			} else {
				args := []string{}
				autoYes := fw.AutoYes || i.AutoYes
				// Same as on restart: an agent that refuses this flag as root
				// would exit at once, and the tab would come back dead.
				if autoYes && i.autoYesRefusedAsRoot(fw.RunsOn(i.ServerID), config) {
					log.Printf("[restoreFollowedWindows] %s refuses %s as root on the server; starting without it",
						config.Command, config.AutoYesFlag)
					autoYes = false
				}

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
					} else if resumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" &&
						!ExtraArgsSetConversation(fw.ExtraArgs) {
						resumeID = uuid.New().String()
						args = append(args, config.SessionIDFlag, resumeID)
						log.Printf("[restoreFollowedWindows] generated a conversation ID for tab %q agent=%s", fw.Name, fw.Agent)
					}
				}
				argv = buildAgentArgv(config.Command, args, fw.ExtraArgs)
			}

			// Create new window with the agent command as separate argv
			// elements (tmux execs directly, no `sh -c`).
			windowArgs = newTmuxWindowArgs(sessionName, tabDir, fw.Name, false, argv)
		}

		output, err := i.tmuxOutput(windowArgs...)
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
		i.tmuxRun("set-option", "-w", "-t", target, "remain-on-exit", "on")
		// Disable automatic-rename so the window keeps the user-specified name
		i.tmuxRun("set-option", "-w", "-t", target, "automatic-rename", "off")
		if fw.Stopped {
			_ = i.tmuxRun(respawnPaneArgs(nil, target, "exit", "0")...)
		}

		// Remember where the chosen tab landed. tmux renumbers the windows as
		// they are recreated, so the index the user picked is not the index the
		// tab ends up on, and the view has to follow the tab rather than the
		// number.
		if onlyWindowIdx != allWindows && originalIdx == onlyWindowIdx {
			chosenNewIdx = newIdx
			chosenFound = true
		}

		// Re-add to followed windows with updated index (preserve all fields)
		restored := fw
		restored.Index = newIdx
		restored.ResumeSessionID = resumeID
		i.FollowedWindows = append(i.FollowedWindows, restored)
	}

	// Clear TabOrder since window indices changed after restart
	i.TabOrder = nil

	// Switch to the window the user will be looking at. Normally that is the
	// session's own agent; with "only this tab" it is the tab they picked —
	// selecting the main window there would land them on the pane that is
	// about to be parked, which reads as "it started everything but the one I
	// asked for".
	if chosenFound {
		i.tmuxRun("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, chosenNewIdx))
		return
	}
	if mainWindowIdx, ok := i.getMainWindowIndex(); ok {
		i.tmuxRun("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, mainWindowIdx))
	}
}

func (i *Instance) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.StopContext(ctx)
}

// StopContext tears down the multiplexer session within one caller-owned
// deadline. Stop runs while App holds the active project's mutation/read lock;
// an unresponsive tmux must not retain that lock forever and prevent project
// switch or application shutdown.
func (i *Instance) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
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

	// Kill the view sessions first, wherever they are.
	//
	// Two shapes, because there are two kinds: the local mirrors created by
	// the terminal handler (<session>_gui_<N>_<millis>) and the ones a remote
	// attach makes on the server (asmgr_view_<session>_<N>). Left behind, each
	// is a session that outlives the thing it was a view of — visible in
	// `tmux ls` on the server for as long as the machine stays up.
	// The session's own machine first — which is this computer for a local
	// session, and the server for one that runs there.
	i.killViewSessionsOn(ctx, i.ServerID, sessionName)
	for _, serverID := range i.tabServerIDs() {
		i.killViewSessionsOn(ctx, serverID, sessionName)
	}

	// Kill the base tmux session
	if err := i.tmuxRunContext(ctx, "kill-session", "-t", sessionName); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("timed out stopping tmux session: %w", ctxErr)
		}
		// If the base session is already gone (killed by group cascade), that's OK
		if i.tmuxRunContext(ctx, "has-session", "-t", sessionName) == nil {
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

// killViewSessionsOn removes the view sessions belonging to this session on
// one machine.
//
// A view is a session that exists only to show one window: killing it leaves
// the window and whatever runs in it untouched, which is why this can be done
// without care for what the user has open.
func (i *Instance) killViewSessionsOn(ctx context.Context, serverID, sessionName string) {
	executor := i.execOn(serverID)
	if serverID == i.ServerID {
		// The session's own route, which is the local executor for a local
		// session and the server's for a remote one.
		executor = i.exec()
	}
	out, err := executor.Output(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return
	}
	localPrefix := sessionName + "_gui_"
	remotePrefix := remoteViewPrefix(sessionName)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, localPrefix) || strings.HasPrefix(name, remotePrefix) {
			_ = executor.Run(ctx, "kill-session", "-t", name)
		}
	}
}

// remoteViewPrefix is what a remote attach names its view sessions.
//
// Kept beside the multiplexer code rather than in the remote package so the
// two cannot drift: one side creates these names, the other has to recognise
// them to clean them up.
func remoteViewPrefix(sessionName string) string {
	return "asmgr_view_" + sanitiseViewName(sessionName)
}

// remoteViewName is the view belonging to one window, matching what a remote
// attach creates for it.
func remoteViewName(sessionName string, windowIdx int) string {
	return fmt.Sprintf("%s_%d", remoteViewPrefix(sessionName), windowIdx)
}

// sanitiseViewName removes the characters a multiplexer reads as part of a
// target, so a session name can be used inside another session's name.
func sanitiseViewName(name string) string {
	return strings.NewReplacer(":", "_", ".", "_", "$", "_").Replace(name)
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
	return i.tmuxRun("new-window", "-t", sessionName, "-c", i.Path)
}

// NewWindowWithName creates a new tmux window with a specific name
func (i *Instance) NewWindowWithName(name string, workDir string) (int, error) {
	return i.NewWindowWithNameOn("", name, workDir)
}

// NewWindowWithNameOn creates a terminal tab on a given machine.
//
// serverID empty means the session's own machine. A terminal is the tab type
// that works on any server at all, including one with no agent installed, so
// this is the path most remote tabs take.
func (i *Instance) NewWindowWithNameOn(serverID string, name string, workDir string) (int, error) {
	if workDir == "" && serverID == "" {
		workDir = i.Path
	}
	if workDir == "" {
		return -1, fmt.Errorf("error.tabNeedsWorkDirOnServer")
	}
	if i.Status != StatusRunning {
		return -1, fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	remote := serverID != "" && serverID != i.ServerID
	if remote {
		if err := i.ensureRemoteSessionFor(serverID, workDir); err != nil {
			return -1, err
		}
	}
	createTarget := sessionName
	if remote {
		createTarget = fmt.Sprintf("%s:%d", sessionName, i.nextRemoteWindowIndex(serverID))
	}
	output, err := i.tmuxOutputOn(serverID,
		newTmuxWindowArgs(createTarget, workDir, name, false, nil)...)
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
		Index:    newIdx,
		Agent:    AgentTerminal,
		Name:     name,
		ServerID: serverID,
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits (shows as stopped)
	target := fmt.Sprintf("%s:%d", sessionName, newIdx)
	i.tmuxRunOn(serverID, "set-option", "-w", "-t", target, "remain-on-exit", "on")
	// Disable automatic-rename so the window keeps the user-specified name
	i.tmuxRunOn(serverID, "set-option", "-w", "-t", target, "automatic-rename", "off")

	return newIdx, nil
}

// StopWindow stops the agent in a specific tmux window.
// For window 0: if there are active followed windows, only kills the main agent
// process (keeps session alive). Otherwise kills the entire tmux session.
// For followed windows: kills the tmux window and marks the tab as stopped.
func (i *Instance) StopWindow(windowIdx int) error {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.StopWindowContext(ctx, windowIdx)
}

// StopWindowContext is the cancellable form used by lifecycle-sensitive
// callers and regression tests. It shares one deadline across window lookup,
// existence validation and the external stop operation.
func (i *Instance) StopWindowContext(ctx context.Context, windowIdx int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Capture before respawn-pane terminates the agent process.
	i.CaptureCodexResumeIDs()

	// And before the pane is gone, so a terminal tab restarts where it was
	// left rather than back at the session root.
	i.CaptureTerminalWorkingDir(windowIdx)

	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	mainWindowIdx, ok := i.getMainWindowIndexContext(ctx)
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", sessionName)
	}
	if !i.windowExistsContext(ctx, sessionName, windowIdx) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("timed out locating tmux window: %w", ctxErr)
		}
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
			return i.StopContext(ctx)
		}

		// Has active followed windows - stop just the main agent process
		return i.parkMainWindowContext(ctx, sessionName, mainWindowIdx)
	}

	// Followed window: stop the process but keep the window (dead pane)
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
	err := i.execOn(i.serverForWindow(windowIdx)).Run(ctx,
		respawnPaneArgs(nil, target, "exit", "0")...)
	if err != nil {
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
	log.Printf("[RestartWindow] session=%s windowIdx=%d requested_resume=%t saved_resume=%t agent=%s", i.ID, windowIdx, resumeID != "", i.ResumeSessionID != "", i.Agent)

	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	mainWindowIdx, ok := i.getMainWindowIndex()
	if !ok {
		return fmt.Errorf("cannot identify main tmux window for session %s", sessionName)
	}
	// A window that is gone is recreated rather than refused.
	//
	// The tab's descriptor still holds everything needed to build it — the
	// agent, the arguments, the directory, the conversation to resume — so
	// there is nothing to gain by telling the user the window is missing and
	// leaving them to delete a tab they wanted. A window can go missing for
	// ordinary reasons: killed on the server, or started before the
	// multiplexer was told to keep dead panes.
	//
	// Only followed tabs. The session's own main window going missing means
	// something else has happened to the session, and recreating it here would
	// paper over that.
	windowMissing := !i.windowExistsContext(context.Background(), sessionName, windowIdx)
	if windowMissing && windowIdx == mainWindowIdx {
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
			args := respawnPaneArgs(restartDirArgs(i.Path), target, shell)
			if err := i.tmuxRun(args...); err != nil {
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
		if resumeID != "" && !ResumeIDExistsForDir(i.Agent, resumeID, i.Path) {
			log.Printf("[RestartWindow] saved main conversation unavailable; starting fresh for session=%s", i.ID)
			resumeID = ""
			i.ResumeSessionID = ""
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
			} else if resumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" &&
				!ExtraArgsSetConversation(i.ExtraArgs) {
				// No resume ID — generate a fresh --session-id so the agent doesn't
				// prompt for resume and we can track the session for future restarts
				newID := uuid.New().String()
				args = append(args, config.SessionIDFlag, newID)
				i.ResumeSessionID = newID
				log.Printf("[RestartWindow] generated a new conversation ID for main window of session=%s", i.ID)
			}
		}
		argv := buildAgentArgv(config.Command, args, i.ExtraArgs)
		log.Printf("[RestartWindow] launching main window session=%s agent=%s argc=%d", i.ID, i.Agent, len(argv))
		tmuxArgs := respawnPaneArgs(nil, target, argv...)
		if err := i.tmuxRun(tmuxArgs...); err != nil {
			return fmt.Errorf("failed to restart main window: %w", err)
		}
		i.MainWindowStopped = false
		i.CaptureCodexResumeIDs()
		return nil
	}

	// Followed window: find the agent config and restart
	fwSliceIdx, collapseDuplicates, err := selectFollowedWindowForRestart(
		i.FollowedWindows, windowIdx, i.ServerID, i.serverForWindow(windowIdx))
	if err != nil {
		return err
	}
	if fwSliceIdx < 0 {
		log.Printf("[RestartWindow] window %d not found in followedWindows (count=%d)", windowIdx, len(i.FollowedWindows))
		for _, w := range i.FollowedWindows {
			log.Printf("[RestartWindow]   fw: index=%d agent=%s name=%q has_resume=%t stopped=%v", w.Index, w.Agent, w.Name, w.ResumeSessionID != "", w.Stopped)
		}
		return fmt.Errorf("window %d not found in followed windows", windowIdx)
	}
	fw := &i.FollowedWindows[fwSliceIdx]

	log.Printf("[RestartWindow] found fw: index=%d agent=%s name=%q has_resume=%t stopped=%v", fw.Index, fw.Agent, fw.Name, fw.ResumeSessionID != "", fw.Stopped)

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
		// An agent that refuses its auto-yes flag as root would print the
		// refusal and exit, leaving a pane that died with no explanation left
		// on screen. Starting without the flag is the usable outcome.
		if autoYes && i.autoYesRefusedAsRoot(i.serverForWindow(windowIdx), config) {
			log.Printf("[RestartWindow] %s refuses %s as root on the server; starting without it",
				config.Command, config.AutoYesFlag)
			autoYes = false
		}
		// Use provided resume ID, or saved one from the tab
		tabResumeID := resumeID
		if tabResumeID == "" {
			tabResumeID = fw.ResumeSessionID
		}
		// Same as on start: the conversation has to exist where the tab runs.
		restartServerID := i.serverForWindow(windowIdx)
		conversationAvailable := tabResumeID == ""
		if !conversationAvailable {
			if restartServerID != "" {
				conversationAvailable = i.resumeIDExistsOnServer(restartServerID, fw.Agent, tabResumeID)
			} else {
				conversationAvailable = ResumeIDExistsForDir(fw.Agent, tabResumeID, i.Path)
			}
		}
		if tabResumeID != "" && !conversationAvailable {
			// Named, and with the machine it was looked for on: a conversation
			// dropped here means the tab restarts without its history, which
			// is worth being able to trace afterwards.
			where := "this computer"
			if restartServerID != "" {
				where = "server " + restartServerID
			}
			log.Printf("[RestartWindow] conversation %s not found on %s; starting fresh for session=%s window=%d",
				tabResumeID, where, i.ID, windowIdx)
			tabResumeID = ""
			fw.ResumeSessionID = ""
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
			} else if tabResumeID == "" && config.SupportsSessionID && config.SessionIDFlag != "" &&
				!ExtraArgsSetConversation(fw.ExtraArgs) {
				newID := uuid.New().String()
				args = append(args, config.SessionIDFlag, newID)
				fw.ResumeSessionID = newID
				log.Printf("[RestartWindow] generated a new conversation ID for tab %s/%d", i.ID, fw.Index)
			}
		}
		argv = buildAgentArgv(config.Command, args, fw.ExtraArgs)
	}

	// Ensure we always have an explicit command — respawn-pane without one
	// re-runs the pane's original start command ("exit 0" for stopped tabs)
	if len(argv) == 0 {
		argv = []string{defaultShell()}
	}
	log.Printf("[RestartWindow] launching followed window target=%s agent=%s argc=%d", target, fw.Agent, len(argv))
	// A terminal tab restarts where it was left; an agent tab keeps whatever
	// directory it was configured with, since that is part of what identifies
	// the conversation it resumes.
	var dirFlags []string
	if fw.Agent == AgentTerminal {
		dirFlags = restartDirArgs(fw.WorkDir)
	}
	tabServer := i.serverForWindow(windowIdx)
	if fw.Agent != AgentCustom && fw.Agent != AgentTerminal {
		if err := i.ensureAgentOnServer(tabServer, AgentConfigs[fw.Agent].Command); err != nil {
			return err
		}
	}
	if windowMissing {
		// Recreated at its own index, so everything that addresses this tab by
		// number — the terminal socket, the status poller, the quick-jump list
		// — still points at it afterwards.
		tabDir := fw.WorkDir
		if tabDir == "" {
			tabDir = i.Path
		}
		// The multiplexer session on the server may be gone too — a reboot, or
		// its last window closing — and a window cannot be created inside a
		// session that does not exist. Rebuilding it is the same work as the
		// first tab on that server did.
		if tabServer != "" && tabServer != i.ServerID {
			if err := i.ensureRemoteSessionFor(tabServer, tabDir); err != nil {
				return err
			}
		}
		if _, err := i.tmuxOutputOn(tabServer,
			newTmuxWindowArgs(target, tabDir, fw.Name, false, argv)...); err != nil {
			return fmt.Errorf("failed to recreate window %d: %w", windowIdx, err)
		}
		// The options a tab is created with, which respawn-pane would have
		// left in place.
		_ = i.tmuxRunOn(tabServer, "set-option", "-w", "-t", target, "remain-on-exit", "on")
		_ = i.tmuxRunOn(tabServer, "set-option", "-w", "-t", target, "automatic-rename", "off")
		log.Printf("[RestartWindow] recreated missing window %s", target)
	} else {
		tmuxArgs := respawnPaneArgs(dirFlags, target, argv...)
		if err := i.tmuxRunOn(tabServer, tmuxArgs...); err != nil {
			return fmt.Errorf("failed to restart window %d: %w", windowIdx, err)
		}
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

// RestopWindow compensates a successful RestartWindow whose storage update
// failed. It deliberately does not call StopWindow: for a main-only session
// StopWindow kills the whole tmux session, while the state immediately before
// RestartWindow was a live tmux session containing a dead main pane. Keeping
// the window and replacing its command with an immediately exiting process
// restores that external state without hiding a newly orphaned process.
func (i *Instance) RestopWindow(windowIdx int) error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	tabServer := i.serverForWindow(windowIdx)
	setErr := i.tmuxRunOn(tabServer, "set-option", "-w", "-t", target, "remain-on-exit", "on")
	if setErr != nil {
		return fmt.Errorf("failed to prepare stopped window %s: %w", target, setErr)
	}

	stopErr := i.tmuxRunOn(tabServer, respawnPaneArgs(nil, target, "exit", "0")...)
	if stopErr != nil {
		return fmt.Errorf("failed to restore stopped window %s: %w", target, stopErr)
	}

	isFollowed := false
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == windowIdx {
			i.FollowedWindows[idx].Stopped = true
			isFollowed = true
		}
	}
	if !isFollowed {
		i.MainWindowStopped = true
	}
	return nil
}

// selectFollowedWindowForRestart finds the descriptor for one window on one
// machine.
//
// sessionServerID and serverID together say which machine: only tabs that run
// there are considered, because a window index is unique within a multiplexer
// and not across them. Returned indexes are into the full slice, so the caller
// can keep using them to address the tab it owns.
func selectFollowedWindowForRestart(windows []FollowedWindow, windowIdx int, sessionServerID, serverID string) (sliceIdx int, collapseDuplicates bool, err error) {
	var matches []int
	for idx := range windows {
		if windows[idx].Index == windowIdx && windows[idx].sameMachine(sessionServerID, serverID) {
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
	if err := i.validateWindowDeletion(windowIdx); err != nil {
		return err
	}

	// Capture before kill-window removes the process that owns the rollout FD.
	i.CaptureCodexResumeIDs()

	if err := i.deleteLiveWindow(windowIdx); err != nil {
		return err
	}
	i.removeWindowMetadata(windowIdx)
	return nil
}

// validateWindowDeletion performs the checks that must happen before a
// durable metadata update. TrashTab uses this separately so it can persist the
// recoverable trash snapshot before killing the real tmux window.
func (i *Instance) validateWindowDeletion(windowIdx int) error {
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
	return nil
}

func (i *Instance) deleteLiveWindow(windowIdx int) error {
	if i.Status == StatusRunning {
		sessionName := i.TmuxSessionName()
		target := fmt.Sprintf("%s:%d", sessionName, windowIdx)
		// tmux silently falls back to the current window for a missing numeric
		// target, so check exact membership before kill-window. Otherwise stale
		// metadata could delete the main agent.
		if i.windowExistsContext(context.Background(), sessionName, windowIdx) {
			tabServer := i.serverForWindow(windowIdx)
			// The view of this window goes with it. A view is a session whose
			// only purpose is to show one window; with that window gone it
			// holds nothing but its own placeholder shell, and would sit in
			// `tmux ls` indefinitely.
			if tabServer != "" {
				_ = i.tmuxRunOn(tabServer, "kill-session", "-t",
					remoteViewName(sessionName, windowIdx))
			}
			killErr := i.tmuxRunOn(tabServer, "kill-window", "-t", target)
			if i.windowExistsContext(context.Background(), sessionName, windowIdx) {
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
	return nil
}

func (i *Instance) removeWindowMetadata(windowIdx int) {
	i.removeWindowMetadataOn(windowIdx, i.serverForWindow(windowIdx))
}

// removeWindowMetadataOn drops the descriptors for one window on one machine.
//
// A tmux index identifies exactly one real window *within one multiplexer*.
// Every matching descriptor is removed so older duplicate-index corruption
// cannot leave phantom tabs behind — but only among the tabs that run in the
// same place, because a tab on a server and a tab here can both be window 2
// while being entirely different tabs.
func (i *Instance) removeWindowMetadataOn(windowIdx int, serverID string) {
	filtered := i.FollowedWindows[:0]
	for _, window := range i.FollowedWindows {
		if window.Index != windowIdx || !window.sameMachine(i.ServerID, serverID) {
			filtered = append(filtered, window)
		}
	}
	i.FollowedWindows = filtered

	// Clear TabOrder since window indices changed
	i.TabOrder = nil
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
	if err := i.tmuxRun("kill-window", "-t", target); err != nil {
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
	output, err := i.tmuxOutput("list-windows", "-t", sessionName)
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
	output, err := i.tmuxOutput("display-message", "-t", sessionName, "-p", "#{window_index}")
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
	output, err := i.tmuxOutput("display-message", "-t", sessionName, "-p", "#{window_name}")
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
	return i.tmuxRun("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, index))
}

// NextWindow switches to the next tmux window
func (i *Instance) NextWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	return i.tmuxRun("next-window", "-t", sessionName)
}

// PrevWindow switches to the previous tmux window
func (i *Instance) PrevWindow() error {
	if i.Status != StatusRunning {
		return fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	return i.tmuxRun("previous-window", "-t", sessionName)
}

// RenameWindow renames exactly one window and updates its persisted descriptor.
// Selecting a window and then renaming the session's current window are two
// separate commands; another attached client can change the selection between
// them and make the rename land on the wrong tab.
func (i *Instance) RenameWindow(index int, name string) (string, error) {
	// A stopped session can be renamed too. The name lives in the store, and a
	// stopped session's tabs are listed from there — so refusing here left the
	// tab bar showing a name the user had just replaced, with "instance not
	// running" as the only explanation. tmux is simply skipped: there is no
	// window to rename, and the name is applied to the one that gets created
	// when the session starts again.
	running := i.Status == StatusRunning

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, index)
	oldName := i.WindowName()
	followedIndex := -1
	for idx := range i.FollowedWindows {
		if i.FollowedWindows[idx].Index == index {
			followedIndex = idx
			oldName = i.FollowedWindows[idx].Name
			break
		}
	}
	if followedIndex < 0 {
		// Which window is the main one is answered by tmux, so a stopped session
		// cannot be asked. The tab bar lists a stopped session as its main tab
		// plus its followed windows, and index 0 is what it shows for the main
		// one — so that is what a rename arriving here refers to.
		if running {
			mainIndex, ok := i.getMainWindowIndex()
			if !ok || index != mainIndex {
				return "", fmt.Errorf("tmux window %s is not a tracked tab", target)
			}
		} else if index != 0 {
			return "", fmt.Errorf("window %d is not a tracked tab", index)
		}
	}
	if running {
		if err := i.tmuxRun("rename-window", "-t", target, name); err != nil {
			return "", err
		}
	}
	if followedIndex < 0 {
		i.MainWindowName = name
		return oldName, nil
	}
	i.FollowedWindows[followedIndex].Name = name
	return oldName, nil
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

	// One listing per machine this session reaches.
	//
	// A window index only means something inside one multiplexer, so a session
	// with tabs on a server has its windows split across two of them. Asking
	// only the session's own machine — which is what this did — left every
	// remote tab out of the tab bar entirely.
	windows := i.windowsOn(i.ServerID, i.tabServerIDs())

	// A tab that answered from nowhere is still a tab. Without this a server
	// that is slow, unreachable or merely still connecting would make its tabs
	// vanish from the bar mid-session, which reads as losing work rather than
	// as losing a connection.
	return i.appendMissingTabs(windows)
}

// tabServerIDs lists the servers this session's tabs sit on, other than the
// machine the session itself runs on.
func (i *Instance) tabServerIDs() []string {
	var servers []string
	for _, window := range i.FollowedWindows {
		remote := window.ServerID
		if remote == "" || remote == i.ServerID {
			continue
		}
		known := false
		for _, existing := range servers {
			if existing == remote {
				known = true
				break
			}
		}
		if !known {
			servers = append(servers, remote)
		}
	}
	return servers
}

// windowsOn lists the windows on the session's own machine and on each of the
// given servers.
//
// Asked in parallel, each under its own timeout: the tab bar is drawn from
// this, and one unreachable server must not decide how long the others take.
func (i *Instance) windowsOn(ownServerID string, tabServers []string) []WindowInfo {
	machines := append([]string{ownServerID}, tabServers...)

	lists := make([][]WindowInfo, len(machines))
	var wg sync.WaitGroup
	for at, serverID := range machines {
		wg.Add(1)
		go func(at int, serverID string) {
			defer wg.Done()
			lists[at] = i.listWindowsOn(serverID)
		}(at, serverID)
	}
	wg.Wait()

	var windows []WindowInfo
	for _, list := range lists {
		windows = append(windows, list...)
	}
	return windows
}

// appendMissingTabs adds the tabs that no machine answered for.
//
// They are marked dead, which is how the bar already draws a tab whose pane
// has exited — the tab is there, and selecting it says so, rather than the
// tab disappearing while its work is still running on the far side.
func (i *Instance) appendMissingTabs(windows []WindowInfo) []WindowInfo {
	listed := make(map[int]bool, len(windows))
	for _, window := range windows {
		listed[window.Index] = true
	}
	for _, followed := range i.FollowedWindows {
		if listed[followed.Index] {
			continue
		}
		windows = append(windows, WindowInfo{
			Index:           followed.Index,
			Name:            followed.Name,
			Followed:        true,
			Agent:           followed.Agent,
			Dead:            true,
			TextColor:       followed.TextColor,
			BackgroundColor: followed.BackgroundColor,
		})
	}
	sort.SliceStable(windows, func(left, right int) bool {
		return windows[left].Index < windows[right].Index
	})
	return windows
}

// listWindowsOn reads one machine's windows for this session.
func (i *Instance) listWindowsOn(serverID string) []WindowInfo {
	sessionName := i.TmuxSessionName()
	// Format: index:name:active_flag:pane_dead
	output, err := i.tmuxOutputOn(serverID, "list-windows", "-t", sessionName,
		"-F", "#{window_index}:#{window_name}:#{window_active}:#{pane_dead}")
	if err != nil {
		return nil
	}

	// On a server, only the indexes this session actually has tabs for.
	//
	// The multiplexer session there was created with a placeholder window to
	// keep it alive, and that window belongs to no tab. Listed alongside the
	// local windows it collided with the session's own main window — same
	// index 0, two different machines — and replaced it in the tab bar, which
	// then drew the server's idle shell where the session's agent should be
	// and marked the real local tabs dead.
	var wanted map[int]bool
	if serverID != "" && serverID != i.ServerID {
		wanted = make(map[int]bool, len(i.FollowedWindows))
		for _, followed := range i.FollowedWindows {
			if followed.ServerID == serverID {
				wanted[followed.Index] = true
			}
		}
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

			if wanted != nil && !wanted[idx] {
				continue
			}

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
	return i.NewAgentWindowOn("", name, agent, customCmd, extraArgs, workDir)
}

// NewAgentWindowOn creates an agent tab on a given machine.
//
// serverID empty means the session's own machine, which is what every caller
// wanted before tabs could be placed individually. A tab on a server gets its
// own multiplexer session there — the same name, on a different machine — and
// an index from the remote range so it cannot collide with a local tab.
func (i *Instance) NewAgentWindowOn(serverID string, name string, agent AgentType, customCmd string, extraArgs string, workDir string) (int, error) {
	if workDir == "" && serverID == "" {
		workDir = i.Path
	}
	if workDir == "" {
		// A tab on a server has no reason to default to a path from this
		// computer: it almost certainly does not exist there, and the
		// multiplexer would refuse to create the window at all.
		return -1, fmt.Errorf("error.tabNeedsWorkDirOnServer")
	}
	if i.Status != StatusRunning {
		return -1, fmt.Errorf("instance not running")
	}

	sessionName := i.TmuxSessionName()
	remote := serverID != "" && serverID != i.ServerID
	if remote {
		if err := i.ensureRemoteSessionFor(serverID, workDir); err != nil {
			return -1, err
		}
		// Before the window exists, so a missing agent is reported rather than
		// leaving a tab whose pane died with its explanation erased.
		if agent != AgentCustom && agent != AgentTerminal {
			if err := i.ensureAgentOnServer(serverID, AgentConfigs[agent].Command); err != nil {
				return -1, err
			}
		}
	}

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
		// Use instance's AutoYes setting for the new agent too — unless the
		// agent refuses it as root on the server, in which case passing it
		// would make the tab exit the moment it starts.
		if i.AutoYes && config.SupportsAutoYes && config.AutoYesFlag != "" &&
			!i.autoYesRefusedAsRoot(serverID, config) {
			args = append(args, config.AutoYesFlag)
		}
		// For agents supporting --session-id, pre-assign a session ID
		if config.SupportsSessionID && config.SessionIDFlag != "" &&
			!ExtraArgsSetConversation(extraArgs) {
			generatedSessionID = uuid.New().String()
			args = append(args, config.SessionIDFlag, generatedSessionID)
		}
		argv = buildAgentArgv(config.Command, args, extraArgs)
	}

	// Create new window with the agent command as separate argv elements
	// (tmux execs directly, no `sh -c`).
	target := sessionName
	if remote {
		// Ask for a specific slot, so the index stays unique across the
		// machines this session spans.
		target = fmt.Sprintf("%s:%d", sessionName, i.nextRemoteWindowIndex(serverID))
	}
	output, err := i.tmuxOutputOn(serverID,
		newTmuxWindowArgs(target, workDir, name, false, argv)...)
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
		ServerID:        serverID,
	})

	// Clear TabOrder since a new window was added
	i.TabOrder = nil

	// Set remain-on-exit so window stays open when command exits (shows as stopped)
	windowTarget := fmt.Sprintf("%s:%d", sessionName, newIdx)
	i.tmuxRunOn(serverID, "set-option", "-w", "-t", windowTarget, "remain-on-exit", "on")
	// Disable automatic-rename so the window keeps the user-specified name
	i.tmuxRunOn(serverID, "set-option", "-w", "-t", windowTarget, "automatic-rename", "off")

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
	mainWindowIdx := i.GetMainWindowIndex()
	if windowIdx != mainWindowIdx {
		found := false
		for _, window := range i.FollowedWindows {
			if window.Index == windowIdx {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("window %d not found", windowIdx)
		}
	}
	agent, sessionID := i.conversationInWindow(windowIdx)
	config, ok := AgentConfigs[agent]
	if !ok || config.ForkFlag == "" {
		return "", fmt.Errorf("%s cannot fork a conversation", agent)
	}
	if sessionID == "" {
		return "", fmt.Errorf("no session ID to fork - session may not have started yet")
	}
	log.Printf("[Fork] session=%s window=%d agent=%s branching saved conversation",
		i.ID, windowIdx, agent)
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
	if config.SupportsSessionID && config.SessionIDFlag != "" &&
		!ExtraArgsSetConversation(i.ExtraArgs) {
		forkedID = uuid.New().String()
		args = append(args, config.SessionIDFlag, forkedID)
	}

	// Carry the session's extra arguments, as every other way of starting a
	// Claude tab does. A fork is the same conversation with the same setup, so
	// dropping them here gave the branch a differently-configured agent —
	// ForkToNewSession passes them, and this did not.
	argv := buildAgentArgv(config.Command, args, i.ExtraArgs)

	// Create new window with forked agent (argv form, no shell layer).
	output, err := i.newWindowOutput(sessionName, i.Path, name, false, argv)
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
	i.tmuxRun("set-option", "-w", "-t", target, "remain-on-exit", "on")
	i.tmuxRun("set-option", "-w", "-t", target, "automatic-rename", "off")

	// Codex names its own branch, so the id has to be read back off the running
	// process — as every other way of starting a Codex tab does. Without it the
	// forked tab has nothing to resume from.
	i.CaptureCodexResumeIDs()

	return newIdx, nil
}

func (i *Instance) IsAlive() bool {
	return i.IsAliveContext(context.Background())
}

// IsAliveContext bounds the multiplexer health probe and lets lifecycle-owned
// background work stop promptly during shutdown.
func (i *Instance) IsAliveContext(ctx context.Context) bool {
	sessionName := i.TmuxSessionName()
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	return i.tmuxRunContext(commandCtx, "has-session", "-t", sessionName) == nil
}

// windowAliveContext asks whether the multiplexer holding one tab is running.
//
// The session-wide check asks the machine the SESSION runs on, which for a tab
// on a server is the wrong one: the local multiplexer knows nothing about that
// session, so the check failed and every per-tab reading — the status line,
// the activity dot — was skipped before it began.
func (i *Instance) windowAliveContext(ctx context.Context, windowIdx int) bool {
	serverID := i.serverForWindow(windowIdx)
	if serverID == "" {
		return i.IsAliveContext(ctx)
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	return i.execOn(serverID).Run(commandCtx, "has-session", "-t", i.TmuxSessionName()) == nil
}

// ResizePane resizes the tmux pane to the specified dimensions
func (i *Instance) ResizePane(width, height int) error {
	if !i.IsAlive() {
		return nil
	}
	sessionName := i.TmuxSessionName()
	return i.tmuxRun("resize-window", "-t", sessionName, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
}

// UpdateDetachBinding updates Ctrl+Q to resize to preview size before detaching
func (i *Instance) UpdateDetachBinding(previewWidth, previewHeight int) {
	if !i.IsAlive() {
		return
	}
	// Bind Ctrl+Q: conditional - only in asmgr-* sessions, with resize before
	// detach. Both actions are native multiplexer commands; wrapping them in
	// `run-shell 'tmux ...'` made the binding unusable with psmux on Windows.
	resizeAndDetach := fmt.Sprintf("resize-window -x %d -y %d ; detach-client", previewWidth, previewHeight)
	i.tmuxRun(asmgrSessionBinding("", "C-q", resizeAndDetach)...)
}

// asmgrSessionBinding builds one global key binding that activates only while
// the current client is in an asmgr-owned session. if-shell's -F form evaluates
// a tmux format directly; it does not launch an OS shell. That distinction is
// required on native Windows, where psmux is the multiplexer and grep, /dev/null
// and a second executable named `tmux` are not part of the runtime.
func asmgrSessionBinding(table, key, command string) []string {
	args := []string{"bind-key"}
	if table == "" {
		args = append(args, "-n")
	} else {
		args = append(args, "-T", table)
	}
	return append(args, key, "if-shell", "-F", "#{m/r:^asm_,#{session_name}}", command, "")
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
	output, err := i.tmuxOutput("capture-pane", "-t", sessionName, "-p", "-e", "-J", "-S", startLine)
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
	output, err := i.tmuxOutput("capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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
	return i.GetStatusInfoForWindowContext(context.Background(), windowIdx, agent)
}

// GetStatusInfoForWindowContext is the cancellable form used by the preview
// poller, which shutdown must be able to reap before project teardown.
func (i *Instance) GetStatusInfoForWindowContext(ctx context.Context, windowIdx int, agent AgentType) StatusInfo {
	result := StatusInfo{}
	if !i.windowAliveContext(ctx, windowIdx) {
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

	target := i.GetCaptureTargetContext(ctx, windowIdx)
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	// Through the window's own executor: a tab on a server has its pane
	// there, and capturing from this computer's multiplexer returns nothing —
	// which is why the status line for a remote agent never moved.
	output, err := i.execOn(i.serverForWindow(windowIdx)).Output(commandCtx,
		"capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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
	output, err := i.tmuxOutput("capture-pane", "-t", target, "-p", "-e", "-J", "-S", "-50")
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
	return i.tmuxRun("send-keys", "-t", sessionName, keys)
}

// SendKeysToWindow sends a tmux key name to a specific window of this session.
func (i *Instance) SendKeysToWindow(windowIdx int, keys string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	return i.tmuxRun("send-keys", "-t", target, keys)
}

// SendText sends text literally (not interpreted as key names)
func (i *Instance) SendText(text string) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}

	sessionName := i.TmuxSessionName()
	// Use -l flag to send text literally without interpreting key names
	return i.tmuxRun("send-keys", "-l", "-t", sessionName, text)
}

// SendTextToWindow types text into a specific window, optionally pressing
// Enter afterwards. Sent with -l so the text is taken literally: a saved
// command containing "C-c" or "Enter" is text, not a key name.
func (i *Instance) SendTextToWindow(windowIdx int, text string, pressEnter bool) error {
	if !i.IsAlive() {
		return fmt.Errorf("session not running")
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	if err := i.tmuxRun("send-keys", "-l", "-t", target, text); err != nil {
		return fmt.Errorf("could not send the command: %w", err)
	}
	if !pressEnter {
		return nil
	}
	// Separate call: Enter is a key name, so it must not carry -l.
	return i.tmuxRun("send-keys", "-t", target, "Enter")
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
	return i.SendPromptToWindowWithSubmit(text, windowIdx, true)
}

// SendPromptToWindowWithSubmit types the text and, when submit is true, presses
// Enter after it.
//
// Leaving it unpressed puts the text in the agent's composer without sending
// it, which is what dictation often wants: speak a prompt, read it back, add to
// it, and submit when it says what was meant.
func (i *Instance) SendPromptToWindowWithSubmit(text string, windowIdx int, submit bool) error {
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
		if err := i.tmuxRun("set-buffer", "--", text); err != nil {
			return fmt.Errorf("failed to set tmux buffer: %w", err)
		}
		if err := i.tmuxRun("paste-buffer", "-p", "-t", sessionName); err != nil {
			// Fallback: paste without -p if not supported
			if err2 := i.tmuxRun("paste-buffer", "-t", sessionName); err2 != nil {
				return fmt.Errorf("failed to paste buffer: %w", err2)
			}
		}
	} else {
		// Single-line text: use send-keys -l for simplicity
		if err := i.tmuxRun("send-keys", "-l", "-t", sessionName, text); err != nil {
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
	if !submit {
		return nil
	}
	return i.tmuxRun("send-keys", "-t", sessionName, "Enter")
}

// IsMainWindowDead checks if the main window (0) pane is dead in tmux
func (i *Instance) IsMainWindowDead() bool {
	return i.IsMainWindowDeadContext(context.Background())
}

// IsMainWindowDeadContext bounds both health probes; a wedged multiplexer must
// not hold storage refresh or a caller indefinitely.
func (i *Instance) IsMainWindowDeadContext(ctx context.Context) bool {
	if !i.IsAliveContext(ctx) {
		return false
	}
	target := fmt.Sprintf("%s:0", i.TmuxSessionName())
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.tmuxOutputContext(commandCtx, "list-panes", "-t", target, "-F", "#{pane_dead}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

func (i *Instance) UpdateStatus() {
	i.UpdateStatusContext(context.Background())
}

// UpdateStatusContext is the cancellable form used by background storage
// snapshots.
func (i *Instance) UpdateStatusContext(ctx context.Context) {
	if i.IsAliveContext(ctx) {
		i.Status = StatusRunning
	} else {
		i.Status = StatusStopped
	}
}

// Git diff functions

// GetSessionDiff returns diff since session start (BaseCommitSHA)
// gitDir returns the directory git commands should run in.
func (i *Instance) gitDir() string {
	if i.BrowseRoot != "" {
		return i.BrowseRoot
	}
	return i.Path
}

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
	readTree, cancelReadTree := GitCommandTimed("-C", i.gitDir(), "read-tree", "HEAD")
	defer cancelReadTree()
	readTree.Env = gitEnv
	if err := readTree.Run(); err != nil {
		// An unborn repository has no HEAD yet; start from an empty index.
		readEmpty, cancelReadEmpty := GitCommandTimed("-C", i.gitDir(), "read-tree", "--empty")
		defer cancelReadEmpty()
		readEmpty.Env = gitEnv
		if emptyErr := readEmpty.Run(); emptyErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to prepare temporary git index: %w", err)
		}
	}

	intentToAdd, cancelIntent := GitCommandTimed("-C", i.gitDir(), "add", "-N", ".")
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
	if err := validateBaseCommitRef(baseRef); err != nil {
		stats.Error = err
		return stats
	}

	gitEnv, cleanup, err := i.diffIndexEnv()
	if err != nil {
		stats.Error = err
		return stats
	}
	defer cleanup()

	args := []string{"-C", i.gitDir(), "--no-pager", "diff"}
	if baseRef != "" {
		args = append(args, "--end-of-options", baseRef)
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
	cmd, cancel := GitCommandTimed("-C", i.gitDir(), "rev-parse", "--git-dir")
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
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.getMainWindowIndexContext(ctx)
}

func (i *Instance) getMainWindowIndexContext(ctx context.Context) (int, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if i.Status != StatusRunning {
		return 0, false
	}

	sessionName := i.TmuxSessionName()
	output, err := i.tmuxOutputContext(ctx, "list-windows", "-t", sessionName, "-F", "#{window_index}\t#{@asmgr_main}")
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
		_ = i.tmuxRunContext(ctx, "set-option", "-w", "-t", target, "@asmgr_main", "1")
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
	cmd, cancel := TmuxCommandTimed("list-windows", "-t", sessionName, "-F", "#{window_index}")
	defer cancel()
	output, err := cmd.Output()
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

var recentStartsPrune struct {
	sync.Mutex
	last time.Time
}

// startSettleWindow is how long after a start another start should wait for
// the session to appear instead of assuming it is absent. Comfortably longer
// than the registration delay measured on psmux, and it only ever delays the
// rarer case of restarting a session that really did go away.
const startSettleWindow = 10 * time.Second

func markStarted(sessionName string) {
	now := time.Now()
	pruneRecentStarts(now)
	recentStarts.Store(sessionName, now)
}

func pruneRecentStarts(now time.Time) {
	recentStartsPrune.Lock()
	if !recentStartsPrune.last.IsZero() && now.Sub(recentStartsPrune.last) < startSettleWindow {
		recentStartsPrune.Unlock()
		return
	}
	recentStartsPrune.last = now
	recentStartsPrune.Unlock()
	recentStarts.Range(func(key, value interface{}) bool {
		started, ok := value.(time.Time)
		if !ok || now.Sub(started) > startSettleWindow {
			// Do not delete a fresh replacement published after Range read the
			// old value; losing that mark can re-open the duplicate-start race.
			recentStarts.CompareAndDelete(key, value)
		}
		return true
	})
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
