package session

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sahilm/fuzzy"
)

const (
	historySessionFileLimit  = 16 << 20
	historyPreviewLimit      = 4 << 20
	historySearchEntryLimit  = 2 << 20
	historyIndexContentLimit = 64 << 20
	historyIndexEntryLimit   = 10000
	historySearchResultLimit = 200
)

// HistoryEntry represents a single searchable history item from any agent
type HistoryEntry struct {
	ID          string
	Agent       AgentType
	Content     string // Full conversation or command (for search)
	Snippet     string // Highlighted excerpt for display
	Path        string // Project path (if applicable)
	Timestamp   time.Time
	Score       int    // Relevance score for sorting
	SessionFile string // Full path to session file (for Claude - to load conversation)
	SessionID   string // Claude session ID (for resume)
}

// ConversationMessage represents a single message in a conversation
type ConversationMessage struct {
	Role      string    // "user" or "assistant"
	Content   string    // Message content
	Timestamp time.Time // Message timestamp
}

// LoadConversation loads the full conversation from a session file
func (e *HistoryEntry) LoadConversation() ([]ConversationMessage, error) {
	if e.SessionFile == "" {
		return nil, nil
	}

	// Handle Gemini sessions (JSON format)
	if e.Agent == AgentGemini {
		return e.loadGeminiConversation()
	}

	// Handle Claude sessions (JSONL format)
	file, err := os.Open(e.SessionFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var messages []ConversationMessage
	totalBytes := 0
	limited := &io.LimitedReader{R: file, N: historySessionFileLimit + 1}
	scanner := bufio.NewScanner(limited)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var entry claudeSessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		// Only include user and assistant messages
		if entry.Type == "user" || entry.Type == "assistant" {
			content := getMessageContent(entry.Message.Content, entry.Type)
			if content == "" {
				continue
			}
			ts := time.Now()
			if entry.Timestamp != "" {
				if parsed, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
					ts = parsed
				}
			}
			if totalBytes+len(content) > historyPreviewLimit {
				return nil, fmt.Errorf("conversation preview exceeds %d bytes", historyPreviewLimit)
			}
			totalBytes += len(content)
			messages = append(messages, ConversationMessage{
				Role:      entry.Type,
				Content:   content,
				Timestamp: ts,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("history file exceeds %d bytes", historySessionFileLimit)
	}
	return messages, nil
}

// loadGeminiConversation loads conversation from a Gemini session JSON file
func (e *HistoryEntry) loadGeminiConversation() ([]ConversationMessage, error) {
	data, err := readLimitedHistoryFile(e.SessionFile, historySessionFileLimit)
	if err != nil {
		return nil, err
	}

	var session geminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	var messages []ConversationMessage
	totalBytes := 0
	for _, msg := range session.Messages {
		// Only include user and gemini messages
		if msg.Type != "user" && msg.Type != "gemini" {
			continue
		}

		role := "user"
		if msg.Type == "gemini" {
			role = "assistant"
		}

		ts := time.Now()
		if parsed, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
			ts = parsed
		}

		if totalBytes+len(msg.Content) > historyPreviewLimit {
			return nil, fmt.Errorf("conversation preview exceeds %d bytes", historyPreviewLimit)
		}
		totalBytes += len(msg.Content)
		messages = append(messages, ConversationMessage{
			Role:      role,
			Content:   msg.Content,
			Timestamp: ts,
		})
	}

	return messages, nil
}

// HistoryIndex manages the searchable history across all agents
type HistoryIndex struct {
	mu        sync.RWMutex
	entries   []HistoryEntry
	loaded    bool
	instances []*Instance // Live instances for terminal search
	loadBytes int
	loadCount int
}

// NewHistoryIndex creates a new history index
func NewHistoryIndex() *HistoryIndex {
	return &HistoryIndex{
		entries: make([]HistoryEntry, 0),
		loaded:  false,
	}
}

// SetInstances sets the live instances for terminal search
func (h *HistoryIndex) SetInstances(instances []*Instance) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.instances = append([]*Instance(nil), instances...)
	h.loaded = false
}

// IsLoaded returns true if history has been loaded
func (h *HistoryIndex) IsLoaded() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.loaded
}

// Load loads history from all available sources
func (h *HistoryIndex) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loadLocked()
}

