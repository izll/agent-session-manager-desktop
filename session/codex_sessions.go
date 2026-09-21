package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// codexSessionMeta represents the first line of a Codex JSONL session file
type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID             string          `json:"id"`
		SessionID      string          `json:"session_id"`
		CWD            string          `json:"cwd"`
		Source         json.RawMessage `json:"source"`
		ThreadSource   string          `json:"thread_source"`
		ParentThreadID string          `json:"parent_thread_id"`
	} `json:"payload"`
}

func (m codexSessionMeta) resumeID() string {
	if m.Payload.SessionID != "" {
		return m.Payload.SessionID
	}
	return m.Payload.ID
}

func (m codexSessionMeta) isRoot() bool {
	if m.Payload.ParentThreadID != "" || m.Payload.ThreadSource == "subagent" {
		return false
	}
	var sourceObject map[string]json.RawMessage
	if json.Unmarshal(m.Payload.Source, &sourceObject) == nil {
		if _, isSubagent := sourceObject["subagent"]; isSubagent {
			return false
		}
	}
	return true
}

func (m codexSessionMeta) isRootCLI() bool {
	if !m.isRoot() {
		return false
	}
	var source string
	return json.Unmarshal(m.Payload.Source, &source) == nil && source == "cli"
}

func codexSessionsDir() (string, error) {
	return codexSessionsDirIn(localFiles{})
}

func codexSessionsDirIn(files agentFiles) (string, error) {
	if _, isLocal := files.(localFiles); !isLocal {
		// CODEX_HOME is this process's environment, which says nothing about
		// the server; and a path from here cannot be resolved against its
		// filesystem. The default location is what a server has.
		homeDir, err := files.home()
		if err != nil {
			return "", err
		}
		return files.join(homeDir, ".codex", "sessions"), nil
	}

	var root string
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		root = filepath.Join(codexHome, "sessions")
	} else {
		homeDir, err := files.home()
		if err != nil {
			return "", err
		}
		root = filepath.Join(homeDir, ".codex", "sessions")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = evaluated
	}
	return root, nil
}

// codexMessage represents a user message in the Codex JSONL session file
type codexMessage struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

// ListCodexSessions lists all Codex sessions for the given project path
func ListCodexSessions(projectPath string) ([]AgentSession, error) {
	return listCodexSessionsFrom(localFiles{}, projectPath)
}

// codexMetaLineLimit bounds the read that decides whether a file is relevant.
//
// Codex writes a session_meta line first, carrying the id and the working
// directory — the two fields the filter needs. Measured across a real store,
// that line ran to 22 KB at its longest, so 128 KB is room to spare while
// keeping the scan affordable: the store itself was 24 GB.
const codexMetaLineLimit = 128 << 10

// codexPromptScanLimit bounds the second read, which looks for the first thing
// the user actually typed.
//
// That message can sit much further in than the metadata — measured at 9 MB
// for a tenth of the files, because everything the agent did first is written
// before it. This is read only for sessions the filter already accepted, so
// the cost is paid for a handful of files rather than all of them.
const codexPromptScanLimit = 4 << 20

// codexFileScanLimit bounds how many files are looked at, as opposed to how
// many sessions come back.
//
// Far above the number of conversations wanted, because most of the store is
// not conversations at all: measured on a real store, 862 files held 41
// sessions the user had started, the rest being the agent's own subagent
// threads. A budget counted in sessions would have been spent on those before
// reaching the second one. Only the metadata line of each is read.
const codexFileScanLimit = 2000

