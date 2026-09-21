package session

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const openCodeSessionMetadataLimit = 1 << 20

// ListOpenCodeSessions lists all OpenCode sessions for the given project path
func ListOpenCodeSessions(projectPath string) ([]AgentSession, error) {
	return listOpenCodeSessionsFrom(localFiles{}, projectPath)
}

// listOpenCodeSessionsFrom is the same listing against a given machine.
func listOpenCodeSessionsFrom(files agentFiles, projectPath string) ([]AgentSession, error) {
	// OpenCode stores sessions at ~/.local/share/opencode/storage/session
	homeDir, err := files.home()
	if err != nil {
		return []AgentSession{}, nil
	}

	sessionDir := files.join(homeDir, ".local", "share", "opencode", "storage", "session")

	// Newest first and capped, so a store that has grown for years does not
	// have to be read in full to show the recent conversations.
	paths, err := files.findFiles(sessionDir, ".json", agentSessionScanLimit)
	if err != nil {
		return []AgentSession{}, nil
	}

	var sessions []AgentSession
	for _, path := range paths {
		// Each record is a title and a few timestamps; anything larger is not
		// what this thinks it is, and the read stops there either way.
		data, readErr := files.readHead(path, openCodeSessionMetadataLimit)
		if readErr != nil {
			continue
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
			continue
		}
		sessionID := metadata.ID
		if sessionID == "" {
			base := path
			if cut := strings.LastIndexAny(base, "/\\"); cut >= 0 {
				base = base[cut+1:]
			}
			sessionID = strings.TrimSuffix(base, ".json")
		}
		if sessionID == "" {
			continue
		}
		// The record's own timestamps. A file time would need a round trip per
		// file on a server, to order records that already carry their times.
		var createdAt, updatedAt time.Time
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
	}

	// Sort by modification time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}
