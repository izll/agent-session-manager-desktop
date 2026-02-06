package filters

// CustomFilter filters status lines for custom commands
// By default, no filtering is applied to custom commands
func CustomFilter(cleanLine string) (skip bool, content string) {
	return false, ""
}
