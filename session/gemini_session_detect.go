package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DetectGeminiSessionID finds the most recent Gemini session ID from the filesystem.
// Gemini creates session files at ~/.gemini/tmp/<projectIdentifier>/chats/session-*.json
// immediately on startup. The sessionId field in the JSON is used for --resume.
// excludeIDs are session IDs already assigned to other tabs, so we skip them.
func DetectGeminiSessionID(projectPath string, excludeIDs ...string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	excludeSet := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		if id != "" {
			excludeSet[id] = true
		}
	}

	identifiers := getGeminiProjectIdentifiers(projectPath, homeDir)

	for _, identifier := range identifiers {
		chatsDir := filepath.Join(homeDir, ".gemini", "tmp", identifier, "chats")

		entries, err := os.ReadDir(chatsDir)
		if err != nil {
			continue
		}

		// Filter session-*.json files and sort by modification time (newest first)
		type sessionFile struct {
			path    string
			modTime int64
		}
		var files []sessionFile

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, sessionFile{
				path:    filepath.Join(chatsDir, entry.Name()),
				modTime: info.ModTime().UnixNano(),
			})
		}

		if len(files) == 0 {
			continue
		}

		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime > files[j].modTime
		})

		// Read session files newest-first, skip already-assigned IDs
		for _, f := range files {
			sid := extractGeminiSessionID(f.path)
			if sid != "" && !excludeSet[sid] {
				return sid
			}
		}
	}

	return ""
}

// getGeminiProjectIdentifiers returns possible project identifiers for the given path.
// Gemini uses either legacy SHA-256 hash or modern slug-based identifiers.
func getGeminiProjectIdentifiers(projectPath string, homeDir string) []string {
	var identifiers []string

	// Legacy: SHA-256 hash of project path
	hash := sha256.Sum256([]byte(projectPath))
	identifiers = append(identifiers, hex.EncodeToString(hash[:]))

	// Modern: slug from projects.json (if it exists)
	projectsFile := filepath.Join(homeDir, ".gemini", "projects.json")
	data, err := os.ReadFile(projectsFile)
	if err == nil {
		slugs := findGeminiSlugs(data, projectPath)
		identifiers = append(identifiers, slugs...)
	}

	return identifiers
}

// findGeminiSlugs parses projects.json and finds slug identifiers for the given project path.
// projects.json format: {"projects": {"slug-name": {"path": "/abs/path", ...}, ...}}
func findGeminiSlugs(data []byte, projectPath string) []string {
	var projectsData struct {
		Projects map[string]struct {
			Path string `json:"path"`
		} `json:"projects"`
	}

	if err := json.Unmarshal(data, &projectsData); err != nil {
		return nil
	}

	var slugs []string
	for slug, proj := range projectsData.Projects {
		if proj.Path == projectPath {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

// extractGeminiSessionID reads a Gemini session JSON file and returns the sessionId field.
func extractGeminiSessionID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	// Only parse the minimal fields we need
	var session struct {
		SessionID string `json:"sessionId"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		log.Printf("[GeminiDetect] failed to parse %s: %v", filepath.Base(path), err)
		return ""
	}

	if session.SessionID != "" && isValidUUID(session.SessionID) {
		return session.SessionID
	}

	return ""
}
