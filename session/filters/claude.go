package filters

import "strings"

// ClaudeFilter filters status lines for Claude Code CLI
func ClaudeFilter(cleanLine string) (skip bool, content string) {
	// Skip status bar elements
	if strings.Contains(cleanLine, "? for") || strings.Contains(cleanLine, "Context left") || strings.Contains(cleanLine, "accept edits") {
		return true, ""
	}
	// Skip separator lines (more than 20 dash chars)
	if strings.Count(cleanLine, "─") > 20 {
		return true, ""
	}
	// Skip empty prompt and box corners
	if cleanLine == ">" || strings.HasPrefix(cleanLine, "╭") || strings.HasPrefix(cleanLine, "╰") {
		return true, ""
	}
	return false, ""
}
