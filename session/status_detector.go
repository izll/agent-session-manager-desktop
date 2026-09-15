package session

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session/filters"
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

// cursorSpinners are the braille cells Cursor's "Thinking" line can start with.
//
// It pads the line with U+2800 — the blank braille cell, which looks like a
// space and is not in the default set — and puts the animating glyph second:
// "⠀⠞ Thinking  28 tokens". findSpinnerLine matches on a prefix, so with the
// default set it found nothing and a working Cursor never registered as busy.
//
// The blank is what every frame of the animation has in common, whatever glyph
// follows it. A glyph missing from the set costs nothing beyond that frame —
// the line is re-read every tick, and "ctrl+c to stop" in the footer answers
// the same question without depending on the animation at all.
//
// Ordered defaults-first to match how patternsFor builds the list from
// patterns.json (DefaultSpinners then extraSpinners): the two are compared
// element by element, so the reverse order would read as a mismatch between
// the file and what is compiled in.
var cursorSpinners = append(append([]string{}, defaultSpinners...), "⠀")

// antigravitySpinners are the braille cells Antigravity's working line spins
// through: "⡿  Generating...", "⢿  Editing files...".
//
// None of the eight is in the default set — that one runs ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏, the
// lighter half of the block, and Antigravity uses the denser half. Collected
// from a running 1.2.3 rather than reasoned about, which is also how it is
// known that the line does animate between two captures 60ms apart.
//
// Ordered defaults-first to match how patternsFor builds the list from
// patterns.json (DefaultSpinners then extraSpinners): the two are compared
// element by element.
var antigravitySpinners = append(append([]string{}, defaultSpinners...),
	"⡿", "⢿", "⣟", "⣯", "⣷", "⣻", "⣽", "⣾")

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
	AgentAntigravity: {
		// Measured off a running agy 1.2.3, not copied from Gemini: the harness
		// is a rewrite and words its prompts its own way.
		//
		// Two prompts were seen. The permission dialog leads with "Requesting
		// permission for:" and asks "Run this command?" over a numbered list
		// ending in "No, cancel". The trust prompt is the first thing a fresh
		// workspace shows — it blocks before the agent has done anything, and a
		// session sitting on it is waiting on the user as surely as any
		// approval.
		//
		// Deliberately absent: "esc to cancel". It is in the footer throughout
		// a turn, including under the permission dialog, so it says the turn is
		// in flight — busy, not waiting — and the generic detector checks
		// waiting first, which is what keeps the two apart.
		WaitingPatterns: []string{
			"requesting permission for:",
			"run this command?",
			"no, cancel",
			"do you trust the contents",
		},
		Spinners: antigravitySpinners,
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
	AgentCursor: {
		// Taken from a captured approval prompt, not from the documentation.
		// The generic wording that was here first — "allow once", "(y/n)" —
		// matched a Go source listing in the transcript, and a session sat on
		// "waiting" because the pane happened to be showing this very file.
		//
		// Each of these appears once on a pane that is asking, and nowhere on
		// one that is not. "Run Everything" is deliberately absent although it
		// is one of the answers offered: it is also the footer's standing mode
		// label, on screen while the agent sits idle, so it would pin every
		// idle session to waiting instead.
		WaitingPatterns: []string{
			"run this command?",
			"not in allowlist:",
			"skip & tell the agent",
		},
		Spinners: cursorSpinners,
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
	} else if agent == AgentCursor {
		activity = detectCursorActivityContext(ctx, lines, patterns, target)
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

// detectCursorActivity recognises Cursor CLI's busy/waiting markers.
//
// Cursor gets its own detection rather than the generic path for two reasons,
// both measured against a running session:
//
//   - Its input box is drawn with half-block characters (▄ above, ▀ below), not
//     the light box-drawing set every other agent uses. The generic path has no
//     boundary at all, so it read the last 15 lines wherever they came from —
//     and matched "allow once" inside a Go source listing that happened to be
//     on screen. The transcript above the box is not a prompt.
//
//   - Its spinner line begins with U+2800, the *blank* braille cell, with the
//     animating glyph second: "⠀⠞ Thinking  28 tokens". findSpinnerLine matches
//     on a prefix, so the default braille set never matched that frame.
//
//   - The spinner is drawn *above* the input box, in the last lines of the
//     transcript — not below it like the footer. Reading only from the box
//     down, which is right for a prompt, cannot see it, and "⠴ Exploring 205s"
//     was reported idle although every character of it was already recognised.
//     Its counter ticks once a second, so re-capturing 60ms later to prove the
//     line is animating usually gets the same line back and proves nothing.
//     The spinner is therefore taken as busy on its own, as Codex's "Working"
//     line is.
func detectCursorActivity(lines []string, patterns AgentPatterns, target string) SessionActivity {
	return detectCursorActivityContext(context.Background(), lines, patterns, target)
}

func detectCursorActivityContext(ctx context.Context, lines []string, patterns AgentPatterns, target string) SessionActivity {
	// Everything below the input box belongs to the current screen. Above it is
	// the transcript, where the agent's own words — or a file it printed — can
	// say anything at all.
	from := cursorInputBoxTop(lines)

	for j := len(lines) - 1; j >= from; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		for _, pattern := range patterns.WaitingPatterns {
			if strings.Contains(lower, pattern) {
				debugf("[StatusDebug] %s → WAITING (%q)", target, pattern)
				return ActivityWaiting
			}
		}
	}

	// Busy: the footer says how to stop the turn while one is in flight, and
	// removes it when the turn ends. Cheaper and steadier than the spinner,
	// which needs a second capture to prove it is animating.
	for j := len(lines) - 1; j >= from; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if clean == "" {
			continue
		}
		if strings.Contains(strings.ToLower(clean), "ctrl+c to stop") {
			debugf("[StatusDebug] %s → BUSY (ctrl+c to stop)", target)
			return ActivityBusy
		}
	}

	// The spinner sits above the input box, so it is looked for in the lines
	// leading up to it rather than below. The window is kept short — a spinner
	// is on the last line the agent wrote, and a wider one would reach into the
	// transcript, where a braille character is just a character.
	//
	// With no box on screen — Cursor draws none while it is starting up, and
	// cursorInputBoxTop then answers 0 — the whole capture is the search area.
	// Slicing to lines[:0] instead would hide the spinner in exactly the case
	// where it is the only thing there is to go on.
	above := lines
	if from > 0 {
		above = lines[:from]
	}
	if line := findSpinnerLine(cursorSpinnerSearchArea(above), patterns.Spinners,
		cursorSpinnerLookback); line != "" {
		debugf("[StatusDebug] %s → BUSY (spinner %q)", target, line)
		return ActivityBusy
	}

	return ActivityIdle
}

