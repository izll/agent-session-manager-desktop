package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionLine struct {
	Type        string `json:"type"`
	SessionID   string `json:"sessionId"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	AgentID     string `json:"agentId"`
	Summary     string `json:"summary"` // For type:"summary" entries
	Message     *struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	} `json:"message"`
}

// isValidUUID checks if a string looks like a UUID (basic check)
func isValidUUID(s string) bool {
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func GetClaudeProjectDir(projectPath string) string {
	homeDir, _ := os.UserHomeDir()

	// Resolve symlinks to get the real path (Claude does this)
	realPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realPath
	}

	// Convert path to Claude's format: /home/user/project_name -> -home-user-project-name
	// Claude replaces: / -> -, _ -> -, space -> -, and accented chars -> -
	var result strings.Builder
	for _, r := range projectPath {
		if r == '/' || r == '_' || r == ' ' {
			result.WriteRune('-')
		} else if r > 127 {
			// Non-ASCII characters (accented letters, etc.) -> -
			result.WriteRune('-')
		} else {
			result.WriteRune(r)
		}
	}
	sanitized := result.String()
	if strings.HasPrefix(sanitized, "-") {
		sanitized = sanitized[1:] // Remove leading dash
	}
	sanitized = "-" + sanitized // Add back the leading dash Claude uses
	return filepath.Join(homeDir, ".claude", "projects", sanitized)
}

func ListAgentSessions(projectPath string) ([]AgentSession, error) {
	// Use PREFIX matching like Claude Code does
	// /home/izll matches /home/izll, /home/izll/NetBeansProjects/*, etc.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	projectDirs, err := os.ReadDir(projectsDir)
	if os.IsNotExist(err) {
		return []AgentSession{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	// Resolve symlinks
	realProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realProjectPath
	}

	// Load history display texts with prefix matching
	historyDisplays := loadHistoryDisplaysPrefix(projectPath)

	var sessions []AgentSession

	// Iterate all project directories and find matching ones
	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}

		// Convert sanitized dir name back to path for comparison
		// -home-izll-NetBeansProjects-foo -> /home/izll/NetBeansProjects/foo
		dirPath := unsanitizePath(projectDir.Name())

		// Check prefix match
		if !strings.HasPrefix(dirPath, projectPath) {
			continue
		}

		claudeDir := filepath.Join(projectsDir, projectDir.Name())
		entries, err := os.ReadDir(claudeDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}

			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")

			// Skip non-UUID files
			if !isValidUUID(sessionID) {
				continue
			}

			sessionPath := filepath.Join(claudeDir, entry.Name())

			session, err := parseSessionFile(sessionPath, sessionID)
			if err != nil {
				continue
			}

			// Only include sessions with messages
			if session.MessageCount > 0 {
				// Use display from history if available
				if display, ok := historyDisplays[sessionID]; ok && display != "" {
					session.LastPrompt = display
				} else if session.Summary != "" {
					session.LastPrompt = session.Summary
				}
				session.ProjectPath = dirPath
				sessions = append(sessions, *session)
			}
		}
	}

	// Sort by UpdatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// unsanitizePath converts a sanitized directory name back to a path
// -home-izll-NetBeansProjects -> /home/izll/NetBeansProjects
func unsanitizePath(sanitized string) string {
	if sanitized == "" {
		return ""
	}
	// Remove leading dash and replace remaining dashes with slashes
	// But be careful with consecutive dashes (escaped dashes in original path)
	result := strings.ReplaceAll(sanitized, "-", "/")
	if strings.HasPrefix(result, "/") {
		return result
	}
	return "/" + result
}

// loadHistoryDisplaysPrefix loads display texts for sessions matching project prefix
func loadHistoryDisplaysPrefix(projectPath string) map[string]string {
	result := make(map[string]string)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return result
	}

	historyPath := filepath.Join(homeDir, ".claude", "history.jsonl")
	file, err := os.Open(historyPath)
	if err != nil {
		return result
	}
	defer file.Close()

	// Resolve symlinks
	realProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realProjectPath
	}

	type historyData struct {
		time    int64
		display string
	}
	sessionData := make(map[string]historyData)

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry struct {
			Timestamp int64  `json:"timestamp"`
			SessionID string `json:"sessionId"`
			Project   string `json:"project"`
			Display   string `json:"display"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.SessionID == "" || entry.Project == "" {
			continue
		}

		entryProject := entry.Project
		realEntryPath, err := filepath.EvalSymlinks(entryProject)
		if err == nil {
			entryProject = realEntryPath
		}

		// PREFIX match for this project
		if strings.HasPrefix(entryProject, projectPath) {
			if entry.Timestamp > sessionData[entry.SessionID].time {
				sessionData[entry.SessionID] = historyData{
					time:    entry.Timestamp,
					display: entry.Display,
				}
			}
		}
	}

	for id, data := range sessionData {
		result[id] = data.display
	}

	return result
}

