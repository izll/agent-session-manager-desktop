package session

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// busyGracePeriod is the duration to keep reporting Busy after the last
// real busy detection. This smooths out brief gaps between phases
// (e.g., thinking ends → tool execution starts).
const busyGracePeriod = 6 * time.Second

// DebugLogging controls whether verbose diagnostics are written.
// Set from main by a dev build, or by --debug / ASMGR_DEBUG=1 on any build, so
// a user can produce a diagnostic log without installing a different binary.
var DebugLogging = false

func debugf(format string, args ...interface{}) {
	if DebugLogging {
		log.Printf(format, args...)
	}
}

// lastBusyTime tracks the last time Busy was detected per target (session:window)
var lastBusyTime sync.Map // map[string]time.Time

// lastYoloState caches the last DEFINITIVE yolo reading per target. While the
// agent shows a permission/question dialog the mode bar is hidden, so we can't
// read the mode — we then return this cached value instead of flickering off.
var lastYoloState sync.Map // map[string]bool

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

// activeThinkingIndicators prefers the configured markers, falling back to the
// compiled ones — same reasoning as the waiting patterns: these are an agent's
// own characters, and it may change them.
func activeThinkingIndicators() []string {
	if fromFile := thinkingIndicatorsFromFile(); len(fromFile) > 0 {
		return fromFile
	}
	return thinkingIndicators
}

