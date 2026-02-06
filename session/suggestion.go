package session

import (
	"os/exec"
	"strings"
)

// GetSuggestion extracts the autocomplete suggestion from the agent's prompt area
func (i *Instance) GetSuggestion() string {
	if !i.IsAlive() {
		return ""
	}

	sessionName := i.TmuxSessionName()
	cmd := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-S", "-30")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")

	switch i.Agent {
	case AgentClaude, "":
		return extractClaudeSuggestion(lines)
	case AgentCodex:
		return extractCodexSuggestion(lines)
	case AgentGemini:
		return extractGeminiSuggestion(lines)
	default:
		return ""
	}
}

// extractClaudeSuggestion extracts suggestion from Claude Code's prompt area
// Claude shows suggestion as text after the ">" prompt between two horizontal lines
func extractClaudeSuggestion(lines []string) string {
	// Find the last two horizontal separators
	var separatorIndices []int
	for idx, line := range lines {
		cleanLine := strings.TrimSpace(StripANSI(line))
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━")
		if sepCount > 20 {
			separatorIndices = append(separatorIndices, idx)
		}
	}

	if len(separatorIndices) < 2 {
		return ""
	}

	topSepIdx := separatorIndices[len(separatorIndices)-2]
	bottomSepIdx := separatorIndices[len(separatorIndices)-1]

	// Look for "> suggestion" line between separators
	for idx := topSepIdx + 1; idx < bottomSepIdx; idx++ {
		cleanLine := strings.TrimSpace(StripANSI(lines[idx]))

		// Skip empty lines
		if cleanLine == "" {
			continue
		}

		// Claude uses non-breaking space (U+00A0) after ">", normalize it
		cleanLine = strings.ReplaceAll(cleanLine, "\u00A0", " ")

		// Found "> " with text after - that's the suggestion
		if strings.HasPrefix(cleanLine, "> ") && len(cleanLine) > 2 {
			return strings.TrimPrefix(cleanLine, "> ")
		}
	}

	return ""
}

// extractCodexSuggestion extracts suggestion from Codex's prompt area
// Codex uses "›" as the prompt character
func extractCodexSuggestion(lines []string) string {
	// Look for "› suggestion" line in last lines
	for j := len(lines) - 1; j >= 0 && j >= len(lines)-10; j-- {
		cleanLine := strings.TrimSpace(StripANSI(lines[j]))

		// Skip empty lines
		if cleanLine == "" {
			continue
		}

		// Normalize non-breaking space
		cleanLine = strings.ReplaceAll(cleanLine, "\u00A0", " ")

		// Found "› " with text after - that's the suggestion
		if strings.HasPrefix(cleanLine, "› ") && len(cleanLine) > 2 {
			return strings.TrimPrefix(cleanLine, "› ")
		}
	}

	return ""
}

// extractGeminiSuggestion extracts suggestion from Gemini's prompt area
// Gemini only shows suggestions during typing, so we can't pre-fetch them
func extractGeminiSuggestion(lines []string) string {
	// Gemini suggestions only appear while typing, not readable from tmux
	return ""
}
