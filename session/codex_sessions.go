package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// codexSessionMeta represents the first line of a Codex JSONL session file
type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID             string          `json:"id"`
		SessionID      string          `json:"session_id"`
		CWD            string          `json:"cwd"`
		Source         json.RawMessage `json:"source"`
		ThreadSource   string          `json:"thread_source"`
		ParentThreadID string          `json:"parent_thread_id"`
	} `json:"payload"`
}

func (m codexSessionMeta) resumeID() string {
	if m.Payload.SessionID != "" {
		return m.Payload.SessionID
	}
	return m.Payload.ID
}

func (m codexSessionMeta) isRoot() bool {
	if m.Payload.ParentThreadID != "" || m.Payload.ThreadSource == "subagent" {
		return false
	}
	var sourceObject map[string]json.RawMessage
	if json.Unmarshal(m.Payload.Source, &sourceObject) == nil {
		if _, isSubagent := sourceObject["subagent"]; isSubagent {
			return false
		}
	}
	return true
}

func (m codexSessionMeta) isRootCLI() bool {
	if !m.isRoot() {
		return false
	}
	var source string
	return json.Unmarshal(m.Payload.Source, &source) == nil && source == "cli"
}

func codexSessionsDir() (string, error) {
	var root string
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		root = filepath.Join(codexHome, "sessions")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(homeDir, ".codex", "sessions")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = evaluated
	}
	return root, nil
}

// codexMessage represents a user message in the Codex JSONL session file
type codexMessage struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

// ListCodexSessions lists all Codex sessions for the given project path
func ListCodexSessions(projectPath string) ([]AgentSession, error) {
	// Try to list sessions from ~/.codex/sessions directory (note: sessions, not session)
	sessionDir, err := codexSessionsDir()
	if err != nil {
		return []AgentSession{}, nil
	}

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		return []AgentSession{}, nil
	}

	var sessions []AgentSession

	// Walk through ~/.codex/sessions/YYYY/MM/DD/*.jsonl files
	err = filepath.Walk(sessionDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Only process .jsonl files
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		// Parse the session file to get ID and first prompt
		sessionID, firstPrompt, cwd := parseCodexSession(path)
		if sessionID == "" {
			return nil // Skip if we couldn't parse
		}

		// Skip empty sessions (those with only system messages, no real user prompt)
		if firstPrompt == sessionID {
			return nil // No real prompt found - empty session
		}

		// Filter by CWD using path hierarchy matching
		// Accept if: CWD matches exactly, or CWD is ancestor of projectPath, or projectPath is ancestor of CWD
		if projectPath != "" && cwd != "" {
			// Normalize paths (add trailing slash for comparison)
			normalizedCWD := cwd
			if !strings.HasSuffix(normalizedCWD, "/") {
				normalizedCWD += "/"
			}
			normalizedProject := projectPath
			if !strings.HasSuffix(normalizedProject, "/") {
				normalizedProject += "/"
			}

			// Check if paths are related (one is ancestor of the other)
			if cwd != projectPath &&
				!strings.HasPrefix(normalizedProject, normalizedCWD) &&
				!strings.HasPrefix(normalizedCWD, normalizedProject) {
				return nil // Skip sessions from unrelated directories
			}
		}

		sessions = append(sessions, AgentSession{
			SessionID:    sessionID,
			FirstPrompt:  firstPrompt,
			LastPrompt:   firstPrompt, // We only read the first prompt
			MessageCount: 1,
			CreatedAt:    info.ModTime(),
			UpdatedAt:    info.ModTime(),
			AgentType:    AgentCodex,
		})

		return nil
	})

	if err != nil {
		return []AgentSession{}, nil
	}

	// Sort by modification time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// parseCodexSession parses a Codex JSONL file and extracts session ID, first prompt, and CWD
func parseCodexSession(path string) (sessionID, firstPrompt, cwd string) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// First line should be session_meta
		if lineNum == 1 {
			var meta codexSessionMeta
			if err := json.Unmarshal([]byte(line), &meta); err == nil {
				if meta.Type == "session_meta" && meta.isRoot() {
					sessionID = meta.Payload.ID
					cwd = meta.Payload.CWD
				}
			}
			continue
		}

		// Look for first user message
		if firstPrompt == "" {
			var msg codexMessage
			if err := json.Unmarshal([]byte(line), &msg); err == nil {
				if msg.Type == "response_item" && msg.Payload.Type == "message" && msg.Payload.Role == "user" {
					// Get the first text content
					for _, content := range msg.Payload.Content {
						if content.Type == "input_text" && content.Text != "" {
							// Skip AGENTS.md instructions and environment context
							if !strings.HasPrefix(content.Text, "# AGENTS.md") &&
								!strings.HasPrefix(content.Text, "<environment_context>") {
								firstPrompt = content.Text
								// Limit length for display
								if len(firstPrompt) > 100 {
									firstPrompt = firstPrompt[:97] + "..."
								}
								break
							}
						}
					}
				}
			}
		}

		// Stop after finding both
		if sessionID != "" && firstPrompt != "" {
			break
		}
	}

	// If no first prompt found, use session ID
	if firstPrompt == "" {
		firstPrompt = sessionID
	}

	return sessionID, firstPrompt, cwd
}
