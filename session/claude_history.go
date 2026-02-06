package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type historyEntry struct {
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"sessionId"`
	Project   string `json:"project"`
	Display   string `json:"display"` // The last message displayed (what Claude Code shows)
}

// GetRecentSessionsFromHistory returns sessions ordered by recent usage from history.jsonl
// This mimics Claude Code's --resume behavior which uses history, not file timestamps
func GetRecentSessionsFromHistory(projectPath string) ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	historyPath := filepath.Join(homeDir, ".claude", "history.jsonl")
	file, err := os.Open(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to open history: %w", err)
	}
	defer file.Close()

	// Map of sessionID -> latest timestamp for this project
	sessionTimes := make(map[string]int64)

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Resolve symlinks in project path (Claude does this)
	realProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realProjectPath
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry historyEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Skip entries without session ID or project
		if entry.SessionID == "" || entry.Project == "" {
			continue
		}

		// Check if this entry matches our project path
		entryProject := entry.Project
		// Resolve symlinks for comparison
		realEntryPath, err := filepath.EvalSymlinks(entryProject)
		if err == nil {
			entryProject = realEntryPath
		}

		if entryProject == projectPath {
			// Update latest timestamp for this session
			if entry.Timestamp > sessionTimes[entry.SessionID] {
				sessionTimes[entry.SessionID] = entry.Timestamp
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading history: %w", err)
	}

	// Sort sessions by timestamp (newest first)
	type sessionTime struct {
		id   string
		time int64
	}
	var sessions []sessionTime
	for id, ts := range sessionTimes {
		sessions = append(sessions, sessionTime{id, ts})
	}

	// Sort by time descending
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].time > sessions[i].time {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	// Extract session IDs in order
	var result []string
	for _, s := range sessions {
		result = append(result, s.id)
	}

	return result, nil
}

// sessionHistoryData stores both timestamp and display text from history
type sessionHistoryData struct {
	time         int64  // Latest timestamp (for sorting by recent activity)
	shortDisplay string // First short valid display (<= 3 chars, like "." or "hi")
	longDisplay  string // First meaningful long display (> 3 chars)
	project      string // Original project path for prefix matching
}

// isValidDisplay checks if a display has no control characters
func isValidDisplay(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	// Skip if has control characters in first 20 chars
	for i, c := range s {
		if i >= 20 {
			break
		}
		if c < 32 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
	}
	return true
}

// isMeaningfulDisplay checks if a display string is meaningful (not a command, not too short, no control chars)
// Claude Code uses a threshold of ~20 chars - shorter displays get replaced with summary
func isMeaningfulDisplay(s string) bool {
	s = strings.TrimSpace(s)
	if !isValidDisplay(s) {
		return false
	}
	// Skip commands
	if strings.HasPrefix(s, "/") {
		return false
	}
	// Skip short displays - Claude shows summary for these
	// Threshold ~20 chars based on observed behavior
	if len(s) <= 20 {
		return false
	}
	return true
}

// ListAgentSessionsByHistory returns sessions ordered by recent usage
// It combines two sources like Claude Code does:
// 1. history.jsonl - sessions used in this project (exact match)
// 2. project directory - sessions with conversation content (user/assistant/summary/system)
func ListAgentSessionsByHistory(projectPath string) ([]AgentSession, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Resolve symlinks in project path
	realProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realProjectPath
	}

	// Map of sessionID -> session data (combined from both sources)
	sessionData := make(map[string]sessionHistoryData)

	// Source 1: Read from history.jsonl (exact project match)
	historyPath := filepath.Join(homeDir, ".claude", "history.jsonl")
	if file, err := os.Open(historyPath); err == nil {
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var entry historyEntry
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

			// Use exact matching for resume (like Claude Code's --resume)
			if entryProject == projectPath {
				existing, exists := sessionData[entry.SessionID]
				trimmedDisplay := strings.TrimSpace(entry.Display)
				displayLen := len(trimmedDisplay)

				if !exists {
					// First entry for this session
					data := sessionHistoryData{
						time:    entry.Timestamp,
						project: entry.Project,
					}
					// Track short displays (<= 3 chars) if valid
					if displayLen > 0 && displayLen <= 3 && isValidDisplay(entry.Display) {
						data.shortDisplay = entry.Display
					}
					// Track long meaningful displays
					if isMeaningfulDisplay(entry.Display) {
						data.longDisplay = entry.Display
					}
					sessionData[entry.SessionID] = data
				} else {
					// Update latest timestamp for sorting
					if entry.Timestamp > existing.time {
						existing.time = entry.Timestamp
					}
					// Keep the FIRST short display (for sessions with only short prompts)
					if existing.shortDisplay == "" && displayLen > 0 && displayLen <= 3 && isValidDisplay(entry.Display) {
						existing.shortDisplay = entry.Display
					}
					// Keep the LAST long meaningful display (Claude shows most recent)
					if isMeaningfulDisplay(entry.Display) {
						existing.longDisplay = entry.Display
					}
					sessionData[entry.SessionID] = existing
				}
			}
		}
	}

	// Note: We only use history.jsonl as the source, matching Claude Code's --resume behavior
	// Session files without history entries are not shown (Claude Code works this way)

	// Sort sessions by timestamp
	type sessionTime struct {
		id           string
		time         int64
		shortDisplay string
		longDisplay  string
		project      string
	}
	var sorted []sessionTime
	for id, data := range sessionData {
		sorted = append(sorted, sessionTime{id, data.time, data.shortDisplay, data.longDisplay, data.project})
	}

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].time > sorted[i].time {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var sessions []AgentSession

	// Load sessions in order from history
	for _, st := range sorted {
		if !isValidUUID(st.id) {
			continue
		}

		// Use session's own project directory (not the base projectPath)
		claudeDir := GetClaudeProjectDir(st.project)
		sessionPath := filepath.Join(claudeDir, st.id+".jsonl")

		var sess *AgentSession

		if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
			// Session file doesn't exist - create entry from history data only
			// Claude Code still shows these in the resume list
			// Priority: longDisplay (meaningful) > shortDisplay > empty
			display := st.longDisplay
			if display == "" && st.shortDisplay != "" {
				display = st.shortDisplay
			}
			sess = &AgentSession{
				SessionID:    st.id,
				FirstPrompt:  display,
				LastPrompt:   display,
				MessageCount: 1, // Assume at least 1 message to show in list
				UpdatedAt:    timeFromMillis(st.time),
				ProjectPath:  st.project,
			}
		} else {
			// Session file exists - parse it
			session, err := parseSessionFile(sessionPath, st.id)
			if err != nil {
				continue
			}

			// Note: Claude Code shows all sessions from history, even if empty
			// Set MessageCount to 1 minimum for display
			if session.MessageCount == 0 {
				session.MessageCount = 1
			}

			// Override UpdatedAt with history timestamp (milliseconds to time)
			session.UpdatedAt = timeFromMillis(st.time)

			// Display priority (matching Claude Code behavior):
			// 1. Short display from history (<= 3 chars like "." or "hi") - show as-is
			// 2. Long display from history (first meaningful user input)
			// 3. Summary from session file (fallback)
			// 4. FirstPrompt from session file (final fallback)
			if st.shortDisplay != "" && st.longDisplay == "" {
				// Session only has short displays - show as-is
				session.LastPrompt = st.shortDisplay
			} else if st.longDisplay != "" {
				// Use first meaningful history display
				session.LastPrompt = st.longDisplay
			} else if session.Summary != "" {
				// No history display, use summary
				session.LastPrompt = session.Summary
			} else if st.shortDisplay != "" {
				// Fallback to short display if nothing else
				session.LastPrompt = st.shortDisplay
			} else if session.FirstPrompt != "" {
				// Final fallback to first prompt from session file
				session.LastPrompt = session.FirstPrompt
			}

			// Skip sessions with no real user/assistant content (like Claude Code does)
			// Sessions with only summary but 0 messages are filtered out
			if session.MessageCount == 0 {
				continue
			}
			// Store project path for grouping/display
			session.ProjectPath = st.project
			sess = session
		}

		sessions = append(sessions, *sess)
	}

	return sessions, nil
}

// timeFromMillis converts milliseconds timestamp to time.Time
func timeFromMillis(millis int64) time.Time {
	return time.Unix(millis/1000, (millis%1000)*1000000)
}

// GetActiveSessionIDFromHistory returns the most recent session ID for a project from history.jsonl
// If afterTime is not zero, only considers entries after that time.
func GetActiveSessionIDFromHistory(projectPath string, afterTime time.Time) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	historyPath := filepath.Join(homeDir, ".claude", "history.jsonl")
	file, err := os.Open(historyPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	// Resolve symlinks in project path (Claude does this)
	realProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = realProjectPath
	}

	// Convert afterTime to milliseconds for comparison
	afterTimeMs := int64(0)
	if !afterTime.IsZero() {
		afterTimeMs = afterTime.UnixMilli()
	}

	var latestSessionID string
	var latestTimestamp int64

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry historyEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.SessionID == "" || entry.Project == "" {
			continue
		}

		// Skip entries before afterTime
		if afterTimeMs > 0 && entry.Timestamp < afterTimeMs {
			continue
		}

		// Resolve symlinks for comparison
		entryProject := entry.Project
		realEntryPath, err := filepath.EvalSymlinks(entryProject)
		if err == nil {
			entryProject = realEntryPath
		}

		if entryProject == projectPath {
			if entry.Timestamp > latestTimestamp {
				latestTimestamp = entry.Timestamp
				latestSessionID = entry.SessionID
			}
		}
	}

	return latestSessionID
}

// GetSessionFileSnapshot returns a map of session ID -> modification time (unix millis)
// for all session files in the project directory
func GetSessionFileSnapshot(projectPath string) map[string]int64 {
	result := make(map[string]int64)
	claudeDir := GetClaudeProjectDir(projectPath)

	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		if !isValidUUID(sessionID) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result[sessionID] = info.ModTime().UnixMilli()
	}

	return result
}

// GetChangedSessionID compares current session files with a snapshot and returns
// the session ID that was modified or created after the snapshot
func GetChangedSessionID(projectPath string, snapshot map[string]int64) string {
	claudeDir := GetClaudeProjectDir(projectPath)

	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return ""
	}

	var changedID string
	var latestModTime int64
	var changedFiles []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		if !isValidUUID(sessionID) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}

		currentModTime := info.ModTime().UnixMilli()
		oldModTime, existed := snapshot[sessionID]

		// Check if file is new or modified
		if !existed || currentModTime > oldModTime {
			changedFiles = append(changedFiles, fmt.Sprintf("%s (old=%d, new=%d)", sessionID[:8], oldModTime, currentModTime))
			if currentModTime > latestModTime {
				latestModTime = currentModTime
				changedID = sessionID
			}
		}
	}

	// Debug log
	debugLog := fmt.Sprintf("GetChangedSessionID:\n  changedFiles=%d: %v\n  result=%s\n",
		len(changedFiles), changedFiles, changedID)
	os.WriteFile("/tmp/asmgr_changed.log", []byte(debugLog), 0644)

	return changedID
}

// GetActiveSessionFromDebugLogs finds the active session ID by checking debug logs
// that have a SessionStart entry after afterTime and matching with session files in the project folder
func GetActiveSessionFromDebugLogs(projectPath string, afterTime time.Time) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	debugDir := filepath.Join(homeDir, ".claude", "debug")
	projectDir := GetClaudeProjectDir(projectPath)

	entries, err := os.ReadDir(debugDir)
	if err != nil {
		return ""
	}

	debugLog := fmt.Sprintf("GetActiveSessionFromDebugLogs:\n  projectDir=%s\n  afterTime=%s\n",
		projectDir, afterTime.Format("15:04:05"))

	var candidates []struct {
		id        string
		startTime time.Time
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".txt") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".txt")
		if !isValidUUID(sessionID) {
			continue
		}

		// Check if corresponding session file exists in the project folder
		sessionFile := filepath.Join(projectDir, sessionID+".jsonl")
		if _, err := os.Stat(sessionFile); err != nil {
			continue
		}

		// Read the debug log and check for SessionStart entry after afterTime
		debugPath := filepath.Join(debugDir, name)
		startTime := getSessionStartTime(debugPath, afterTime)
		if startTime.IsZero() {
			continue
		}

		candidates = append(candidates, struct {
			id        string
			startTime time.Time
		}{sessionID, startTime})
		debugLog += fmt.Sprintf("  match: %s (started %s)\n", sessionID[:8], startTime.Format("15:04:05"))
	}

	debugLog += fmt.Sprintf("  candidates=%d\n", len(candidates))

	if len(candidates) == 0 {
		debugLog += "  result: none\n"
		os.WriteFile("/tmp/asmgr_debug_session.log", []byte(debugLog), 0644)
		return ""
	}

	// Return the one with the latest SessionStart time
	latest := candidates[0]
	for _, c := range candidates[1:] {
		if c.startTime.After(latest.startTime) {
			latest = c
		}
	}

	debugLog += fmt.Sprintf("  result: %s\n", latest.id)
	os.WriteFile("/tmp/asmgr_debug_session.log", []byte(debugLog), 0644)

	return latest.id
}

// getSessionStartTime parses a debug log file and returns the latest SessionStart timestamp
// that is after afterTime. Returns zero time if no valid SessionStart found.
func getSessionStartTime(debugPath string, afterTime time.Time) time.Time {
	file, err := os.Open(debugPath)
	if err != nil {
		return time.Time{}
	}
	defer file.Close()

	var latestStart time.Time
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Look for SessionStart entries
		if !strings.Contains(line, "SessionStart") {
			continue
		}

		// Parse timestamp from beginning of line (format: 2026-01-05T00:11:47.485Z)
		if len(line) < 24 {
			continue
		}
		tsStr := line[:24]
		ts, err := time.Parse("2006-01-02T15:04:05.000Z", tsStr)
		if err != nil {
			// Try without milliseconds
			if len(line) >= 20 {
				ts, err = time.Parse("2006-01-02T15:04:05Z", line[:20])
			}
			if err != nil {
				continue
			}
		}

		// Convert to local time for comparison
		ts = ts.Local()

		// Only consider entries after afterTime
		if !afterTime.IsZero() && ts.Before(afterTime) {
			continue
		}

		if ts.After(latestStart) {
			latestStart = ts
		}
	}

	return latestStart
}

// GetLatestModifiedSessionID returns the session ID from the most recently modified session file
// in the Claude project directory. If afterTime is not zero, only considers files modified after that time.
func GetLatestModifiedSessionID(projectPath string, afterTime time.Time) string {
	claudeDir := GetClaudeProjectDir(projectPath)

	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return ""
	}

	var latestID string
	var latestModTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		if !isValidUUID(sessionID) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime()
		if !afterTime.IsZero() && modTime.Before(afterTime) {
			continue
		}

		if modTime.After(latestModTime) {
			latestModTime = modTime
			latestID = sessionID
		}
	}

	return latestID
}

// hasConversationContent checks if a session file has actual conversation content
// (user, assistant, summary, or system entries - not just file-history-snapshot)
func hasConversationContent(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	conversationTypes := map[string]bool{
		"user":      true,
		"assistant": true,
		"summary":   true,
		"system":    true,
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if conversationTypes[entry.Type] {
			return true
		}
	}

	return false
}

// HasExistingClaudeSessions checks if there are any Claude sessions for a project
// (either in history or as session files)
func HasExistingClaudeSessions(projectPath string) bool {
	// First check history.jsonl for sessions with this project
	sessions, err := GetRecentSessionsFromHistory(projectPath)
	if err == nil && len(sessions) > 0 {
		return true
	}

	// Also check project directory for session files
	claudeDir := GetClaudeProjectDir(projectPath)
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		if isValidUUID(sessionID) {
			return true
		}
	}

	return false
}

// ListAllSessionsByHistory returns ALL sessions from ALL projects ordered by recent usage
// This matches Claude Code's default --resume behavior (global view)
func ListAllSessionsByHistory() ([]AgentSession, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	historyPath := filepath.Join(homeDir, ".claude", "history.jsonl")
	file, err := os.Open(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentSession{}, nil
		}
		return nil, fmt.Errorf("failed to open history: %w", err)
	}
	defer file.Close()

	// Map of sessionID -> latest history data (global, all projects)
	sessionData := make(map[string]sessionHistoryData)
	sessionProject := make(map[string]string) // sessionID -> project path

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry historyEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.SessionID == "" || entry.Project == "" {
			continue
		}

		existing, exists := sessionData[entry.SessionID]
		trimmedDisplay := strings.TrimSpace(entry.Display)
		displayLen := len(trimmedDisplay)

		if !exists {
			data := sessionHistoryData{
				time:    entry.Timestamp,
				project: entry.Project,
			}
			if displayLen > 0 && displayLen <= 3 && isValidDisplay(entry.Display) {
				data.shortDisplay = entry.Display
			}
			if isMeaningfulDisplay(entry.Display) {
				data.longDisplay = entry.Display
			}
			sessionData[entry.SessionID] = data
			sessionProject[entry.SessionID] = entry.Project
		} else {
			if entry.Timestamp > existing.time {
				existing.time = entry.Timestamp
			}
			if existing.shortDisplay == "" && displayLen > 0 && displayLen <= 3 && isValidDisplay(entry.Display) {
				existing.shortDisplay = entry.Display
			}
			if existing.longDisplay == "" && isMeaningfulDisplay(entry.Display) {
				existing.longDisplay = entry.Display
			}
			sessionData[entry.SessionID] = existing
			sessionProject[entry.SessionID] = entry.Project
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading history: %w", err)
	}

	// Sort sessions by timestamp
	type sessionTimeGlobal struct {
		id           string
		time         int64
		shortDisplay string
		longDisplay  string
		project      string
	}
	var sorted []sessionTimeGlobal
	for id, data := range sessionData {
		sorted = append(sorted, sessionTimeGlobal{id, data.time, data.shortDisplay, data.longDisplay, sessionProject[id]})
	}

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].time > sorted[i].time {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var sessions []AgentSession

	// Load sessions in order from history
	for _, st := range sorted {
		if !isValidUUID(st.id) {
			continue
		}

		// Get the Claude project directory for this session's project
		claudeDir := GetClaudeProjectDir(st.project)
		sessionPath := filepath.Join(claudeDir, st.id+".jsonl")

		if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
			continue
		}

		session, err := parseSessionFile(sessionPath, st.id)
		if err != nil {
			continue
		}

		// Filter like Claude Code: need real content
		if session.MessageCount > 0 && session.FirstPrompt != "" &&
			session.FirstPrompt != "No prompt" &&
			!strings.HasPrefix(session.FirstPrompt, "This session is being continued") {
			// Override UpdatedAt with history timestamp
			session.UpdatedAt = timeFromMillis(st.time)

			// Display priority (matching Claude Code behavior)
			if st.shortDisplay != "" && st.longDisplay == "" {
				session.LastPrompt = st.shortDisplay
			} else if st.longDisplay != "" {
				session.LastPrompt = st.longDisplay
			} else if session.Summary != "" {
				session.LastPrompt = session.Summary
			} else if st.shortDisplay != "" {
				session.LastPrompt = st.shortDisplay
			} else if session.FirstPrompt != "" {
				session.LastPrompt = session.FirstPrompt
			}

			// Store project path for display
			session.ProjectPath = st.project
			sessions = append(sessions, *session)
		}
	}

	return sessions, nil
}
