package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListOpenCodeSessions lists all OpenCode sessions for the given project path
func ListOpenCodeSessions(projectPath string) ([]AgentSession, error) {
	// OpenCode stores sessions at ~/.local/share/opencode/storage/session
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []AgentSession{}, nil
	}

	sessionDir := filepath.Join(homeDir, ".local", "share", "opencode", "storage", "session")
	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		return []AgentSession{}, nil
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return []AgentSession{}, nil
	}

	var sessions []AgentSession

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Use filename (without extension) as session ID
		sessionID := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

		sessions = append(sessions, AgentSession{
			SessionID:    sessionID,
			FirstPrompt:  sessionID, // We don't have access to actual prompts without parsing
			LastPrompt:   sessionID,
			MessageCount: 1,
			CreatedAt:    info.ModTime(),
			UpdatedAt:    info.ModTime(),
			AgentType:    AgentOpenCode,
		})
	}

	// Sort by modification time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}