func (h *HistoryIndex) loadLocked() error {
	h.entries = make([]HistoryEntry, 0)
	h.loadBytes = 0
	h.loadCount = 0

	// Load from each source
	claudeEntries := h.parseClaudeHistory()
	h.entries = append(h.entries, claudeEntries...)

	aiderEntries := h.parseAiderHistory()
	h.entries = append(h.entries, aiderEntries...)

	openCodeEntries := h.parseOpenCodeDB()
	h.entries = append(h.entries, openCodeEntries...)

	geminiEntries := h.parseGeminiHistory()
	h.entries = append(h.entries, geminiEntries...)

	terminalEntries := h.parseTerminalHistory()
	h.entries = append(h.entries, terminalEntries...)

	// Sort by timestamp (newest first)
	sort.Slice(h.entries, func(i, j int) bool {
		return h.entries[i].Timestamp.After(h.entries[j].Timestamp)
	})

	h.loaded = true
	return nil
}

func (h *HistoryIndex) appendHistoryEntry(entries *[]HistoryEntry, entry HistoryEntry) bool {
	if h.loadCount >= historyIndexEntryLimit || len(entry.Content) > historySearchEntryLimit ||
		h.loadBytes+len(entry.Content) > historyIndexContentLimit {
		return false
	}
	*entries = append(*entries, entry)
	h.loadBytes += len(entry.Content)
	h.loadCount++
	return true
}

// Search searches the history index for matching entries
// Falls back to fuzzy search if no exact matches found
func (h *HistoryIndex) Search(query string) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loaded {
		_ = h.loadLocked()
	}

	// Don't search with empty query
	if query == "" {
		return []HistoryEntry{}
	}

	// First try exact substring search
	results := h.substringSearch(query)

	// If no results, fall back to fuzzy search
	if len(results) == 0 {
		results = h.fuzzySearchLocked(query)
	}
	if len(results) > historySearchResultLimit {
		results = results[:historySearchResultLimit]
	}

	return results
}

// substringSearch performs exact substring matching
func (h *HistoryIndex) substringSearch(query string) []HistoryEntry {
	queryLower := strings.ToLower(query)
	var results []HistoryEntry

	for _, entry := range h.entries {
		content := strings.ToLower(entry.Content)
		if strings.Contains(content, queryLower) {
			entryCopy := entry
			entryCopy.Snippet = h.extractSnippet(entry.Content, query)
			results = append(results, entryCopy)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	return results
}

// fuzzySource implements fuzzy.Source interface for history entries
type fuzzySource struct {
	entries []HistoryEntry
}

func (s fuzzySource) String(i int) string {
	// Use first 500 chars of content for fuzzy matching
	content := s.entries[i].Content
	if len(content) > 500 {
		content = content[:500]
	}
	return content
}

func (s fuzzySource) Len() int {
	return len(s.entries)
}

// FuzzySearch performs fuzzy matching with typo tolerance
func (h *HistoryIndex) FuzzySearch(query string) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.fuzzySearchLocked(query)
}

func (h *HistoryIndex) fuzzySearchLocked(query string) []HistoryEntry {
	if len(h.entries) == 0 || query == "" {
		return nil
	}

	source := fuzzySource{entries: h.entries}
	matches := fuzzy.FindFrom(query, source)

	var results []HistoryEntry
	for _, match := range matches {
		entry := h.entries[match.Index]
		entry.Score = match.Score
		entry.Snippet = h.extractSnippet(entry.Content, query)
		results = append(results, entry)
	}

	// Sort by timestamp (newest first) - fuzzy score is secondary
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	return results
}

// GetEntry returns only an entry that this index discovered from the current
// ASMGR session set. Callers never need to trust a file path supplied back by
// the webview to load a preview.
func (h *HistoryIndex) GetEntry(id string) (HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, entry := range h.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return HistoryEntry{}, false
}

// extractSnippet extracts a relevant snippet around the query match
func (h *HistoryIndex) extractSnippet(content, query string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, query)
	if idx == -1 {
		// Return beginning if no match found
		if len(content) > 100 {
			return content[:100] + "..."
		}
		return content
	}

	// Extract context around match
	start := idx - 30
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 70
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]

	// Clean up snippet
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	snippet = strings.ReplaceAll(snippet, "\t", " ")
	for strings.Contains(snippet, "  ") {
		snippet = strings.ReplaceAll(snippet, "  ", " ")
	}

	// Remove all agent prefixes from snippet for cleaner display
	snippet = strings.ReplaceAll(snippet, "User: ", "")
	snippet = strings.ReplaceAll(snippet, "Gemini: ", "")
	snippet = strings.ReplaceAll(snippet, "Assistant: ", "")
	snippet = strings.ReplaceAll(snippet, "Claude: ", "")
	snippet = strings.ReplaceAll(snippet, "Aider: ", "")
	// Clean up any resulting double spaces
	for strings.Contains(snippet, "  ") {
		snippet = strings.ReplaceAll(snippet, "  ", " ")
	}

	// Add ellipsis if truncated at start
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return strings.TrimSpace(snippet)
}

