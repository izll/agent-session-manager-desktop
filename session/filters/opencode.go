package filters

import "strings"

// OpenCodeFilter filters status lines for OpenCode CLI
func OpenCodeFilter(cleanLine string) (skip bool, content string) {
	// Skip separator lines
	if strings.Count(cleanLine, "─") > 15 || strings.Count(cleanLine, "━") > 15 {
		return true, ""
	}
	// Skip prompt
	if cleanLine == ">" || cleanLine == "›" {
		return true, ""
	}
	// Skip status bar
	if strings.Contains(cleanLine, "ctrl+?") || strings.Contains(cleanLine, "Context:") {
		return true, ""
	}
	// Skip help/instruction lines
	if strings.Contains(cleanLine, "press enter to send") || strings.Contains(cleanLine, "press esc") {
		return true, ""
	}
	// Skip model info line
	if strings.Contains(cleanLine, "No diagnostics") || strings.Contains(cleanLine, "GPT-4o") || strings.Contains(cleanLine, "Cost:") {
		return true, ""
	}
	// Skip task/glob output markers
	if strings.HasPrefix(cleanLine, "└") || strings.HasPrefix(cleanLine, "├") || strings.HasPrefix(cleanLine, "│") {
		return true, ""
	}
	// Skip lines starting with common prefixes (paths, commands, etc.)
	if strings.HasPrefix(cleanLine, "Glob:") || strings.HasPrefix(cleanLine, "List:") || strings.HasPrefix(cleanLine, "Task:") {
		return true, ""
	}
	// Show "Generating..." as status
	if strings.Contains(cleanLine, "Generating") {
		return false, "Generating..."
	}
	// Content lines start with ┃ - extract the content
	if strings.HasPrefix(cleanLine, "┃") {
		extracted := strings.TrimSpace(strings.TrimPrefix(cleanLine, "┃"))
		// Skip empty, "None.", short fragments, and file paths
		if extracted == "" || extracted == "None." || len(extracted) < 15 {
			return true, ""
		}
		if strings.HasPrefix(extracted, "/") || strings.HasPrefix(extracted, "~") || strings.HasPrefix(extracted, ".") {
			return true, ""
		}
		// Skip glob/list markers inside content
		if strings.HasPrefix(extracted, "└") || strings.HasPrefix(extracted, "├") || strings.HasPrefix(extracted, "Glob:") {
			return true, ""
		}
		// Return extracted content
		return false, extracted
	}
	return false, ""
}
