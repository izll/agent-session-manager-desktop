package session

import (
	"time"
)

// ListAmazonQSessions lists all Amazon Q sessions for the given project path
// Note: Amazon Q automatically saves conversations by working directory
// and resumes with "q chat --resume" without needing session IDs
func ListAmazonQSessions(projectPath string) ([]AgentSession, error) {
	// Amazon Q doesn't provide a way to list sessions via CLI
	// It automatically resumes the last session for the current working directory
	// So we return an empty list - the user can just use "resume" which will
	// automatically pick up the last session for this directory
	
	// If there might be a saved session for this directory, return a placeholder
	// This allows the UI to show the resume option
	return []AgentSession{
		{
			SessionID:    "auto",
			FirstPrompt:  "Resume last session for this directory",
			LastPrompt:   "Resume last session for this directory",
			MessageCount: 0,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			AgentType:    AgentAmazonQ,
		},
	}, nil
}