// claudeHistoryEntry represents an entry in Claude's history.jsonl
type claudeHistoryEntry struct {
	Display   string `json:"display"`
	Project   string `json:"project"`
	Timestamp int64  `json:"timestamp"`
}

// claudeSessionEntry represents a message in Claude's session files
type claudeSessionEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // Can be string (user) or array (assistant)
	} `json:"message"`
	CWD       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"` // ISO 8601 format: "2025-12-04T23:02:48.441Z"
}

// claudeContentBlock represents a content block in assistant messages
type claudeContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// getMessageContent extracts text content from a Claude message
func getMessageContent(raw json.RawMessage, msgType string) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first (user messages)
	var strContent string
	if err := json.Unmarshal(raw, &strContent); err == nil {
		return strContent
	}

	// Try array (assistant messages)
	var blocks []claudeContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var result strings.Builder
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(block.Text)
			}
		}
		return result.String()
	}

	return ""
}

// parseClaudeHistory parses Claude's session files from ASMGR project directories only
func (h *HistoryIndex) parseClaudeHistory() []HistoryEntry {
	var entries []HistoryEntry
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return entries
	}

	claudeDir := filepath.Join(homeDir, ".claude")
	projectsDir := filepath.Join(claudeDir, "projects")

	// Build set of Claude directory names from ASMGR instance paths
	// Claude uses URL-encoded paths like: -home-izll-NetBeansProjects-project
	knownDirs := make(map[string]bool)
	for _, inst := range h.instances {
		if inst.Path != "" {
			// Convert path to Claude's directory naming: /home/izll/foo -> -home-izll-foo
			claudeDirName := strings.ReplaceAll(inst.Path, "/", "-")
			if strings.HasPrefix(claudeDirName, "-") {
				claudeDirName = claudeDirName[1:] // Remove leading dash
			}
			claudeDirName = "-" + claudeDirName // Add back single leading dash
			knownDirs[claudeDirName] = true
		}
	}

	// Parse only directories matching ASMGR sessions
	if dirs, err := readDirAtMost(projectsDir, maxDiscoveryDirectoryEntries); err == nil {
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			// Only process if this directory matches an ASMGR session
			if !knownDirs[dir.Name()] {
				continue
			}
			projPath := filepath.Join(projectsDir, dir.Name())
			if files, err := readDirAtMost(projPath, maxDiscoveryDirectoryEntries); err == nil {
				for _, file := range files {
					if !strings.HasSuffix(file.Name(), ".jsonl") {
						continue
					}
					sessionFile := filepath.Join(projPath, file.Name())
					h.parseClaudeSessionFile(sessionFile, &entries)
				}
			}
		}
	}

	return entries
}

// parseClaudeSessionFile parses a Claude session JSONL file
func (h *HistoryIndex) parseClaudeSessionFile(filePath string, entries *[]HistoryEntry) {
	if len(*entries) >= historyIndexEntryLimit {
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	limited := &io.LimitedReader{R: file, N: historySessionFileLimit + 1}
	scanner := bufio.NewScanner(limited)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var sessionID string
	var cwd string

	for scanner.Scan() {
		if len(*entries) >= historyIndexEntryLimit {
			break
		}
		var entry claudeSessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
			// Capture session ID from first entry
			if sessionID == "" && entry.SessionID != "" {
				sessionID = entry.SessionID
			}
			// Capture CWD
			if cwd == "" && entry.CWD != "" {
				cwd = entry.CWD
			}

			if entry.Type == "user" {
				content := getMessageContent(entry.Message.Content, entry.Type)
				if content == "" {
					continue
				}
				ts := time.Now()
				if entry.Timestamp != "" {
					if parsed, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
						ts = parsed
					}
				}
				if !h.appendHistoryEntry(entries, HistoryEntry{
					ID:          generateHistoryID(),
					Agent:       AgentClaude,
					Content:     content,
					Path:        cwd,
					Timestamp:   ts,
					SessionFile: filePath,
					SessionID:   sessionID,
				}) {
					return
				}
			}
		}
	}
}

