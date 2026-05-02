package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ResumeIDExists reports whether a saved resume session ID still exists on disk.
// We check this before invoking `claude --resume <id>` or `codex resume <id>`,
// because both CLIs error out hard if the ID is gone — Claude says
// "No conversation found with session ID: ..." and Codex says the same. Rather
// than letting the agent boot into a fatal error, we detect the missing ID
// up-front and either start fresh or fall back to a new session.
//
// Returns true for agents we don't know how to validate (e.g. Gemini) so we
// don't break their existing flow.
func ResumeIDExists(agent AgentType, resumeID string) bool {
	if resumeID == "" {
		return false
	}
	switch agent {
	case AgentClaude:
		return claudeResumeIDExists(resumeID)
	case AgentCodex:
		return codexResumeIDExists(resumeID)
	default:
		// Unknown agent: assume the ID is valid, don't second-guess.
		return true
	}
}

// claudeResumeIDExists scans ~/.claude/projects/*/<id>.jsonl. Claude stores
// each conversation under a per-project directory, so we have to walk the
// project dirs — but only one filename match is needed.
func claudeResumeIDExists(resumeID string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return true // can't check — be safe and let the CLI try
	}
	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return true
	}
	target := resumeID + ".jsonl"
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, e.Name(), target)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// codexResumeIDExists scans ~/.codex/sessions/**/*.jsonl. Codex stores the
// session ID in the JSON body, not the filename, so we have to read the
// `session_meta` line of each file. To stay cheap, we early-return on first
// match.
func codexResumeIDExists(resumeID string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	root := filepath.Join(homeDir, ".codex", "sessions")
	if _, err := os.Stat(root); err != nil {
		return false
	}
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		// session_meta is line 1; bail if it's missing or doesn't match.
		if !scanner.Scan() {
			return nil
		}
		var meta codexSessionMeta
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			return nil
		}
		if meta.Type == "session_meta" && meta.Payload.ID == resumeID {
			found = true
		}
		return nil
	})
	return found
}
