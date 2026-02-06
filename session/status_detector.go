package session

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// busyGracePeriod is the duration to keep reporting Busy after the last
// real busy detection. This smooths out brief gaps between phases
// (e.g., thinking ends → tool execution starts).
const busyGracePeriod = 6 * time.Second

// lastBusyTime tracks the last time Busy was detected per target (session:window)
var lastBusyTime sync.Map // map[string]time.Time

// SessionActivity represents the activity state of a session
type SessionActivity int

const (
	ActivityIdle    SessionActivity = iota // No activity, no prompt
	ActivityBusy                           // Agent is working
	ActivityWaiting                        // Agent needs user input/permission
)

// AgentPatterns holds detection patterns for a specific agent
type AgentPatterns struct {
	WaitingPatterns []string // Patterns that indicate waiting for user input
	BusyPatterns    []string // Patterns that indicate agent is working (kept for compatibility)
	Spinners        []string // Spinner characters - primary busy indicator
}

// Default spinner characters (braille dots)
var defaultSpinners = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Extended thinking indicators (static, non-animated)
// These appear at line start during extended thinking: "✽ Thinking… (stats)"
// After completion they show "✻ Cogitated for Xs" (no ellipsis)
var thinkingIndicators = []string{"✽", "✻"}

// Agent-specific patterns
var agentPatterns = map[AgentType]AgentPatterns{
	AgentClaude: {
		WaitingPatterns: []string{
			"do you want to proceed",
			"esc to cancel",
			"allow once",
			"allow always",
			"yes, allow",
			"yes, and always allow",
		},
		Spinners: defaultSpinners,
	},
	AgentGemini: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"waiting for user",
			"do you want to proceed",
			"keep trying",
			"high demand",
		},
		Spinners: append(defaultSpinners, "∴", "∵", "⋮", "⋯", "✦"),
	},
	AgentAider: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"do you want to proceed",
			"waiting for user",
		},
		Spinners: defaultSpinners,
	},
	AgentCodex: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"do you want to proceed",
			"waiting for user",
		},
		Spinners: defaultSpinners,
	},
	AgentAmazonQ: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"do you want to proceed",
			"waiting for user",
		},
		Spinners: defaultSpinners,
	},
	AgentOpenCode: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"do you want to proceed",
			"waiting for user",
		},
		Spinners: defaultSpinners,
	},
	AgentCustom: {
		WaitingPatterns: []string{
			"allow once",
			"allow always",
			"do you want to proceed",
			"waiting for user",
		},
		Spinners: defaultSpinners,
	},
}

// getAgentPatterns returns patterns for the given agent type
func getAgentPatterns(agent AgentType) AgentPatterns {
	if patterns, ok := agentPatterns[agent]; ok {
		return patterns
	}
	// Default to Claude patterns
	return agentPatterns[AgentClaude]
}

// DetectActivity analyzes tmux pane content to determine session activity
// This checks the main agent window (first window, not necessarily index 0)
func (i *Instance) DetectActivity() SessionActivity {
	return i.DetectActivityForWindow(i.GetMainWindowIndex())
}

// DetectActivityForWindow analyzes a specific tmux window to determine activity
func (i *Instance) DetectActivityForWindow(windowIdx int) SessionActivity {
	if !i.IsAlive() {
		return ActivityIdle
	}

	sessionName := i.TmuxSessionName()
	target := fmt.Sprintf("%s:%d", sessionName, windowIdx)

	// Determine agent type for this window
	agent := i.Agent
	if agent == "" {
		agent = AgentClaude
	}
	if windowIdx > 0 {
		for _, fw := range i.FollowedWindows {
			if fw.Index == windowIdx {
				agent = fw.Agent
				break
			}
		}
	}

	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return ActivityIdle
	}

	lines := strings.Split(string(output), "\n")
	patterns := getAgentPatterns(agent)

	var activity SessionActivity
	// Claude uses separator-based waiting detection
	if agent == AgentClaude {
		activity = detectClaudeActivity(lines, patterns, target)
	} else {
		// All other agents use generic detection
		activity = detectGenericActivity(lines, patterns, target)
	}

	// Apply busy grace period: if we detected busy, update the timestamp.
	// If we got idle but were busy recently, keep reporting busy.
	if activity == ActivityBusy {
		lastBusyTime.Store(target, time.Now())
		return ActivityBusy
	}
	if activity == ActivityIdle {
		if lastTime, ok := lastBusyTime.Load(target); ok {
			if time.Since(lastTime.(time.Time)) < busyGracePeriod {
				return ActivityBusy
			}
			// Grace period expired, clean up
			lastBusyTime.Delete(target)
		}
	}
	// Waiting always takes priority, and also clears grace period
	if activity == ActivityWaiting {
		lastBusyTime.Delete(target)
	}

	return activity
}