// listCodexSessionsFrom is the same listing against a given machine.
//
// Two passes, because the store is large and most of it is irrelevant: the
// first reads only the metadata line of each file to find the ones belonging
// to this project, the second reads further into those alone to find a prompt
// worth showing. Reading every file whole would be 24 GB over SSH on the store
// this was measured against.
func listCodexSessionsFrom(files agentFiles, projectPath string) ([]AgentSession, error) {
	sessionDir, err := codexSessionsDirIn(files)
	if err != nil {
		return []AgentSession{}, nil
	}

	// By name, not by modification time. Codex files its sessions under
	// YYYY/MM/DD with the timestamp in each filename, so the path sorts
	// chronologically — and unlike an mtime, that survives the store being
	// copied. On a store that had been moved, every file carried the same
	// minute, and picking by mtime found one of a project's sessions where
	// this finds seventeen.
	paths, err := files.findFilesByName(sessionDir, ".jsonl", codexFileScanLimit)
	if err != nil {
		return []AgentSession{}, nil
	}

	var sessions []AgentSession
	for _, path := range paths {
		if len(sessions) >= agentSessionScanLimit {
			break
		}
		head, readErr := files.readHead(path, codexMetaLineLimit)
		if readErr != nil {
			continue
		}
		sessionID, cwd := parseCodexMeta(head)
		if sessionID == "" {
			// Not a session the user started: most files here are the
			// agent's own subagent threads, which parseCodexMeta rejects.
			continue
		}
		if !codexSessionBelongsTo(cwd, projectPath) {
			continue
		}

		// Only now, for a file already known to belong here.
		firstPrompt := ""
		if body, err := files.readHead(path, codexPromptScanLimit); err == nil {
			firstPrompt = parseCodexFirstPrompt(body)
		}
		if firstPrompt == "" {
			// A session with no prompt of its own is one the user never spoke
			// in; showing it would offer an empty conversation to resume.
			continue
		}

		sessions = append(sessions, AgentSession{
			SessionID:    sessionID,
			FirstPrompt:  firstPrompt,
			LastPrompt:   firstPrompt, // We only read the first prompt
			MessageCount: 1,
			AgentType:    AgentCodex,
		})
	}

	// findFiles already returns newest first, which is the order wanted here.
	return sessions, nil
}

// codexSessionBelongsTo reports whether a session's working directory and the
// project are the same tree — either may be an ancestor of the other.
func codexSessionBelongsTo(cwd, projectPath string) bool {
	if projectPath == "" || cwd == "" {
		return true
	}
	if cwd == projectPath {
		return true
	}
	normalisedCWD := cwd
	if !strings.HasSuffix(normalisedCWD, "/") {
		normalisedCWD += "/"
	}
	normalisedProject := projectPath
	if !strings.HasSuffix(normalisedProject, "/") {
		normalisedProject += "/"
	}
	return strings.HasPrefix(normalisedProject, normalisedCWD) ||
		strings.HasPrefix(normalisedCWD, normalisedProject)
}

// parseCodexMeta reads the session_meta line Codex writes first, which carries
// the id and the directory the session ran in.
//
// Byte-based rather than file-based so the same parser serves a local file and
// a bounded read from a server.
func parseCodexMeta(head []byte) (sessionID, cwd string) {
	scanner := bufio.NewScanner(bytes.NewReader(head))
	scanner.Buffer(make([]byte, 64*1024), codexMetaLineLimit)
	if !scanner.Scan() {
		return "", ""
	}
	var meta codexSessionMeta
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return "", ""
	}
	if meta.Type != "session_meta" || !meta.isRoot() {
		return "", ""
	}
	return meta.Payload.ID, meta.Payload.CWD
}

// parseCodexFirstPrompt finds the first thing the user typed, skipping the
// instructions and environment blocks the agent inserts before it.
//
// An empty result means none was found within what was read; the caller treats
// that as a session with nothing worth resuming, which is also what it means
// when the file genuinely holds no user message.
func parseCodexFirstPrompt(body []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg codexMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type != "response_item" || msg.Payload.Type != "message" ||
			msg.Payload.Role != "user" {
			continue
		}
		for _, content := range msg.Payload.Content {
			if content.Type != "input_text" || content.Text == "" {
				continue
			}
			if strings.HasPrefix(content.Text, "# AGENTS.md") ||
				strings.HasPrefix(content.Text, "<environment_context>") {
				continue
			}
			prompt := content.Text
			if len(prompt) > 100 {
				prompt = prompt[:97] + "..."
			}
			return prompt
		}
	}
	return ""
}
