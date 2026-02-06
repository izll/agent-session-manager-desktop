package session

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ListGeminiSessions lists all Gemini sessions for the given project path
func ListGeminiSessions(projectPath string) ([]AgentSession, error) {
	// Run gemini --list-sessions in the project directory
	cmd := exec.Command("gemini", "--list-sessions")
	cmd.Dir = projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		// If command fails, assume no sessions (gemini not installed or no sessions)
		return []AgentSession{}, nil
	}

	return parseGeminiSessionList(string(output))
}

// parseGeminiSessionList parses the output of "gemini --list-sessions"
func parseGeminiSessionList(output string) ([]AgentSession, error) {
	// Pattern: "  1. List files in directory (6 minutes ago) [a1bd3012-6029-49ba-897b-8b0e83635d48]"
	re := regexp.MustCompile(`^\s+\d+\.\s+(.+?)\s+\(([^)]+)\)\s+\[([a-f0-9-]+)\]$`)

	lines := strings.Split(output, "\n")
	var sessions []AgentSession

	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		prompt := strings.TrimSpace(matches[1])
		relativeTime := matches[2]
		sessionID := matches[3]

		// Parse relative time to approximate timestamp
		updatedAt := parseRelativeTime(relativeTime)

		sessions = append(sessions, AgentSession{
			SessionID:    sessionID,
			FirstPrompt:  prompt,
			LastPrompt:   prompt,
			MessageCount: 1, // We don't know the exact count from --list-sessions
			CreatedAt:    updatedAt,
			UpdatedAt:    updatedAt,
			AgentType:    AgentGemini,
		})
	}

	// Sessions are already sorted by gemini (most recent first)
	// Reverse numbering: session 1 is most recent
	return sessions, nil
}

// parseRelativeTime converts relative time strings to approximate timestamps
func parseRelativeTime(relativeTime string) time.Time {
	now := time.Now()
	relativeTime = strings.ToLower(relativeTime)

	if strings.Contains(relativeTime, "just now") || strings.Contains(relativeTime, "now") {
		return now
	}

	// Parse patterns like "6 minutes ago", "2 hours ago", "3 days ago"
	var duration time.Duration
	if strings.Contains(relativeTime, "second") {
		var seconds int
		fmt.Sscanf(relativeTime, "%d", &seconds)
		duration = time.Duration(seconds) * time.Second
	} else if strings.Contains(relativeTime, "minute") {
		var minutes int
		fmt.Sscanf(relativeTime, "%d", &minutes)
		duration = time.Duration(minutes) * time.Minute
	} else if strings.Contains(relativeTime, "hour") {
		var hours int
		fmt.Sscanf(relativeTime, "%d", &hours)
		duration = time.Duration(hours) * time.Hour
	} else if strings.Contains(relativeTime, "day") {
		var days int
		fmt.Sscanf(relativeTime, "%d", &days)
		duration = time.Duration(days) * 24 * time.Hour
	} else if strings.Contains(relativeTime, "week") {
		var weeks int
		fmt.Sscanf(relativeTime, "%d", &weeks)
		duration = time.Duration(weeks) * 7 * 24 * time.Hour
	} else if strings.Contains(relativeTime, "month") {
		var months int
		fmt.Sscanf(relativeTime, "%d", &months)
		duration = time.Duration(months) * 30 * 24 * time.Hour
	}

	return now.Add(-duration)
}
