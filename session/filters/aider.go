package filters

import "strings"

// AiderFilter filters status lines for Aider CLI
func AiderFilter(cleanLine string) (skip bool, content string) {
	// Skip prompt lines
	if strings.HasPrefix(cleanLine, ">") || strings.HasPrefix(cleanLine, "aider>") {
		return true, ""
	}
	// Skip separator lines
	if strings.Count(cleanLine, "─") > 20 || strings.Count(cleanLine, "━") > 20 {
		return true, ""
	}
	return false, ""
}
