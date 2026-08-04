package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeRegistryEntry puts one of Claude's per-process files in a temporary
// config dir and points CLAUDE_CONFIG_DIR at it.
func writeRegistryEntry(t *testing.T, pid int, entry map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	sessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("creating sessions dir: %v", err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshalling entry: %v", err)
	}
	path := filepath.Join(sessions, fmt.Sprintf("%d.json", pid))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("writing entry: %v", err)
	}
}

// liveProcStart returns this process's start time, so a test entry can claim to
// describe a process that really is running.
func liveProcStart(t *testing.T) string {
	t.Helper()
	ticks, ok := procStartTicks(os.Getpid())
	if !ok {
		t.Skip("no readable /proc/<pid>/stat on this platform")
	}
	return ticks
}

// The conversation a Claude process is on comes from Claude's own record of it.
// Reading the arguments it was started with instead is what missed an in-session
// /resume: that switches the running process to another conversation without
// restarting it, so argv keeps naming the one the user left.
func TestClaudeSessionIDComesFromTheRegistry(t *testing.T) {
	pid := os.Getpid()
	want := "c1312030-9715-4966-91d5-99dd6f175750"
	writeRegistryEntry(t, pid, map[string]any{
		"pid":       pid,
		"sessionId": want,
		"cwd":       "/work",
		"procStart": liveProcStart(t),
	})

	if got := ClaudeSessionIDForPID(pid); got != want {
		t.Errorf("ClaudeSessionIDForPID = %q, want %q", got, want)
	}
}

// Claude deletes the file when it exits, but not when it is killed or crashes.
// Pids are reused, so an entry left behind by a dead process would otherwise
// name a conversation belonging to nothing, and resume the wrong one.
func TestStaleRegistryEntryIsRejected(t *testing.T) {
	pid := os.Getpid()
	writeRegistryEntry(t, pid, map[string]any{
		"pid":       pid,
		"sessionId": "11111111-1111-4111-8111-111111111111",
		"cwd":       "/work",
		// A start time that is not this process's: the number was reused.
		"procStart": "1",
	})

	if got := ClaudeSessionIDForPID(pid); got != "" {
		t.Errorf("ClaudeSessionIDForPID = %q for an entry describing a different "+
			"process; a reused pid must not resolve to its predecessor's conversation", got)
	}
}

// The file is written by another program and is not covered by any documented
// interface, so every way it can disappoint has to end in "no answer" rather
// than in a bad id reaching a command line.
func TestUnusableRegistryEntriesYieldNothing(t *testing.T) {
	start := liveProcStart(t)
	pid := os.Getpid()

	cases := []struct {
		name  string
		entry map[string]any
	}{
		{"no session id", map[string]any{
			"pid": pid, "procStart": start,
		}},
		{"entry is for another pid", map[string]any{
			"pid": pid + 1, "sessionId": "11111111-1111-4111-8111-111111111111",
			"procStart": start,
		}},
		{"no recorded start time", map[string]any{
			"pid": pid, "sessionId": "11111111-1111-4111-8111-111111111111",
		}},
		{"id would not be safe on a command line", map[string]any{
			"pid": pid, "sessionId": "x; rm -rf ~", "procStart": start,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeRegistryEntry(t, pid, tc.entry)
			if got := ClaudeSessionIDForPID(pid); got != "" {
				t.Errorf("ClaudeSessionIDForPID = %q, want none", got)
			}
		})
	}
}

// A missing file is the ordinary case, not an error: the pane may be running
// something else entirely, or a Claude too old to keep this record.
func TestMissingRegistryEntryYieldsNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if got := ClaudeSessionIDForPID(os.Getpid()); got != "" {
		t.Errorf("ClaudeSessionIDForPID = %q with no file present, want none", got)
	}
}

// A pane's process tree is what the caller has, and only one of those processes
// is the agent.
func TestFirstAnsweringPIDWins(t *testing.T) {
	pid := os.Getpid()
	want := "22222222-2222-4222-8222-222222222222"
	writeRegistryEntry(t, pid, map[string]any{
		"pid":       pid,
		"sessionId": want,
		"cwd":       "/work",
		"procStart": liveProcStart(t),
	})

	got := ClaudeSessionIDForPIDs([]string{"", "not-a-number", "999999999", fmt.Sprint(pid)})
	if got != want {
		t.Errorf("ClaudeSessionIDForPIDs = %q, want %q", got, want)
	}
}

// CLAUDE_CONFIG_DIR moves the whole config directory. Without it a user who has
// relocated theirs gets silent misses everywhere.
func TestConfigDirIsHonoured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	got, err := claudeSessionsDir()
	if err != nil {
		t.Fatalf("claudeSessionsDir: %v", err)
	}
	if want := filepath.Join(dir, "sessions"); got != want {
		t.Errorf("claudeSessionsDir = %q, want %q", got, want)
	}
}

// The start time is field 22 of /proc/<pid>/stat, and field 2 is the executable
// name in parentheses, which may itself contain spaces. Splitting the whole line
// on whitespace gets the wrong field for any process whose name has one.
func TestProcStartSkipsPastTheProcessName(t *testing.T) {
	ticks, ok := procStartTicks(os.Getpid())
	if !ok {
		t.Skip("no readable /proc/<pid>/stat on this platform")
	}
	if ticks == "" {
		t.Fatal("no start time read for this process")
	}
	for _, r := range ticks {
		if r < '0' || r > '9' {
			t.Fatalf("start time = %q, which is not a number — the field offset is "+
				"probably landing inside the process name", ticks)
		}
	}
}
