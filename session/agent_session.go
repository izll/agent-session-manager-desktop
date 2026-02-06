package session

import "time"

// AgentSession is a generic session structure for any agent type
type AgentSession struct {
	SessionID    string    `json:"session_id"`
	FirstPrompt  string    `json:"first_prompt"`
	LastPrompt   string    `json:"last_prompt"`
	Summary      string    `json:"summary,omitempty"`      // Session summary (from session file)
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	AgentType    AgentType `json:"agent_type"`
	ProjectPath  string    `json:"project_path,omitempty"` // Project directory name for display
}
