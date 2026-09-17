package session

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// antigravitySessionListLimit caps how many conversations are read back. The
// store is global — every project's conversations land in one table — so a
// long-lived install can hold far more than a picker can show.
const antigravitySessionListLimit = 200

// antigravitySummariesPath returns ~/.gemini/antigravity-cli/conversation_summaries.db.
//
// Antigravity keeps one SQLite database per conversation under conversations/,
// plus this single summary table listing all of them. The summaries are what a
// picker needs — id, title, time, workspace — and reading one file beats
// opening a database per conversation.
func antigravitySummariesPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".gemini", "antigravity-cli", "conversation_summaries.db"), nil
}

// ListAntigravitySessions returns the conversations Antigravity has recorded
// for one project.
//
// The database is opened read-only. The CLI may be running against it at the
// same time, and this is a picker: it must never be the reason a live session
// loses a write.
func ListAntigravitySessions(projectPath string) ([]AgentSession, error) {
	dbPath, err := antigravitySummariesPath()
	if err != nil {
		return []AgentSession{}, nil
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		return []AgentSession{}, nil
	}

	db, err := sql.Open("sqlite", readOnlySQLiteDSN(dbPath))
	if err != nil {
		return []AgentSession{}, nil
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT conversation_id, title, preview, workspace_uris,
		       step_count, last_modified_time, last_user_input_time
		FROM conversation_summaries
		ORDER BY last_modified_time DESC
		LIMIT ?`, antigravitySessionListLimit)
	if err != nil {
		return []AgentSession{}, nil
	}
	defer rows.Close()

	var sessions []AgentSession
	for rows.Next() {
		var id string
		var title, preview, workspaces sql.NullString
		var stepCount sql.NullInt64
		var modified, lastInput sql.NullString
		if scanErr := rows.Scan(&id, &title, &preview, &workspaces,
			&stepCount, &modified, &lastInput); scanErr != nil {
			continue
		}
		if !IsSafeResumeID(id) {
			continue
		}
		// A conversation records its workspace only once work has started in
		// it: a session that stopped at the trust prompt has the column empty.
		// Those cannot be attributed to a project, and a picker scoped to one
		// must not offer them.
		if !antigravityWorkspaceMatches(workspaces.String, projectPath) {
			continue
		}

		label := strings.TrimSpace(title.String)
		if label == "" {
			label = strings.TrimSpace(preview.String)
		}
		if label == "" {
			label = id
		}
		updatedAt := parseAntigravityTime(modified.String)
		createdAt := parseAntigravityTime(lastInput.String)
		if createdAt.IsZero() {
			createdAt = updatedAt
		}
		count := int(stepCount.Int64)
		if count <= 0 {
			count = 1
		}

		sessions = append(sessions, AgentSession{
			SessionID:    id,
			FirstPrompt:  label,
			LastPrompt:   strings.TrimSpace(preview.String),
			MessageCount: count,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			AgentType:    AgentAntigravity,
			ProjectPath:  projectPath,
		})
	}
	if rows.Err() != nil {
		return []AgentSession{}, nil
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// antigravityWorkspaceMatches reports whether a conversation belongs to the
// project being asked about.
//
// workspace_uris is a JSON array of file:// URIs — a conversation can have
// more than one workspace open, and any of them counts.
func antigravityWorkspaceMatches(workspaceURIs, projectPath string) bool {
	if projectPath == "" {
		return false
	}
	raw := strings.TrimSpace(workspaceURIs)
	if raw == "" {
		return false
	}
	var uris []string
	if json.Unmarshal([]byte(raw), &uris) != nil {
		return false
	}
	for _, uri := range uris {
		if dir := antigravityURIToPath(uri); dir != "" && pathWithinProject(dir, projectPath) {
			return true
		}
	}
	return false
}

// antigravityURIToPath turns a file:// URI into a local path, and answers
// empty for anything else — a remote workspace is not a directory we can
// resume into.
func antigravityURIToPath(uri string) string {
	trimmed := strings.TrimSpace(uri)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	// url.Parse leaves percent-escapes decoded in Path, which is what a path
	// with spaces in it needs.
	path := parsed.Path

	// On Windows a file URI carries the drive inside the path:
	// file:///C:/Users/... parses to "/C:/Users/...". Left as it is, the
	// leading slash makes it a path that matches nothing, so every Antigravity
	// conversation would be filtered out of the picker there.
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Clean(filepath.FromSlash(path))
}

// parseAntigravityTime reads the timestamps the CLI writes. They arrive as
// SQLite datetimes with a numeric offset ("2026-09-15 15:26:10.12+00:00"),
// which is not one of Go's named layouts.
func parseAntigravityTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
		time.RFC3339,
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