// Agent-specific patterns
var agentPatterns = map[AgentType]AgentPatterns{
	AgentClaude: {
		WaitingPatterns: []string{
			"do you want to proceed",
			"would you like to proceed",
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
		// Codex phrases its approval prompt its own way; the generic
		// "allow once"/"do you want to proceed" wording never appears, so
		// those alone left every Codex question undetected.
		WaitingPatterns: []string{
			"would you like to run",
			"press enter to confirm",
			"yes, proceed",
			"yes, and don't ask again",
			"tell codex what to do differently",
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

// getAgentPatterns returns patterns for the given agent type.
//
// The JSON file first — that is what can be corrected without a release when an
// agent rewords a prompt. The map below is the fallback, and stays as the
// answer if the file is missing an agent or will not parse.
func getAgentPatterns(agent AgentType) AgentPatterns {
	if fromFile, ok := patternsFor(agent); ok {
		return fromFile
	}
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
	activity, _ := i.DetectActivityForWindowWithValidity(windowIdx)
	return activity
}

// DetectActivityForWindowWithValidity distinguishes a real idle detection from
// a failed tmux probe. Callers collecting statistics must not turn capture
// errors into idle time.
func (i *Instance) DetectActivityForWindowWithValidity(windowIdx int) (SessionActivity, bool) {
	return i.DetectActivityForWindowWithValidityContext(context.Background(), windowIdx)
}

// DetectActivityForWindowWithValidityContext is the cancellable form used by
// lifecycle-owned polling work.
func (i *Instance) DetectActivityForWindowWithValidityContext(ctx context.Context, windowIdx int) (SessionActivity, bool) {
	if !i.IsAliveContext(ctx) {
		return ActivityIdle, false
	}

	target := i.GetCaptureTargetContext(ctx, windowIdx)

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

	// Terminal tabs are not AI agents - skip activity detection entirely
	if agent == AgentTerminal {
		return ActivityIdle, true
	}

	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	cmd := TmuxCommandContext(commandCtx, "capture-pane", "-t", target, "-p", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return ActivityIdle, false
	}

	lines := strings.Split(string(output), "\n")
	patterns := getAgentPatterns(agent)

	var activity SessionActivity
	// Claude uses separator-based waiting detection
	if agent == AgentClaude {
		activity = detectClaudeActivityContext(ctx, lines, patterns, target)
	} else if agent == AgentCodex {
		activity = detectCodexActivity(lines, patterns)
	} else {
		// All other agents use generic detection
		activity = detectGenericActivityContext(ctx, lines, patterns, target)
	}

	// Apply busy grace period: if we detected busy, update the timestamp.
	// If we got idle but were busy recently, keep reporting busy.
	if activity == ActivityBusy {
		lastBusyTime.Store(target, time.Now())
		return ActivityBusy, true
	}
	if activity == ActivityIdle {
		if lastTime, ok := lastBusyTime.Load(target); ok {
			if time.Since(lastTime.(time.Time)) < busyGracePeriod {
				return ActivityBusy, true
			}
			// Grace period expired, clean up
			lastBusyTime.Delete(target)
		}
	}
	// Waiting always takes priority, and also clears grace period
	if activity == ActivityWaiting {
		lastBusyTime.Delete(target)
	}

	return activity, true
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

// DetectYoloForWindow reports whether the agent in this window is currently in a
// non-interactive ("YOLO") mode, read live from the pane's status bar.
//
// Claude Code's Shift+Tab cycle shows one of these in the bottom bar:
//
//	"⏵⏵ bypass permissions on (shift+tab to cycle)"  → YOLO (skips ALL checks)
//	"⏵⏵ auto mode on (shift+tab to cycle) ..."        → YOLO (auto-approves via a
//	     risk classifier — still runs without prompting the user)
//	"⏵⏵ accept edits on ..."                          → NOT yolo (edits only;
//	     other commands still prompt)
//
// Both bypass and auto count as YOLO here because the user asked the badge to
// flag any "runs without asking me" mode. This follows a Shift+Tab toggle inside
// Claude, not just the stored launch flag.
// Returns false for non-Claude agents (only Claude has this status line).
func (i *Instance) DetectYoloForWindow(windowIdx int) bool {
	return i.DetectYoloForWindowContext(context.Background(), windowIdx)
}

// DetectYoloForWindowContext is the cancellable form used by the preview
// poller.
func (i *Instance) DetectYoloForWindowContext(ctx context.Context, windowIdx int) bool {
	if !i.IsAliveContext(ctx) {
		return false
	}
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
	if agent != AgentClaude {
		return false
	}

	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	// The mode line ("⏵⏵ bypass permissions on") lives near the bottom, but while
	// the agent is BUSY extra rows appear below it (spinner, separators, the
	// input box, a running-agents/token list). A 6-line window was too small —
	// the mode line scrolled out of it during work, so the YOLO badge flickered
	// off whenever the tab was busy. Capture more rows so it stays in view. Still
	// cheap (one capture per tab per poll).
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	out, err := TmuxCommandContext(commandCtx, "capture-pane", "-t", target, "-p", "-S", "-16").Output()
	if err != nil {
		return cachedYolo(target)
	}
	lower := strings.ToLower(string(out))

	yoloOn := strings.Contains(lower, "bypass permissions on") ||
		strings.Contains(lower, "auto mode on")
	// A NON-yolo mode is definitively shown: plain default (no marker but the
	// mode bar is present) or accept-edits. We detect "the mode bar is present"
	// via the shift+tab hint that always accompanies it.
	nonYolo := strings.Contains(lower, "accept edits on")
	modeBarVisible := yoloOn || nonYolo || strings.Contains(lower, "shift+tab to cycle")

	if modeBarVisible {
		// Definitive reading — cache and return it.
		lastYoloState.Store(target, yoloOn)
		return yoloOn
	}
	// Mode bar hidden (e.g. a permission/question dialog is up). Keep the last
	// known state instead of flickering the badge off.
	return cachedYolo(target)
}

// cachedYolo returns the last definitive yolo reading for target, or false.
func cachedYolo(target string) bool {
	if v, ok := lastYoloState.Load(target); ok {
		return v.(bool)
	}
	return false
}

// detectClaudeActivity uses Claude Code's UI structure for waiting detection,
// and spinner animation + extended thinking check for busy detection.
func detectClaudeActivity(lines []string, patterns AgentPatterns, target string) SessionActivity {
	return detectClaudeActivityContext(context.Background(), lines, patterns, target)
}

func detectClaudeActivityContext(ctx context.Context, lines []string, patterns AgentPatterns, target string) SessionActivity {
	// --- Waiting detection: uses separator structure to avoid scrollback false positives ---
	waiting := checkClaudeWaiting(lines, patterns)
	if waiting {
		debugf("[StatusDebug] %s → WAITING", target)
		return ActivityWaiting
	}

	// --- Busy detection 0: "esc to interrupt" in status bar - FASTEST, most reliable ---
	// Claude Code shows "esc to interrupt" in the bottom status bar while working
	if hasEscToInterrupt(lines) {
		debugf("[StatusDebug] %s → BUSY (esc to interrupt)", target)
		return ActivityBusy
	}

	// --- Busy detection 1: a background agent is still running ---
	// The main thread can be idle while a spawned agent works for an hour, and
	// none of the other indicators fire: there is no "esc to interrupt", no
	// spinner, and the line saying so sits ABOVE the input separator, where the
	// thinking check stops looking. The session is plainly busy all the same.
	if hasRunningBackgroundAgent(lines) {
		debugf("[StatusDebug] %s → BUSY (background agent)", target)
		return ActivityBusy
	}

	// --- Busy detection 2: extended thinking indicator (✽/✻ with …) - FAST, no delay ---
	if hasActiveThinking(lines, 20) {
		debugf("[StatusDebug] %s → BUSY (thinking)", target)
		return ActivityBusy
	}

	// --- Busy detection 2: tool execution (⎿ ... ending with …) - FAST, no delay ---
	if hasActiveToolExecution(lines, 10) {
		debugf("[StatusDebug] %s → BUSY (tool exec)", target)
		return ActivityBusy
	}

	// --- Busy detection 3: braille spinner animation - SLOW, needs 2 captures ---
	if isSpinnerAnimatingContext(ctx, lines, patterns.Spinners, 20, target) {
		debugf("[StatusDebug] %s → BUSY (spinner)", target)
		return ActivityBusy
	}

	debugf("[StatusDebug] %s → IDLE", target)
	return ActivityIdle
}

// checkClaudeWaiting checks for waiting patterns in Claude's UI structure.
// Uses separator lines to identify the input area and permission dialogs.
// Claude's permission dialogs always show separator lines, so we require
// fresh separators to detect waiting. This prevents false positives from
// old permission text lingering in scrollback history.
func checkClaudeWaiting(lines []string, patterns AgentPatterns) bool {
	// Find separator line positions
	var separatorIndices []int
	for idx, line := range lines {
		cleanLine := strings.TrimSpace(stripANSIForDetect(line))
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━") + strings.Count(cleanLine, "╌")
		if sepCount > 20 {
			separatorIndices = append(separatorIndices, idx)
		}
	}

	debugf("[WaitDebug] totalLines=%d separators=%d at=%v", len(lines), len(separatorIndices), separatorIndices)

	// Claude's permission dialogs always have separator lines.
	// No separators = not in a permission state, never waiting.
	if len(separatorIndices) == 0 {
		debugf("[WaitDebug] no separators → false")
		return false
	}

	var checkLines []string

	if len(separatorIndices) >= 2 {
		topSepIdx := separatorIndices[len(separatorIndices)-2]
		bottomSepIdx := separatorIndices[len(separatorIndices)-1]

		// Check if separators are stale (from a previous turn).
		// In normal state, only 0-3 lines below the bottom separator.
		nonEmptyBelow := 0
		for j := bottomSepIdx + 1; j < len(lines); j++ {
			cl := strings.TrimSpace(stripANSIForDetect(lines[j]))
			if cl != "" {
				nonEmptyBelow++
			}
		}
		debugf("[WaitDebug] 2+ seps: top=%d bottom=%d nonEmptyBelow=%d", topSepIdx, bottomSepIdx, nonEmptyBelow)
		if nonEmptyBelow > 12 {
			// Separators are stale - Claude moved past the permission dialog.
			// Threshold is 12 to accommodate permission prompts with multiple options
			// (e.g., "Yes" / "Yes, allow X from this project" / "No" + context lines)
			debugf("[WaitDebug] stale separators → false")
			return false
		}

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
	} else {
		// 1 separator - check proximity (must be near bottom of output)
		sepIdx := separatorIndices[0]
		debugf("[WaitDebug] 1 sep at=%d distFromBottom=%d", sepIdx, len(lines)-sepIdx)
		if sepIdx < len(lines)-15 {
			// Separator is too far from bottom, likely stale from old content
			debugf("[WaitDebug] 1 sep too far from bottom → false")
			return false
		}

		// Permission dialog: check lines below separator
		for idx := sepIdx + 1; idx < len(lines); idx++ {
			cleanLine := strings.TrimSpace(stripANSIForDetect(lines[idx]))
			if cleanLine != "" {
				checkLines = append(checkLines, cleanLine)
			}
		}
	}

	debugf("[WaitDebug] checkLines(%d): %v", len(checkLines), truncateLines(checkLines, 5))

	for _, line := range checkLines {
		lineLower := strings.ToLower(line)
		for _, pattern := range patterns.WaitingPatterns {
			if strings.Contains(lineLower, pattern) {
				debugf("[WaitDebug] MATCH pattern=%q in line=%q → true", pattern, truncStr(line, 80))
				return true
			}
		}
	}

	debugf("[WaitDebug] no pattern match → false")
	return false
}

// truncStr truncates a string for debug logging
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// truncateLines returns first N lines for debug logging
func truncateLines(lines []string, max int) []string {
	if len(lines) <= max {
		return lines
	}
	result := make([]string, max)
	for i := 0; i < max; i++ {
		result[i] = truncStr(lines[i], 60)
	}
	return append(result, fmt.Sprintf("...+%d more", len(lines)-max))
}

// detectCodexActivity recognises Codex CLI's busy/waiting markers without
// relying on a spinner animation (Codex often shows a static "Working …"
// line, no rotating glyph). Markers we look for, anywhere in the captured
// pane (Codex pads with blank lines so "last N non-empty" misses them):
//
//	busy:
//	  - "Working (Ns · esc to interrupt)"
//	  - "esc to interrupt" anywhere
//	  - "Explored", "Ran <cmd>", "Read <file>" tool-execution lines
//	    still in their active form (no completion marker)
//
//	waiting:
//	  - approval prompts ("allow once", "do you want to proceed", etc.)
//
// idle: bottom status bar `gpt-X.Y high · ~/...` is alone with no Working.
func detectCodexActivity(lines []string, patterns AgentPatterns) SessionActivity {
	// 1) Waiting: scan more of the buffer than the generic last-15 window —
	//    Codex prompts can be padded with empty lines.
	for j := len(lines) - 1; j >= 0 && j > len(lines)-40; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		for _, pattern := range patterns.WaitingPatterns {
			if strings.Contains(lower, pattern) {
				return ActivityWaiting
			}
		}
	}

	// 2) Busy: look for "esc to interrupt" or a "Working" line anywhere in
	//    the recent capture. Codex keeps these in place while the agent runs
	//    and removes them once it goes idle.
	for j := len(lines) - 1; j >= 0; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		if strings.Contains(lower, "esc to interrupt") {
			return ActivityBusy
		}
		// Codex shows "• Working (Ns)" while a turn is in flight.
		if strings.Contains(clean, "Working (") || strings.HasPrefix(clean, "Working") {
			return ActivityBusy
		}
	}

	return ActivityIdle
}

// detectGenericActivity checks last lines for waiting patterns,
// then checks for spinner animation for busy detection.
func detectGenericActivity(lines []string, patterns AgentPatterns, target string) SessionActivity {
	return detectGenericActivityContext(context.Background(), lines, patterns, target)
}

func detectGenericActivityContext(ctx context.Context, lines []string, patterns AgentPatterns, target string) SessionActivity {
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
	if isSpinnerAnimatingContext(ctx, lines, patterns.Spinners, 15, target) {
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
	return isSpinnerAnimatingContext(context.Background(), lines, spinners, maxLines, target)
}

func isSpinnerAnimatingContext(ctx context.Context, lines []string, spinners []string, maxLines int, target string) bool {
	spinnerLine1 := findSpinnerLine(lines, spinners, maxLines)
	if spinnerLine1 == "" {
		return false
	}

	// Spinner found - wait briefly and re-capture to verify animation
	timer := time.NewTimer(60 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return false
	}

	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	cmd := TmuxCommandContext(commandCtx, "capture-pane", "-t", target, "-p", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines2 := strings.Split(string(output), "\n")
	spinnerLine2 := findSpinnerLine(lines2, spinners, maxLines)

	// Spinner is animating if the line changed
	return spinnerLine2 != "" && spinnerLine2 != spinnerLine1
}

// hasEscToInterrupt checks if the Claude Code status bar shows "esc to interrupt"
// which is a reliable indicator that Claude is actively processing.
// Only checks the last few non-empty lines to avoid false positives from scrollback.
func hasEscToInterrupt(lines []string) bool {
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < 5; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		nonEmptyCount++
		lineLower := strings.ToLower(cleanLine)
		if strings.Contains(lineLower, "esc to interrupt") {
			return true
		}
	}
	return false
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
		// Stop at separator boundary - anything above is from a previous turn
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━") + strings.Count(cleanLine, "╌")
		if sepCount > 20 {
			return false
		}
		if strings.HasPrefix(cleanLine, "⎿") && strings.HasSuffix(cleanLine, "…") {
			return true
		}
	}
	return false
}

// hasActiveThinking checks for extended thinking indicators (✽/✻) with
// ellipsis (…) which indicates thinking is still in progress.
// After completion, the line shows "✻ Cogitated for Xs" without ellipsis.
// defaultBackgroundAgentPatterns is the fallback when patterns.json has none:
// Claude Code's wording for a spawned agent that has not finished.
var defaultBackgroundAgentPatterns = []string{
	// The full sentence, printed when there is room for it.
	`waiting for \d+ background agents? to finish`,
	// The status bar's abbreviation when there is not. The count is what makes
	// this safe: "← for agents" is the idle wording and appears constantly.
	`←\s*\d+\s+agents?\b`,
}

var (
	backgroundAgentMu       sync.Mutex
	backgroundAgentCompiled []*regexp.Regexp
	backgroundAgentSource   []string
)

// backgroundAgentPatterns compiles the configured expressions, reusing the last
// result while the configuration is unchanged. An expression that does not
// compile is skipped rather than fatal: the pattern file is updated over the
// network, and one bad entry must not stop the rest from working.
func backgroundAgentPatterns() []*regexp.Regexp {
	source := backgroundAgentPatternsFromFile()
	if source == nil {
		source = defaultBackgroundAgentPatterns
	}

	backgroundAgentMu.Lock()
	defer backgroundAgentMu.Unlock()
	if slicesEqual(source, backgroundAgentSource) {
		return backgroundAgentCompiled
	}

	compiled := make([]*regexp.Regexp, 0, len(source))
	for _, expr := range source {
		re, err := regexp.Compile("(?i)" + expr)
		if err != nil {
			debugf("[StatusDebug] ignoring unparsable background-agent pattern %q: %v", expr, err)
			continue
		}
		compiled = append(compiled, re)
	}
	backgroundAgentSource = append([]string(nil), source...)
	backgroundAgentCompiled = compiled
	return compiled
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hasRunningBackgroundAgent reports whether the pane says a spawned agent is
// still working.
//
// Unlike the other busy checks this one does not stop at the separator: the
// notice is printed above the input box, so a separator-bounded search would
// never reach it. The whole visible pane is searched instead, which is safe
// because the line is removed as soon as the agent finishes — unlike the
// scrollback text the separator rule exists to ignore.
func hasRunningBackgroundAgent(lines []string) bool {
	patterns := backgroundAgentPatterns()
	if len(patterns) == 0 {
		return false
	}
	for _, line := range lines {
		clean := stripANSIForDetect(line)
		for _, re := range patterns {
			if re.MatchString(clean) {
				return true
			}
		}
	}
	return false
}

func hasActiveThinking(lines []string, maxLines int) bool {
	nonEmptyCount := 0
	for j := len(lines) - 1; j >= 0 && nonEmptyCount < maxLines; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		nonEmptyCount++
		// Stop at separator boundary - anything above is from a previous turn
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━") + strings.Count(cleanLine, "╌")
		if sepCount > 20 {
			return false
		}
		for _, indicator := range activeThinkingIndicators() {
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
