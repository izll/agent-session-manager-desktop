package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Claude Code records which conversation each of its processes is on, in
// ~/.claude/sessions/<pid>.json, and rewrites that file when the conversation
// changes.
//
// This is what makes an in-session /resume visible from outside. The command
// does not restart anything — it switches the running process to another
// transcript — so the process keeps the arguments it was launched with, and the
// id in its command line goes stale the moment the user picks a different
// conversation. Reading argv, which is what this code did before, then resumed
// the conversation the user had moved away from, or an empty one.
//
// Verified against Claude Code 2.1.221 by resuming inside a live session: same
// pid, same argv, and the file's sessionId changed from the one launched with
// to the one selected.
//
// Nothing documents this file, so treat it as a hint that may vanish: every
// failure here returns "not found" and leaves the caller with whatever it
// already knew.

// claudeSessionRegistryEntry is the part of the file we rely on. Claude writes
// more (name, status, version); those are left out rather than parsed and
// ignored, so this does not look like it depends on them.
type claudeSessionRegistryEntry struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	// ProcStart is the process's start time in clock ticks — field 22 of
	// /proc/<pid>/stat. Claude records it to tell its own process apart from an
	// unrelated one that later reused the number, and we check it for the same
	// reason.
	ProcStart string `json:"procStart"`
}

// claudeSessionsDir returns where Claude keeps the per-process files.
//
// CLAUDE_CONFIG_DIR relocates the whole config directory, so it is honoured
// here — a user who has moved it would otherwise get silent misses.
func claudeSessionsDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "sessions"), nil
}

// procStartTicks reads a process's start time, field 22 of /proc/<pid>/stat.
//
// Linux only. Elsewhere there is no /proc, the read fails, and every entry is
// rejected for want of a liveness check — so those platforms fall back to
// reading argv, exactly as before. Deliberate: without a way to tell a live
// process from a reused pid, a file naming a dead one would resume the wrong
// conversation, which is worse than missing an in-session /resume.
//
// The fields cannot simply be split on spaces: field 2 is the executable name
// in parentheses and may itself contain spaces. Everything after the final ')'
// is fixed-width, so the split starts there — field 22 overall is field 20 of
// the remainder, since the name and the two fields before it are consumed.
func procStartTicks(pid int) (string, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	close := strings.LastIndexByte(string(raw), ')')
	if close < 0 {
		return "", false
	}
	fields := strings.Fields(string(raw)[close+1:])
	const startTimeOffset = 19 // field 22 counting from 1, less pid, comm, state
	if len(fields) <= startTimeOffset {
		return "", false
	}
	return fields[startTimeOffset], true
}

// ClaudeSessionIDForPID reports the conversation the given Claude process is on
// now, or "" when that cannot be established.
//
// A stale file is the failure that matters: pids are reused, and answering with
// a dead process's conversation would resume the wrong one. Claude removes the
// file on exit, but not after a kill or a crash, so the recorded start time is
// checked against the live process. Where that cannot be read the entry is
// rejected rather than trusted.
func ClaudeSessionIDForPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	dir, err := claudeSessionsDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)))
	if err != nil {
		return ""
	}
	var entry claudeSessionRegistryEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ""
	}
	if entry.SessionID == "" || entry.PID != pid {
		return ""
	}
	// Same shape check the rest of the resume path applies: this value ends up
	// on a command line, and it comes from a file any process can write.
	if !IsSafeResumeID(entry.SessionID) {
		return ""
	}
	live, ok := procStartTicks(pid)
	if !ok || live != entry.ProcStart {
		return ""
	}
	return entry.SessionID
}

// ClaudeSessionIDForPIDs returns the conversation of the first process that has
// one, for callers holding a pane's process tree rather than a single pid.
func ClaudeSessionIDForPIDs(pids []string) string {
	for _, raw := range pids {
		pid, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if id := ClaudeSessionIDForPID(pid); id != "" {
			return id
		}
	}
	return ""
}