// DetectAggregatedActivity checks all followed windows and returns highest priority activity
// Priority: Waiting > Busy > Idle
func (i *Instance) DetectAggregatedActivity() SessionActivity {
	if !i.IsAlive() {
		return ActivityIdle
	}

	// Always check the main window (first window, not necessarily 0)
	mainWindowIdx := i.GetMainWindowIndex()
	windowsToCheck := []int{mainWindowIdx}

	// Add followed windows
	for _, fw := range i.FollowedWindows {
		if fw.Index != mainWindowIdx { // main window is already added
			windowsToCheck = append(windowsToCheck, fw.Index)
		}
	}

	highestActivity := ActivityIdle

	for _, winIdx := range windowsToCheck {
		activity := i.DetectActivityForWindow(winIdx)
		// Waiting has highest priority
		if activity == ActivityWaiting {
			return ActivityWaiting
		}
		// Busy is higher than Idle
		if activity == ActivityBusy && highestActivity == ActivityIdle {
			highestActivity = ActivityBusy
		}
	}

	return highestActivity
}

// detectClaudeActivity uses Claude Code's UI structure for waiting detection,
// and spinner animation + extended thinking check for busy detection.
func detectClaudeActivity(lines []string, patterns AgentPatterns, target string) SessionActivity {
	// --- Waiting detection: uses separator structure to avoid scrollback false positives ---
	if checkClaudeWaiting(lines, patterns) {
		return ActivityWaiting
	}

	// --- Busy detection 1: braille spinner is actively animating ---
	if isSpinnerAnimating(lines, patterns.Spinners, 20, target) {
		return ActivityBusy
	}

	// --- Busy detection 2: extended thinking indicator (✽/✻ with …) ---
	if hasActiveThinking(lines, 20) {
		return ActivityBusy
	}

	// --- Busy detection 3: tool execution (⎿ ... ending with …) ---
	if hasActiveToolExecution(lines, 10) {
		return ActivityBusy
	}

	return ActivityIdle
}

// checkClaudeWaiting checks for waiting patterns in Claude's UI structure
// Uses separator lines to identify the input area and permission dialogs
func checkClaudeWaiting(lines []string, patterns AgentPatterns) bool {
	// Find separator line positions
	var separatorIndices []int
	for idx, line := range lines {
		cleanLine := strings.TrimSpace(stripANSIForDetect(line))
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━")
		if sepCount > 20 {
			separatorIndices = append(separatorIndices, idx)
		}
	}

	var checkLines []string

	if len(separatorIndices) >= 2 {
		topSepIdx := separatorIndices[len(separatorIndices)-2]
		bottomSepIdx := separatorIndices[len(separatorIndices)-1]

		// Lines between separators (input area)
		for idx := topSepIdx + 1; idx < bottomSepIdx; idx++ {
			cleanLine := strings.TrimSpace(stripANSIForDetect(lines[idx]))
			if cleanLine != "" {
				checkLines = append(checkLines, cleanLine)
			}
		}

		// Lines below bottom separator (permission buttons)
		for idx := bottomSepIdx + 1; idx < len(lines); idx++ {
			cleanLine := strings.TrimSpace(stripANSIForDetect(lines[idx]))
			if cleanLine != "" {
				checkLines = append(checkLines, cleanLine)
			}
		}
	} else if len(separatorIndices) == 1 {
		// Permission dialog: only 1 separator, check lines below it
		sepIdx := separatorIndices[0]
		for idx := sepIdx + 1; idx < len(lines); idx++ {
			cleanLine := strings.TrimSpace(stripANSIForDetect(lines[idx]))
			if cleanLine != "" {
				checkLines = append(checkLines, cleanLine)
			}
		}
	} else {
		// No separators - check last lines
		for j := len(lines) - 1; j >= 0 && j >= len(lines)-10; j-- {
			cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
			if cleanLine != "" {
				checkLines = append(checkLines, cleanLine)
			}
		}
	}

	for _, line := range checkLines {
		lineLower := strings.ToLower(line)
		for _, pattern := range patterns.WaitingPatterns {
			if strings.Contains(lineLower, pattern) {
				return true
			}
		}
	}

	return false
}

