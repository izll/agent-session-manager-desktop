package session

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const openCodeSessionMetadataLimit = 1 << 20

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

	var sessions []AgentSession
	err = filepath.WalkDir(sessionDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > openCodeSessionMetadataLimit {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		data, readErr := io.ReadAll(io.LimitReader(file, openCodeSessionMetadataLimit+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > openCodeSessionMetadataLimit {
			return nil
		}
		var metadata struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
			Title     string `json:"title"`
			Time      struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			} `json:"time"`
		}
		if json.Unmarshal(data, &metadata) != nil || !pathWithinProject(metadata.Directory, projectPath) {
			// The storage directory is global. Records without a trustworthy
			// directory cannot safely be exposed in a project-scoped dialog.
			return nil
		}
		sessionID := metadata.ID
		if sessionID == "" {
			sessionID = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		if sessionID == "" {
			return nil
		}
		createdAt := info.ModTime()
		updatedAt := info.ModTime()
		if metadata.Time.Created > 0 {
			createdAt = time.UnixMilli(metadata.Time.Created)
		}
		if metadata.Time.Updated > 0 {
			updatedAt = time.UnixMilli(metadata.Time.Updated)
		}
		title := strings.TrimSpace(metadata.Title)
		if title == "" {
			title = sessionID
		}
		sessions = append(sessions, AgentSession{
			SessionID:    sessionID,
			FirstPrompt:  title,
			LastPrompt:   title,
			MessageCount: 1,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			AgentType:    AgentOpenCode,
			ProjectPath:  metadata.Directory,
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
