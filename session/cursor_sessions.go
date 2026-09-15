package session

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cursorChatMetadataLimit caps the per-chat files this reads. They are a title
// and a list of prompts, so anything larger is not what we think it is.
const cursorChatMetadataLimit = 1 << 20

// cursorChatsDir returns ~/.cursor/chats, the root Cursor keeps its CLI
// conversations under.
func cursorChatsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cursor", "chats"), nil
}

// cursorProjectHash is the directory name Cursor files a project's chats under.
//
// It is the MD5 of the project's absolute path, with no trailing slash —
// measured against a real store, where all three directories matched their
// path exactly. Adding a separator produces a different digest and finds
// nothing, which is the mistake this exists to make impossible.
//
// Symlinks are resolved first so that /var and /private/var, or a home
// reached through a link, name the same project rather than two.
func cursorProjectHash(projectPath string) string {
	if projectPath == "" {
		return ""
	}
	path := projectPath
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	sum := md5.Sum([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:])
}

// ListCursorSessions returns the chats Cursor has recorded for one project.
//
// Unlike the other agents this needs no scan: the chats of a project live in a
// directory named after that project, so the lookup is a single stat. Each
// chat is a UUID directory holding meta.json (the title Cursor gave it and the
// timestamps) and prompt_history.json (what was actually typed). The chat id
// is the directory name, and it is what cursor-agent --resume takes.
func ListCursorSessions(projectPath string) ([]AgentSession, error) {
	root, err := cursorChatsDir()
	if err != nil {
		return []AgentSession{}, nil
	}
	hash := cursorProjectHash(projectPath)
	if hash == "" {
		return []AgentSession{}, nil
	}
	projectDir := filepath.Join(root, hash)
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		// No directory means no chats here, which is not an error to report.
		return []AgentSession{}, nil
	}

	var sessions []AgentSession
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chatID := entry.Name()
		// The id goes on a command line; refuse anything that is not a plain
		// identifier rather than trusting a directory name.
		if !IsSafeResumeID(chatID) {
			continue
		}

		var meta struct {
			Title           string `json:"title"`
			CreatedAtMs     int64  `json:"createdAtMs"`
			UpdatedAtMs     int64  `json:"updatedAtMs"`
			HasConversation bool   `json:"hasConversation"`
		}
		metaPath := filepath.Join(projectDir, chatID, "meta.json")
		if data, readErr := readFileAtMost(metaPath, cursorChatMetadataLimit); readErr == nil {
			_ = json.Unmarshal(data, &meta)
		}

		// A chat that was opened and never used has no conversation to return
		// to. Cursor records that directly; where it does not, an empty prompt
		// history says the same thing.
		prompts := readCursorPromptHistory(filepath.Join(projectDir, chatID, "prompt_history.json"))
		if !meta.HasConversation && len(prompts) == 0 {
			continue
		}

		first := ""
		if len(prompts) > 0 {
			first = prompts[0]
		}
		last := first
		if len(prompts) > 1 {
			last = prompts[len(prompts)-1]
		}
		// The typed prompt reads better in a picker than Cursor's own summary,
		// but a chat resumed from elsewhere may have a title and no history.
		label := first
		if label == "" {
			label = strings.TrimSpace(meta.Title)
		}
		if label == "" {
			label = chatID
		}

		createdAt, updatedAt := cursorChatTimes(projectDir, chatID, meta.CreatedAtMs, meta.UpdatedAtMs)
		messageCount := len(prompts)
		if messageCount == 0 {
			messageCount = 1
		}

		sessions = append(sessions, AgentSession{
			SessionID:    chatID,
			FirstPrompt:  label,
			LastPrompt:   last,
			MessageCount: messageCount,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			AgentType:    AgentCursor,
			ProjectPath:  projectPath,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// readCursorPromptHistory returns the prompts typed into one chat, newest last.
// A missing or unreadable file is an empty history, not an error: the chat may
// simply never have been used.
func readCursorPromptHistory(path string) []string {
	data, err := readFileAtMost(path, cursorChatMetadataLimit)
	if err != nil {
		return nil
	}
	var prompts []string
	if json.Unmarshal(data, &prompts) != nil {
		return nil
	}
	cleaned := prompts[:0]
	for _, prompt := range prompts {
		if trimmed := strings.TrimSpace(prompt); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

// cursorChatTimes prefers the timestamps Cursor records and falls back to the
// directory's own, so a chat with a malformed meta.json still sorts sensibly
// rather than landing at the epoch.
func cursorChatTimes(projectDir, chatID string, createdMs, updatedMs int64) (time.Time, time.Time) {
	var fallback time.Time
	if info, err := os.Stat(filepath.Join(projectDir, chatID)); err == nil {
		fallback = info.ModTime()
	}
	createdAt, updatedAt := fallback, fallback
	if createdMs > 0 {
		createdAt = time.UnixMilli(createdMs)
	}
	if updatedMs > 0 {
		updatedAt = time.UnixMilli(updatedMs)
	}
	return createdAt, updatedAt
}