// aiderHistoryEntry represents an entry in Aider's history
type aiderHistoryEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// parseAiderHistory parses Aider's history files
func (h *HistoryIndex) parseAiderHistory() []HistoryEntry {
	var entries []HistoryEntry
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return entries
	}

	// Find Aider session path from ASMGR instances (Aider has global history, no project info)
	aiderPath := ""
	for _, inst := range h.instances {
		if inst.Agent == AgentAider && inst.Path != "" {
			aiderPath = inst.Path
			break // Use first Aider session's path
		}
	}

	// Check multiple possible locations
	aiderPaths := []string{
		filepath.Join(homeDir, ".aider", "history.jsonl"),
		filepath.Join(homeDir, ".aider.history"),
	}

	for _, historyFile := range aiderPaths {
		if file, err := os.Open(historyFile); err == nil {
			defer file.Close()
			limited := &io.LimitedReader{R: file, N: historySessionFileLimit + 1}
			scanner := bufio.NewScanner(limited)
			buf := make([]byte, 0, 64*1024)
			scanner.Buffer(buf, 1024*1024)

			for scanner.Scan() {
				if len(entries) >= historyIndexEntryLimit {
					break
				}
				line := scanner.Text()
				// Try JSON format first
				var entry aiderHistoryEntry
				if err := json.Unmarshal([]byte(line), &entry); err == nil {
					if entry.Role == "user" && entry.Content != "" {
						snippet := entry.Content
						if len(snippet) > 100 {
							snippet = snippet[:100] + "..."
						}
						if !h.appendHistoryEntry(&entries, HistoryEntry{
							ID:        generateHistoryID(),
							Agent:     AgentAider,
							Content:   entry.Content,
							Snippet:   snippet,
							Path:      aiderPath,
							Timestamp: time.Now(), // Aider doesn't store timestamps
						}) {
							return entries
						}
					}
				} else {
					// Plain text format
					if strings.TrimSpace(line) != "" {
						snippet := line
						if len(snippet) > 100 {
							snippet = snippet[:100] + "..."
						}
						if !h.appendHistoryEntry(&entries, HistoryEntry{
							ID:        generateHistoryID(),
							Agent:     AgentAider,
							Content:   line,
							Snippet:   snippet,
							Path:      aiderPath,
							Timestamp: time.Now(),
						}) {
							return entries
						}
					}
				}
			}
		}
	}

	return entries
}

// parseOpenCodeDB parses OpenCode's SQLite databases (stored locally in each project)
func (h *HistoryIndex) parseOpenCodeDB() []HistoryEntry {
	var entries []HistoryEntry

	// OpenCode stores DB locally in each project at .opencode/opencode.db
	// Collect paths from ASMGR instances that have OpenCode
	dbPaths := make(map[string]string) // dbPath -> projectPath
	for _, inst := range h.instances {
		if inst.Path != "" {
			localDB := filepath.Join(inst.Path, ".opencode", "opencode.db")
			if _, err := os.Stat(localDB); err == nil {
				dbPaths[localDB] = inst.Path
			}
		}
	}

	// Parse each database
	for dbPath, projectPath := range dbPaths {
		dbEntries := h.parseOpenCodeDBFile(dbPath, projectPath)
		entries = append(entries, dbEntries...)
	}

	return entries
}

// parseOpenCodeDBFile parses a single OpenCode database file
func (h *HistoryIndex) parseOpenCodeDBFile(dbPath, projectPath string) []HistoryEntry {
	var entries []HistoryEntry

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return entries
	}
	defer db.Close()

	// Query messages with session info (both user and assistant)
	// Note: created_at is Unix timestamp in milliseconds, parts is JSON
	query := `
		SELECT m.parts, m.role, m.created_at, s.title
		FROM messages m
		LEFT JOIN sessions s ON m.session_id = s.id
		WHERE m.role IN ('user', 'assistant') AND length(m.parts) <= ?
		ORDER BY m.created_at DESC
		LIMIT 500
	`

	rows, err := db.Query(query, historySearchEntryLimit)
	if err != nil {
		return entries
	}
	defer rows.Close()

	for rows.Next() {
		var partsJSON, role string
		var createdAtMs int64
		var title sql.NullString
		if err := rows.Scan(&partsJSON, &role, &createdAtMs, &title); err == nil {
			// Convert Unix milliseconds to time
			ts := time.UnixMilli(createdAtMs)

			// Parse JSON parts to extract text content
			content := extractOpenCodeText(partsJSON)
			if content == "" {
				continue
			}

			// Use project path, fallback to session title
			path := projectPath
			if path == "" && title.Valid && title.String != "" {
				path = title.String
			}

			snippet := content
			if len(snippet) > 100 {
				snippet = snippet[:100] + "..."
			}

			if !h.appendHistoryEntry(&entries, HistoryEntry{
				ID:        generateHistoryID(),
				Agent:     AgentOpenCode,
				Content:   content,
				Snippet:   snippet,
				Path:      path,
				Timestamp: ts,
			}) {
				break
			}
		}
	}

	return entries
}

