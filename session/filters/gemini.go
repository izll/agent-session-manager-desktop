package filters

import "strings"

// GeminiFilter filters status lines for Gemini CLI
func GeminiFilter(cleanLine string) (skip bool, content string) {
	// Skip UI elements
	if strings.HasPrefix(cleanLine, "╭") || strings.HasPrefix(cleanLine, "╰") || strings.HasPrefix(cleanLine, "│") {
		return true, ""
	}
	// Skip prompt line
	if strings.HasPrefix(cleanLine, ">") || strings.Contains(cleanLine, "Type your message") {
		return true, ""
	}
	// Skip separator lines
	if strings.Count(cleanLine, "─") > 20 {
		return true, ""
	}
	// Skip directory indicators (~/path or /path)
	if strings.HasPrefix(cleanLine, "~/") || (strings.HasPrefix(cleanLine, "/") && !strings.Contains(cleanLine, " ")) {
		return true, ""
	}
	return false, ""
}
