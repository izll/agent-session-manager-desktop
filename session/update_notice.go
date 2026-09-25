package session

import (
	"regexp"
	"strings"
)

// UpdateKind says what an agent's update notice asks of the user.
type UpdateKind string

const (
	// UpdateAvailable: a newer version exists and has not been installed.
	UpdateAvailable UpdateKind = "available"
	// UpdateInstalledRestart: the new version is installed, and the running
	// agent keeps the old one until it is restarted.
	UpdateInstalledRestart UpdateKind = "installed-restart"
)

// UpdateNotice is an agent's own "there is a newer version" notice, read off
// the bottom of its pane.
type UpdateNotice struct {
	Kind UpdateKind `json:"kind"`
	// Blocking: the agent is sitting on a prompt about the update and does
	// nothing until the user answers it. Such a tab reads as waiting.
	Blocking bool `json:"blocking"`
	// Current and Version are the running and the new version, when the notice
	// names them.
	Current string `json:"current,omitempty"`
	Version string `json:"version,omitempty"`
	// Command is the agent's own command for updating, when its notice names
	// one. Shown to the user, never run.
	Command string `json:"command,omitempty"`
}

// updatePattern recognises one agent's update notice.
type updatePattern struct {
	agents []AgentType
	// match is tried against each line at the bottom of the pane, lower-cased
	// and without colours. Its named groups "current" and "version", when it
	// has them, fill in the notice.
	match *regexp.Regexp
	kind  UpdateKind
	// blocking: the notice is itself a prompt the agent waits on.
	blocking bool
	// blockingWhen: the notice is a prompt only while this matches one of the
	// last few lines — the prompt's own, still unanswered. Without it on
	// screen it is the passing variant of the same headline, or a question
	// already answered.
	blockingWhen *regexp.Regexp
	// belowLastRule: only lines below the pane's last horizontal rule count.
	// Claude draws its footer under the input box, and the transcript above
	// the box can say anything — this table, for one.
	belowLastRule bool
	// command: the update command the notice names.
	command string
}

// updateTailLines is how far up from the bottom of the pane a notice is looked
// for, counted in non-empty lines.
//
// A notice further up has scrolled into the transcript, and may be long dealt
// with. The tallest one, Gemini's box above its input box and footer, ends
// about ten lines from the bottom.
const updateTailLines = 12