// extractOpenCodeText extracts text content from OpenCode's JSON parts format
func extractOpenCodeText(partsJSON string) string {
	// OpenCode stores parts as: [{"type":"text","data":{"text":"..."}}]
	var parts []struct {
		Type string `json:"type"`
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(partsJSON), &parts); err != nil {
		return ""
	}

	var texts []string
	for _, part := range parts {
		if part.Type == "text" && part.Data.Text != "" {
			texts = append(texts, part.Data.Text)
		}
	}

	return strings.Join(texts, " ")
}

// buildGeminiHashMap builds a map from SHA256 hashes to paths from known instances
func (h *HistoryIndex) buildGeminiHashMap() map[string]string {
	hashMap := make(map[string]string)
	for _, inst := range h.instances {
		if inst.Path == "" {
			continue
		}
		// Compute SHA256 hash of the path (same as Gemini does)
		hash := sha256.Sum256([]byte(inst.Path))
		hashStr := hex.EncodeToString(hash[:])
		hashMap[hashStr] = inst.Path
	}
	return hashMap
}

// geminiSession represents a Gemini CLI session file
type geminiSession struct {
	SessionID   string          `json:"sessionId"`
	ProjectHash string          `json:"projectHash"`
	StartTime   string          `json:"startTime"`
	LastUpdated string          `json:"lastUpdated"`
	Messages    []geminiMessage `json:"messages"`
}

// geminiMessage represents a message in a Gemini session
type geminiMessage struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"` // "user", "gemini", "error", "info"
	Content   string `json:"content"`
}

// parseGeminiHistory parses Gemini CLI session files
func (h *HistoryIndex) parseGeminiHistory() []HistoryEntry {
	var entries []HistoryEntry
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return entries
	}

	geminiDir := filepath.Join(homeDir, ".gemini", "tmp")

	// Build hash-to-path map from known instances
	hashMap := h.buildGeminiHashMap()

	// Walk through all project directories
	projectDirs, err := readDirAtMost(geminiDir, maxDiscoveryDirectoryEntries)
	if err != nil {
		return entries
	}

	for _, projectDir := range projectDirs {
		if len(entries) >= historyIndexEntryLimit {
			break
		}
		if !projectDir.IsDir() {
			continue
		}

		// Try to look up path from known instances (directory name is SHA256 hash)
		projectPath := hashMap[projectDir.Name()]
		if projectPath == "" {
			// The Gemini store is global. Only hashed directories belonging to
			// a current ASMGR session may enter this project-scoped index.
			continue
		}

		chatsDir := filepath.Join(geminiDir, projectDir.Name(), "chats")
		chatFiles, err := readDirAtMost(chatsDir, maxDiscoveryDirectoryEntries)
		if err != nil {
			continue
		}

		for _, chatFile := range chatFiles {
			if len(entries) >= historyIndexEntryLimit {
				break
			}
			if !strings.HasPrefix(chatFile.Name(), "session-") || !strings.HasSuffix(chatFile.Name(), ".json") {
				continue
			}

			sessionPath := filepath.Join(chatsDir, chatFile.Name())
			data, err := readLimitedHistoryFile(sessionPath, historySessionFileLimit)
			if err != nil {
				continue
			}

			var session geminiSession
			if err := json.Unmarshal(data, &session); err != nil {
				continue
			}

			// Build conversation content from messages
			var content strings.Builder
			var lastTimestamp time.Time

			for _, msg := range session.Messages {
				// Only include user and gemini messages (skip error/info)
				if msg.Type != "user" && msg.Type != "gemini" {
					continue
				}

				prefix := "User: "
				if msg.Type == "gemini" {
					prefix = "Gemini: "
				}
				if content.Len()+len(prefix)+len(msg.Content)+2 > historySearchEntryLimit {
					break
				}
				content.WriteString(prefix)
				content.WriteString(msg.Content)
				content.WriteString("\n\n")

				// Parse timestamp
				if ts, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
					if ts.After(lastTimestamp) {
						lastTimestamp = ts
					}
				}
			}

			contentStr := content.String()
			if contentStr == "" {
				continue
			}

			// Create snippet from first user message
			snippet := ""
			for _, msg := range session.Messages {
				if msg.Type == "user" && msg.Content != "" {
					snippet = msg.Content
					if len(snippet) > 100 {
						snippet = snippet[:100] + "..."
					}
					break
				}
			}

			if !h.appendHistoryEntry(&entries, HistoryEntry{
				ID:          generateHistoryID(),
				Agent:       AgentGemini,
				Content:     contentStr,
				Snippet:     snippet,
				Path:        projectPath,
				Timestamp:   lastTimestamp,
				SessionFile: sessionPath,
				SessionID:   session.SessionID,
			}) {
				return entries
			}
		}
	}

	return entries
}