// ListAllClaudeSessions returns all Claude sessions from all projects globally
// This mimics Claude Code's --resume behavior which searches across all projects
func ListAllClaudeSessions() ([]AgentSession, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	projectDirs, err := os.ReadDir(projectsDir)
	if os.IsNotExist(err) {
		return []AgentSession{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	var sessions []AgentSession

	// Iterate through all project directories
	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}

		claudeDir := filepath.Join(projectsDir, projectDir.Name())
		entries, err := os.ReadDir(claudeDir)
		if err != nil {
			continue // Skip directories we can't read
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}

			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")

			// Skip non-UUID files (like agent-* files which are subagent sessions)
			if !isValidUUID(sessionID) {
				continue
			}

			sessionPath := filepath.Join(claudeDir, entry.Name())

			session, err := parseSessionFile(sessionPath, sessionID)
			if err != nil {
				continue // Skip invalid files
			}
			// Only include sessions with at least one real user message
			if session.MessageCount > 0 && session.FirstPrompt != "" {
				// Add project path info for display
				session.ProjectPath = projectDir.Name()
				sessions = append(sessions, *session)
			}
		}
	}

	// Sort by UpdatedAt descending (newest first) - global sort
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

func parseSessionFile(path string, sessionID string) (*AgentSession, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &AgentSession{
		SessionID: sessionID,
	}

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large lines (some assistant responses can be >4MB)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var firstUserMessage string
	var lastUserMessage string
	var firstTimestamp time.Time
	var lastTimestamp time.Time
	var latestSummary string
	messageCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var sl sessionLine
		if err := json.Unmarshal([]byte(line), &sl); err != nil {
			continue
		}

		// Capture summary entries (latest wins)
		if sl.Type == "summary" && sl.Summary != "" {
			latestSummary = sl.Summary
		}

		// Skip meta and sidechain messages (like Claude Code does)
		if sl.IsMeta || sl.IsSidechain {
			continue
		}

		// Count both user and assistant messages (like Claude Code)
		if sl.Type == "user" && sl.Message != nil {
			content := extractContent(sl.Message.Content)
			if content != "" {
				messageCount++
				ts, _ := time.Parse(time.RFC3339, sl.Timestamp)
				// Skip non-meaningful messages for first message detection
				isContinuation := strings.HasPrefix(content, "This session is being continued")
				isCommand := strings.HasPrefix(content, "<command-name>") || strings.HasPrefix(content, "<local-command")
				isTooShort := len(strings.TrimSpace(content)) <= 3
				if firstUserMessage == "" && !isContinuation && !isCommand && !isTooShort {
					firstUserMessage = content
					firstTimestamp = ts
				}
				lastUserMessage = content
				lastTimestamp = ts
			}
		} else if sl.Type == "assistant" && sl.Message != nil {
			// Count assistant messages with actual text content
			content := extractContent(sl.Message.Content)
			if content != "" {
				messageCount++
				ts, _ := time.Parse(time.RFC3339, sl.Timestamp)
				if lastTimestamp.IsZero() || ts.After(lastTimestamp) {
					lastTimestamp = ts
				}
			}
		}
	}

	session.FirstPrompt = truncateString(firstUserMessage, 80)
	session.LastPrompt = truncateString(lastUserMessage, 80)
	session.Summary = latestSummary
	session.MessageCount = messageCount
	session.CreatedAt = firstTimestamp
	session.UpdatedAt = lastTimestamp

	return session, nil
}

func extractContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		// Skip tool results and notifications
		if strings.HasPrefix(v, "<bash-notification>") ||
			strings.HasPrefix(v, "<tool_result>") ||
			strings.HasPrefix(v, "{\"tool_use_id\":") {
			return ""
		}
		return v
	case []interface{}:
		// Content can be array of content blocks
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				// Skip tool_result type blocks
				if t, ok := m["type"].(string); ok && t == "tool_result" {
					continue
				}
				if text, ok := m["text"].(string); ok {
					return text
				}
			}
		}
	}
	return ""
}

func truncateString(s string, maxLen int) string {
	// Remove newlines
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// GetClaudeStatusLine handles Claude Code's special UI with horizontal separator lines.
// If the input area (between two horizontal lines) has only 1 line (the prompt),
// it returns the content above the top separator instead of the prompt line.
func GetClaudeStatusLine(lines []string, stripANSIFunc func(string) string) string {
	// Spinner characters used by Claude
	spinnerChars := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◑◒◓"

	// Find horizontal line positions (lines with many ─ or ━ characters)
	var separatorIndices []int
	for idx, line := range lines {
		cleanLine := strings.TrimSpace(stripANSIFunc(line))
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━")
		if sepCount > 20 {
			separatorIndices = append(separatorIndices, idx)
		}
	}

	// Need at least 2 separators to detect input area
	if len(separatorIndices) < 2 {
		return ""
	}

	// Get the last two separators (they form the input area boundary)
	topSepIdx := separatorIndices[len(separatorIndices)-2]
	_ = separatorIndices[len(separatorIndices)-1] // bottomSepIdx not needed, we only look above topSep

	// Always look above the top separator for agent output.
	// Content between separators is always user input (prompt area).

	// Find spinner/thinking line first, then return the line BEFORE it (the actual content)
	spinnerLineIdx := -1
	for j := topSepIdx - 1; j >= 0 && j >= topSepIdx-15; j-- {
		line := lines[j]
		cleanLine := strings.TrimSpace(stripANSIFunc(line))
		if cleanLine == "" {
			continue
		}

		// Check if line contains spinner character
		hasSpinner := false
		for _, r := range spinnerChars {
			if strings.ContainsRune(cleanLine, r) {
				hasSpinner = true
				break
			}
		}

		// Also check for extended thinking indicators (✽/✻)
		if strings.HasPrefix(cleanLine, "✽") || strings.HasPrefix(cleanLine, "✻") {
			hasSpinner = true
		}

		if hasSpinner {
			// Skip completed status lines (spinner + "for" = finished, e.g. "Churned for 1m")
			cleanLineLower := strings.ToLower(cleanLine)
			if strings.Contains(cleanLineLower, " for ") {
				continue
			}
			spinnerLineIdx = j
			break
		}
	}

	// If spinner found, look for content BEFORE (above) it
	startIdx := topSepIdx - 1
	if spinnerLineIdx > 0 {
		startIdx = spinnerLineIdx - 1
	}

	// Search for meaningful content
	for j := startIdx; j >= 0 && j >= topSepIdx-15; j-- {
		line := lines[j]
		cleanLine := strings.TrimSpace(stripANSIFunc(line))
		if cleanLine == "" {
			continue
		}

		// Skip separator lines
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━")
		if sepCount > 20 {
			continue
		}

		// Skip UI elements
		if strings.HasPrefix(cleanLine, "╭") || strings.HasPrefix(cleanLine, "╰") {
			continue
		}

		// Skip tip lines and "next step" indicators
		if strings.HasPrefix(cleanLine, "└") || strings.HasPrefix(cleanLine, "Tip:") {
			continue
		}

		// Skip continuation lines (output after spinner, tips, etc.)
		// These lines contain ⎿ or start with "Next:"
		if strings.Contains(cleanLine, "⎿") || strings.HasPrefix(cleanLine, "Next:") {
			continue
		}

		// Skip lines that look like planned steps (indented with tree chars)
		if strings.HasPrefix(cleanLine, "├") || strings.HasPrefix(cleanLine, "│") {
			continue
		}

		// Skip completed timing lines (e.g. "Baked for", "Churned for")
		cleanLineLower := strings.ToLower(cleanLine)
		if strings.Contains(cleanLineLower, " for ") {
			continue
		}

		// Found actual content above input area
		return line
	}

	// No content found - return empty for fallback processing
	return ""
}