// updatePatterns lists every known update notice; adding an agent's notice is
// one entry here. Each entry was seen on a real pane or is quoted from the
// agent's own source or binary — see update_notice_test.go for the samples.
var updatePatterns = []updatePattern{
	// Codex, before its composer starts, waiting for a choice:
	//   ✨ Update available! 0.155.1 -> 0.156.1          (0.155, seen live)
	//   Update available · 0.155.1 → 0.156.1            (update_prompt.rs now)
	//   › 1. Update now (runs `npm install -g @openai/codex`)
	//     2. Skip
	//     3. Skip until next version
	// The same headline in a box in the transcript (history_cell/notices.rs)
	// is only a message: no "skip until next version" under it.
	{
		agents:       []AgentType{AgentCodex},
		match:        regexp.MustCompile(`update available\s*[!·]\s*(?P<current>[0-9](?:[\w.+-]*\w)?)\s*(?:->|→)\s*(?P<version>[0-9](?:[\w.+-]*\w)?)`),
		kind:         UpdateAvailable,
		blockingWhen: regexp.MustCompile(`skip until next version`),
	},
	// Codex, after "Update now", back at the shell (cli/src/main.rs).
	{
		agents: []AgentType{AgentCodex},
		match:  regexp.MustCompile(`update ran successfully! please restart codex`),
		kind:   UpdateInstalledRestart,
	},
	// Claude Code, at the right of its footer (seen live; 2.1.282 binary):
	//   ✔ Update installed · Restart to update
	//   ✓ Update installed via native · Restart to apply
	// A narrow pane cuts it short after "installed".
	{
		agents:        []AgentType{AgentClaude},
		match:         regexp.MustCompile(`[✔✓]\s*update installed\b`),
		kind:          UpdateInstalledRestart,
		belowLastRule: true,
	},
	// Claude Code, when it could not install it itself (2.1.282 binary):
	//   Update available! Run: claude update
	{
		agents:        []AgentType{AgentClaude},
		match:         regexp.MustCompile(`update available! run:`),
		kind:          UpdateAvailable,
		belowLastRule: true,
	},
	// Gemini CLI (packages/cli/src/ui/utils/updateCheck.ts):
	//   Gemini CLI update available! 0.1.0 → 0.2.0
	//   A new version of Gemini CLI is available! 0.1.0 → 0.2.0   (nightly)
	{
		agents: []AgentType{AgentGemini},
		match:  regexp.MustCompile(`(?:gemini cli update available|a new version of gemini cli is available)!\s*(?P<current>[0-9](?:[\w.+-]*\w)?)\s*(?:→|->)\s*(?P<version>[0-9](?:[\w.+-]*\w)?)`),
		kind:   UpdateAvailable,
	},
	// Gemini CLI, after updating itself (packages/cli/src/utils/handleAutoUpdate.ts).
	{
		agents: []AgentType{AgentGemini},
		match:  regexp.MustCompile(`update successful! the new version will be used on your next run`),
		kind:   UpdateInstalledRestart,
	},
	// Aider (aider/versioncheck.py), asking before it installs:
	//   Newer aider version v0.86.1 is available.
	//   Run pip install? (Y)es/(N)o [Yes]:
	// On Windows and in Docker it only prints the command, and does not ask.
	{
		agents:       []AgentType{AgentAider},
		match:        regexp.MustCompile(`newer aider version v?(?P<version>[0-9](?:[\w.+-]*\w)?) is available`),
		kind:         UpdateAvailable,
		blockingWhen: regexp.MustCompile(`run pip install\? \(y\)es/\(n\)o \[yes\]:$`),
	},
	// Aider, after installing it; it exits (aider/versioncheck.py).
	{
		agents: []AgentType{AgentAider},
		match:  regexp.MustCompile(`re-run aider to use new version`),
		kind:   UpdateInstalledRestart,
	},
	// OpenCode's modal dialog, with Skip and Confirm (packages/tui/src/app.tsx):
	//   A new release v1.2.0 is available. Would you like to update now?
	{
		agents:   []AgentType{AgentOpenCode},
		match:    regexp.MustCompile(`a new release v?(?P<version>[0-9](?:[\w.+-]*\w)?) is available`),
		kind:     UpdateAvailable,
		blocking: true,
	},
	// OpenCode, after updating; it exits (packages/tui/src/app.tsx).
	{
		agents: []AgentType{AgentOpenCode},
		match:  regexp.MustCompile(`successfully updated to opencode v?(?P<version>[0-9](?:[\w.+-]*\w)?)`),
		kind:   UpdateInstalledRestart,
	},
	// Cursor updates itself silently. Two traces of it, from its bundle:
	//   2026.09.20-1a2b3c4 (update available — run `agent update`)   (/about)
	//   The installed Cursor Agent files changed while it was running
	//   (usually due to an update). Restart the CLI to fix this.
	{
		agents: []AgentType{AgentCursor},
		match:  regexp.MustCompile(`(?P<version>[0-9](?:[\w.+-]*\w)?)\s*\(update available`),
		kind:   UpdateAvailable,
	},
	{
		agents: []AgentType{AgentCursor},
		match:  regexp.MustCompile(`cursor agent files changed while it was running`),
		kind:   UpdateInstalledRestart,
	},
	// Amazon Q, printed as a shell starts (figterm/src/update.rs; seen live):
	//   A new version of q is available: 2.24.0
	//   Run q update to update to the new version
	{
		agents:  []AgentType{AgentAmazonQ},
		match:   regexp.MustCompile(`a new version of q is available:?\s*v?(?P<version>[0-9](?:[\w.+-]*\w)?)?`),
		kind:    UpdateAvailable,
		command: "q update",
	},
}

// DetectUpdateNotice reads an agent's update notice off the bottom of its pane.
// It returns nil when there is none, and for an agent without a known notice.
func DetectUpdateNotice(agent AgentType, lines []string) *UpdateNotice {
	var tail, footer []string
	for _, p := range updatePatterns {
		if !hasAgent(p.agents, agent) {
			continue
		}
		if tail == nil {
			tail = paneTail(lines, updateTailLines)
			footer = belowLastRule(tail)
		}
		scope := tail
		if p.belowLastRule {
			scope = footer
		}
		for _, line := range scope {
			m := p.match.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			notice := &UpdateNotice{Kind: p.kind, Command: p.command}
			for gi, name := range p.match.SubexpNames() {
				switch name {
				case "current":
					notice.Current = m[gi]
				case "version":
					notice.Version = m[gi]
				}
			}
			notice.Blocking = p.blocking ||
				(p.blockingWhen != nil && anyMatches(tail[:min(len(tail), promptLines)], p.blockingWhen))
			return notice
		}
	}
	return nil
}

func hasAgent(agents []AgentType, agent AgentType) bool {
	for _, a := range agents {
		if a == agent {
			return true
		}
	}
	return false
}

// paneTail returns the last n non-empty lines of a capture, bottom first,
// lower-cased and without colours.
func paneTail(lines []string, n int) []string {
	tail := make([]string, 0, n)
	for j := len(lines) - 1; j >= 0 && len(tail) < n; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if clean == "" {
			continue
		}
		tail = append(tail, strings.ToLower(clean))
	}
	return tail
}

// belowLastRule returns the lines of a bottom-first tail that come below its
// lowest horizontal rule, or none when it has no rule.
func belowLastRule(tail []string) []string {
	for j, line := range tail {
		if strings.HasPrefix(line, "────") {
			return tail[:j]
		}
	}
	return nil
}

// promptLines is how many lines from the bottom a blocking prompt's own text
// is looked for: an unanswered prompt is the last thing on the pane.
const promptLines = 3

func anyMatches(lines []string, re *regexp.Regexp) bool {
	for _, line := range lines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