// parseTerminalHistory captures terminal content from live session terminal tabs via tmux
func (h *HistoryIndex) parseTerminalHistory() []HistoryEntry {
	var entries []HistoryEntry

	if len(h.instances) == 0 {
		return entries
	}

	for _, inst := range h.instances {
		// Skip non-running sessions
		if inst.Status != StatusRunning {
			continue
		}

		// Check each followed window for terminal tabs
		for _, fw := range inst.FollowedWindows {
			if fw.Agent != AgentTerminal {
				continue
			}

			// Capture tmux pane content (last 500 lines)
			target := fmt.Sprintf("%s:%d", inst.TmuxSessionName(), fw.Index)
			output, err := captureTerminalPane(target, 500)
			if err != nil || strings.TrimSpace(output) == "" {
				continue
			}

			// Create a single entry for the terminal content
			tabName := fw.Name
			if tabName == "" {
				tabName = fmt.Sprintf("Terminal %d", fw.Index)
			}

			// Extract snippet (last few non-empty lines)
			snippet := extractTerminalSnippet(output, 100)

			if !h.appendHistoryEntry(&entries, HistoryEntry{
				ID:        generateHistoryID(),
				Agent:     AgentTerminal,
				Content:   output,
				Snippet:   snippet,
				Path:      inst.Path,
				Timestamp: time.Now(), // Terminal content is "live"
				SessionID: inst.ResumeSessionID,
			}) {
				return entries
			}
		}
	}

	return entries
}

// captureTerminalPane captures the scrollback buffer from a tmux pane
func captureTerminalPane(target string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	cmd := TmuxCommandContext(ctx, "capture-pane", "-t", target, "-p", "-S", fmt.Sprintf("-%d", lines))
	var output limitedHistoryOutput
	cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	if output.truncated {
		return "", fmt.Errorf("terminal history exceeds %d bytes", historySearchEntryLimit)
	}
	return string(output.data), nil
}

type limitedHistoryOutput struct {
	data      []byte
	truncated bool
}

func (w *limitedHistoryOutput) Write(p []byte) (int, error) {
	remaining := historySearchEntryLimit - len(w.data)
	if remaining > 0 {
		keep := min(remaining, len(p))
		w.data = append(w.data, p[:keep]...)
	}
	if len(p) > max(remaining, 0) {
		w.truncated = true
	}
	return len(p), nil
}

func readLimitedHistoryFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("history file exceeds %d bytes", limit)
	}
	return data, nil
}

// extractTerminalSnippet extracts the last meaningful lines from terminal output
func extractTerminalSnippet(content string, maxLen int) string {
	lines := strings.Split(content, "\n")

	// Find last non-empty lines
	var lastLines []string
	for i := len(lines) - 1; i >= 0 && len(lastLines) < 3; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			lastLines = append([]string{line}, lastLines...)
		}
	}

	snippet := strings.Join(lastLines, " ")
	if len(snippet) > maxLen {
		snippet = snippet[:maxLen-3] + "..."
	}
	return snippet
}

// parseTimestamp parses a Unix timestamp string
func parseTimestamp(s string) (int64, error) {
	s = strings.TrimSpace(s)
	var ts int64
	err := json.Unmarshal([]byte(s), &ts)
	if err != nil {
		return 0, err
	}
	return ts, nil
}

// generateHistoryID generates a simple unique ID for history entries
func generateHistoryID() string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102150405.000000000"), atomic.AddUint64(&historyIDCounter, 1))
}

var historyIDCounter uint64