// cursorSpinnerSearchArea drops the lines Cursor draws for itself, so that the
// lookback below counts what the agent wrote rather than what the interface
// painted.
//
// Measured on a running pane: between the spinner and the input box sat a
// "Tip: Use /plan …" hint and the box's own top border. Three screen lines
// were therefore spent before reaching a spinner two lines up, and a working
// session read as idle. The same filter the sidebar uses already knows both
// for what they are.
func cursorSpinnerSearchArea(lines []string) []string {
	config, ok := filters.LoadFilters()["cursor"]
	if !ok {
		return lines
	}
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(stripANSIForDetect(line))
		if clean == "" {
			continue
		}
		if skip, _ := filters.ApplyFilter(config, clean); skip {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// cursorSpinnerLookback is how many lines of the agent's own output above the
// input box are searched for the spinner.
//
// Two, counted after cursorSpinnerSearchArea has removed the interface's own
// lines. Measured against three panes: on a live turn and on the reported
// screenshot the spinner is the first line that survives filtering, while a
// braille character quoted in the transcript sits third, behind two sentences
// the agent actually said. Two separates them; three does not.
const cursorSpinnerLookback = 2

// cursorInputBoxTop returns the index of the line where Cursor's current screen
// starts — everything below it belongs to now, everything above is transcript.
// 0 when no boundary is on screen.
//
// Cursor draws two different rules, and both have to count. Its input box is
// made of half blocks (▄ on top, ▀ underneath), but its approval dialog is
// introduced by a plain ─ rule instead, with no box around it at all. Looking
// only for half blocks found no boundary on a pane that was asking a question,
// which put the search back over the whole transcript — the thing this is here
// to prevent.
//
// The lowest rule wins: it is the one belonging to whatever is on screen now,
// rather than to something scrolled up. A single glyph is not enough — both
// characters occur in ordinary output, in a chart or a progress bar — so a run
// is required, the same threshold the other separator checks use.
func cursorInputBoxTop(lines []string) int {
	for j := len(lines) - 1; j >= 0; j-- {
		clean := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if strings.Count(clean, "▄")+strings.Count(clean, "▀") > 20 {
			return j
		}
		if strings.Count(clean, "─")+strings.Count(clean, "━")+strings.Count(clean, "╌") > 20 {
			return j
		}
	}
	return 0
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

// isBrailleSpinnerRune reports whether r is in the braille block, U+2800-U+28FF.
//
// Every agent here that spins does it with braille, and each picks its own
// frames out of the 256 available: the classic ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏, Antigravity's
// denser ⡿⢿⣟⣯⣷⣻⣽⣾, Cursor's ⠘⠤ and ⠞. Listing them was a losing game —
// "⠴ Exploring" was recognised while "⠘⠤ Thinking" was not, on the same agent,
// minutes apart, because one frame happened to be in the list and the other
// was not. A missing frame is silent: the session simply reads as idle while
// it works.
//
// The block holds nothing but spinner cells, so accepting all of it costs no
// precision and ends the whole class of bug. Spinners outside braille — the
// ◐◑◒◓ wheel, Gemini's ∴∵⋮⋯✦ — still come from the per-agent list.
func isBrailleSpinnerRune(r rune) bool {
	return r >= 0x2800 && r <= 0x28FF
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
		for _, r := range cleanLine {
			if isBrailleSpinnerRune(r) {
				return cleanLine
			}
			break // only the first rune decides
		}
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
//
// The search is bounded by the input box rather than by a line count. It used
// to stop after five non-empty lines from the bottom, which was enough only
// while nothing followed the status bar — but Claude lists every spawned agent
// underneath it, and a session running five agents pushed the bar six lines up,
// out of reach. A busy session then showed as idle precisely when it was at its
// busiest.
//
// Everything below the last separator belongs to the current screen, never to
// the scrollback the count was there to exclude, so the whole of it is safe to
// search.
func hasEscToInterrupt(lines []string) bool {
	for j := len(lines) - 1; j >= 0; j-- {
		cleanLine := strings.TrimSpace(stripANSIForDetect(lines[j]))
		if cleanLine == "" {
			continue
		}
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━") + strings.Count(cleanLine, "╌")
		if sepCount > 20 {
			return false
		}
		if strings.Contains(strings.ToLower(cleanLine), "esc to interrupt") {
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
	// Claude Code's explicit sentence, and only that. The status bar's
	// "← 1 agent" looked like a shorter way to say the same thing and is not:
	// the count is how many agents the session HAS, not how many are running,
	// so it stays on screen after the work ends and marked finished sessions
	// busy. The sentence appears only while an agent is actually working.
	`waiting for \d+ background agents? to finish`,
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
// The notice is printed just above the input box, so a search bounded by the
// separator would never see it. Searching the whole pane is wrong too: the
// notice is not removed when the agent finishes, it scrolls up into the
// transcript, where it went on claiming the session was busy — one session read
// as busy for over an hour with the sentence sitting 77 lines up.
//
// What separates the two is what comes after it. A live notice is the last
// thing the agent has said; once the work finishes, Claude answers, and that
// reply ("● ...") lands below the notice. So a notice with a reply under it
// belongs to the transcript, whatever its distance from the box.
func hasRunningBackgroundAgent(lines []string) bool {
	patterns := backgroundAgentPatterns()
	if len(patterns) == 0 {
		return false
	}

	for j := len(lines) - 1; j >= 0; j-- {
		clean := stripANSIForDetect(lines[j])
		trimmed := strings.TrimSpace(clean)
		if trimmed == "" {
			continue
		}

		// A reply below the notice means the wait it announced is over. Reached
		// first while walking up, it ends the search: anything above is past.
		//
		// Column matters: Claude's replies start at column zero, while the
		// status bar's branch and agent rows ("  ● main") are indented and sit
		// below the notice, where they would end the search on every pane.
		if strings.HasPrefix(clean, "●") {
			return false
		}

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
