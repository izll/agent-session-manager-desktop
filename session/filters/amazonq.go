package filters

import "strings"

// AmazonQFilter filters status lines for Amazon Q CLI
func AmazonQFilter(cleanLine string) (skip bool, content string) {
	// Skip UI elements
	if strings.HasPrefix(cleanLine, ">") || strings.Contains(cleanLine, "Amazon Q") {
		return true, ""
	}
	// Skip separator lines
	if strings.Count(cleanLine, "─") > 20 || strings.Count(cleanLine, "━") > 20 {
		return true, ""
	}
	return false, ""
}