// detectGenericActivity checks last lines for waiting patterns,
// then checks for spinner animation for busy detection.
func detectGenericActivity(lines []string, patterns AgentPatterns, target string) SessionActivity {
	// Check for waiting patterns in last N non-empty lines
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < 15; j-- {
		line := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if line == "" {
			continue
		}
		nonEmptyCount++
		lineLower := strings.ToLower(line)
		for _, pattern := range patterns.WaitingPatterns {
			if strings.Contains(lineLower, pattern) {
				return ActivityWaiting
			}
		}
	}

	// Check for spinner animation = busy
	if isSpinnerAnimating(lines, patterns.Spinners, 15, target) {
		return ActivityBusy
	}

	return ActivityIdle
}

// findSpinnerLine returns the first line (from bottom) that starts with a
// spinner character. Returns the cleaned line content, or "" if not found.
func findSpinnerLine(lines []string, spinners []string, maxLines int) string {
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < maxLines; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		nonEmptyCount++
		for _, s := range spinners {
			if strings.HasPrefix(cleanLine, s) {
				return cleanLine
			}
		}
	}
	return ""
}

// isSpinnerAnimating checks if a spinner is actively animating by capturing
// the pane twice with a short delay. If the spinner line changed between
// captures, it's a real active spinner (not a stale one in scrollback).
func isSpinnerAnimating(lines []string, spinners []string, maxLines int, target string) bool {
	spinnerLine1 := findSpinnerLine(lines, spinners, maxLines)
	if spinnerLine1 == "" {
		return false
	}

	// Spinner found - wait briefly and re-capture to verify animation
	time.Sleep(150 * time.Millisecond)

	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines2 := strings.Split(string(output), "\n")
	spinnerLine2 := findSpinnerLine(lines2, spinners, maxLines)

	// Spinner is animating if the line changed
	return spinnerLine2 != "" && spinnerLine2 != spinnerLine1
}

// hasActiveToolExecution checks for active tool execution by looking for
// lines starting with ⎿ (tool output prefix) and ending with … (ellipsis).
// During execution: "⎿  Running…". After completion, results replace this.
func hasActiveToolExecution(lines []string, maxLines int) bool {
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < maxLines; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		nonEmptyCount++
		if strings.HasPrefix(cleanLine, "⎿") && strings.HasSuffix(cleanLine, "…") {
			return true
		}
	}
	return false
}

// hasActiveThinking checks for extended thinking indicators (✽/✻) with
// ellipsis (…) which indicates thinking is still in progress.
// After completion, the line shows "✻ Cogitated for Xs" without ellipsis.
func hasActiveThinking(lines []string, maxLines int) bool {
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < maxLines; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		nonEmptyCount++
		for _, indicator := range thinkingIndicators {
			if strings.HasPrefix(cleanLine, indicator) && strings.Contains(cleanLine, "…") {
				return true
			}
		}
	}
	return false
}

// stripANSIForDetect removes ANSI escape sequences (uses stripANSI from instance.go)
func stripANSIForDetect(s string) string {
	return StripANSI(s)
}
