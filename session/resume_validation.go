package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// safeResumeIDRe matches the only shapes Claude/Codex/etc. ever use for a
// session/resume ID: UUIDs and similar opaque tokens. Everything is built
// from [A-Za-z0-9._-]; notably it forbids whitespace, quotes, ';', '|',
// '$', '`', '&', '(', ')' and path separators.
//
// This matters because a resume ID is not always typed by the user — it is
// also harvested from /proc/<pid>/cmdline of the process running inside an
// agent's tmux pane (getClaudeSessionIDFromTmux*). A compromised/hostile
// agent could spawn a child with a crafted --session-id, and that string
// would later be concatenated into a `tmux respawn-pane ... <cmd>` shell
// string. Rejecting anything that isn't this safe shape closes that
// agent-to-host command-injection vector cheaply.
var safeResumeIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// IsSafeResumeID reports whether resumeID is safe to place on a command
// line. Empty is considered "safe" (means: no resume) so callers can use
// it as a gate without special-casing the empty string.
func IsSafeResumeID(resumeID string) bool {
	if resumeID == "" {
		return true
	}
	return safeResumeIDRe.MatchString(resumeID)
}

// ResumeIDExists reports whether a saved resume session ID still exists on disk.
// We check this before invoking `claude --resume <id>` or `codex resume <id>`,
// because both CLIs error out hard if the ID is gone — Claude says
// "No conversation found with session ID: ..." and Codex says the same. Rather
// than letting the agent boot into a fatal error, we detect the missing ID
// up-front and either start fresh or fall back to a new session.
//
// Returns true for agents we don't know how to validate, so we don't break
// their existing flow.
func ResumeIDExists(agent AgentType, resumeID string) bool {
	if resumeID == "" {
		return false
	}
	// A syntactically unsafe ID can never be a legitimate on-disk session
	// and must never reach a command line — treat it as "does not exist"
	// so callers fall back to a clean start.
	if !IsSafeResumeID(resumeID) {
		return false
	}
	switch agent {
	case AgentClaude:
		return claudeResumeIDExists(resumeID)
	case AgentCodex:
		return codexResumeIDExists(resumeID)
	case AgentGemini:
		// Gemini scopes sessions to the working directory, so the same id can
		// exist and still be unusable from elsewhere. The caller knows the
		// directory; this entry point does not, so it answers the weaker
		// question. ResumeIDExistsForDir is the one to use where the directory
		// is known.
		return geminiResumeIDExists(resumeID, "")
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
	root, err := codexSessionsDir()
	if err != nil {
		return true
	}
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
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		// session_meta is line 1; bail if it's missing or doesn't match.
		if !scanner.Scan() {
			return nil
		}
		var meta codexSessionMeta
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			return nil
		}
		if meta.Type == "session_meta" && meta.isRoot() && meta.Payload.ID == resumeID {
			found = true
		}
		return nil
	})
	return found
}

// geminiResumeIDExists scans ~/.gemini/tmp/*/chats/*.json.
//
// The filename carries only the first eight characters of the id, so a match
// there is not proof — the id is in the file's own sessionId field, and that is
// what the CLI compares against. Checking the filename alone would accept an
// id that Gemini then refuses, which is the failure this exists to prevent:
// "Invalid session identifier ... No previous sessions found for this project",
// printed twice and leaving a dead tab.
func geminiResumeIDExists(resumeID, projectDir string) bool {
	dir := geminiConfigDirForResume()
	if dir == "" {
		return true // can't check — be safe and let the CLI try
	}
	// The prefix narrows the search to at most a handful of files; without it
	// this would open every transcript the CLI has ever written.
	prefix := resumeID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	matches, err := filepath.Glob(filepath.Join(dir, "tmp", "*", "chats", "*-"+prefix+".json"))
	if err != nil || len(matches) == 0 {
		return false
	}
	for _, path := range matches {
		raw, err := readFileAtMost(path, geminiSessionProbeLimit)
		if err != nil {
			continue
		}
		var session struct {
			SessionID   string `json:"sessionId"`
			ProjectHash string `json:"projectHash"`
			Messages    []struct {
				Type string `json:"type"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &session); err != nil {
			continue
		}
		if session.SessionID != resumeID {
			continue
		}
		// Found it — but the file existing is not enough for Gemini to accept it.
		//
		// It hides any transcript holding nothing but system messages, so a
		// session that failed at startup and recorded only errors is invisible
		// to it while sitting right there on disk. That is exactly what a failed
		// login leaves behind, and resuming it produced "Invalid session
		// identifier ... No previous sessions found for this project" on every
		// start.
		if !geminiHasRealMessage(session.Messages) {
			return false
		}
		// And it files sessions per directory, so an id belonging elsewhere is
		// as good as missing.
		if projectDir == "" || session.ProjectHash == "" {
			return true
		}
		return session.ProjectHash == geminiProjectHash(projectDir)
	}
	return false
}

// geminiSessionProbeLimit bounds the read: only the head of a transcript is
// needed, and they can be large.
const geminiSessionProbeLimit = 1 << 20

func geminiConfigDirForResume() string {
	if dir := os.Getenv("GEMINI_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini")
}

// ResumeIDExistsForDir is ResumeIDExists with the working directory the agent
// will start in — which Gemini needs, since it files sessions per directory.
func ResumeIDExistsForDir(agent AgentType, resumeID, projectDir string) bool {
	if agent == AgentGemini {
		if resumeID == "" || !IsSafeResumeID(resumeID) {
			return false
		}
		return geminiResumeIDExists(resumeID, projectDir)
	}
	return ResumeIDExists(agent, resumeID)
}

// geminiProjectHash mirrors the CLI's own getProjectHash: sha256 of the project
// root, hex-encoded, with no separator or normalisation.
func geminiProjectHash(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return hex.EncodeToString(sum[:])
}

// geminiHasRealMessage reports whether a transcript holds anything the user
// said or the model answered, as opposed to the info/error notices Gemini
// writes when a session cannot start. Its own listing applies the same rule.
func geminiHasRealMessage(messages []struct {
	Type string `json:"type"`
}) bool {
	for _, m := range messages {
		switch m.Type {
		case "info", "error", "warning", "":
			continue
		default:
			return true
		}
	}
	return false
}
