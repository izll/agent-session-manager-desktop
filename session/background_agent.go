package session

import (
	"encoding/json"
	"log"
	"os/exec"
	"time"
)

// ReleaseClaudeBackgroundAgent frees a Claude conversation that is currently
// held by a background agent (the user pressed Ctrl+B, or dispatched it with
// `claude --bg`). While such an agent holds the session, `claude --resume <id>`
// refuses to start ("currently running as a background agent"), which left an
// asmgr tab bound to that session unable to come back.
//
// We stop the agent with the official `claude stop <id>` (Claude Code
// persists the transcript continuously, so the conversation history is
// intact) and wait briefly for it to disappear from the agents list before
// the caller resumes. Only `kind == "background"` agents are touched —
// interactive sessions running in other terminals are left alone.
func ReleaseClaudeBackgroundAgent(resumeID string) {
	if resumeID == "" {
		return
	}
	shortID, held := findBackgroundAgent(resumeID)
	if !held {
		return
	}

	// `claude stop` only accepts the short job id (the agents list's "id"
	// field; in practice the first 8 chars of the session UUID) — the full
	// UUID is rejected, and the command exits 0 either way, so we verify by
	// polling the list below instead of trusting the exit code.
	if err := exec.Command("claude", "stop", shortID).Run(); err != nil {
		log.Printf("[bg-agent] claude stop %s failed: %v", shortID, err)
		return
	}
	log.Printf("[bg-agent] stopped background agent %s (session=%s)", shortID, resumeID)

	// Wait (max ~2s) for it to leave the list so the immediate resume
	// doesn't race the shutdown and hit the same refusal.
	for i := 0; i < 10; i++ {
		if _, held := findBackgroundAgent(resumeID); !held {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	log.Printf("[bg-agent] session=%s still listed after stop; resume may be refused once", resumeID)
}

// findBackgroundAgent reports whether a live background agent currently owns
// the given session ID, and returns its short job id (used by `claude stop`).
func findBackgroundAgent(resumeID string) (string, bool) {
	out, err := exec.Command("claude", "agents", "--json").Output()
	if err != nil {
		return "", false // claude CLI missing/old — treat as free
	}
	var agents []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(out, &agents); err != nil {
		return "", false
	}
	for _, ag := range agents {
		if ag.Kind != "background" || ag.SessionID != resumeID {
			continue
		}
		shortID := ag.ID
		if shortID == "" && len(resumeID) >= 8 {
			shortID = resumeID[:8]
		}
		return shortID, true
	}
	return "", false
}
