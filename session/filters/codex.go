package filters

import "strings"

// CodexFilter filters status lines for OpenAI Codex CLI
func CodexFilter(cleanLine string) (skip bool, content string) {
	// Skip prompt characters
	if strings.HasPrefix(cleanLine, ">") || strings.HasPrefix(cleanLine, "codex>") || strings.HasPrefix(cleanLine, "›") {
		return true, ""
	}
	// Skip separator lines and box drawing
	if strings.Count(cleanLine, "─") > 20 || strings.HasPrefix(cleanLine, "╭") || strings.HasPrefix(cleanLine, "╰") || strings.HasPrefix(cleanLine, "│") {
		return true, ""
	}
	// Skip status line
	if strings.Contains(cleanLine, "context left") || strings.Contains(cleanLine, "? for") {
		return true, ""
	}
	return false, ""
}
