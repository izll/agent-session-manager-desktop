package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexMetaLine(id, sessionID, cwd, source, threadSource, parentID string) string {
	return fmt.Sprintf(
		`{"type":"session_meta","payload":{"id":%q,"session_id":%q,"cwd":%q,"source":%s,"thread_source":%q,"parent_thread_id":%q}}`+"\n",
		id,
		sessionID,
		cwd,
		source,
		threadSource,
		parentID,
	)
}

func TestParseCodexRootSessionMeta(t *testing.T) {
	t.Parallel()

	const cwd = "/work/project"
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "modern root prefers session ID",
			line: codexMetaLine("rollout-id", "session-id", cwd, `"cli"`, "user", ""),
			want: "session-id",
		},
		{
			name: "legacy root falls back to ID",
			line: codexMetaLine("legacy-id", "", cwd, `"cli"`, "", ""),
			want: "legacy-id",
		},
		{
			name: "subagent source object rejected",
			line: codexMetaLine("child-id", "root-id", cwd, `{"subagent":{"thread_spawn":{}}}`, "subagent", "root-id"),
		},
		{
			name: "parent thread rejected",
			line: codexMetaLine("child-id", "root-id", cwd, `"cli"`, "", "root-id"),
		},
		{
			name: "wrong cwd rejected",
			line: codexMetaLine("root-id", "root-id", "/work/other", `"cli"`, "user", ""),
		},
		{
			name: "unsafe ID rejected",
			line: codexMetaLine("bad;id", "", cwd, `"cli"`, "user", ""),
		},
		{
			name: "malformed JSON rejected",
			line: "{",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseCodexRootSessionMeta(strings.NewReader(tt.line), cwd); got != tt.want {
				t.Fatalf("parseCodexRootSessionMeta() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCodexRootSessionMetaRejectsOversizedFirstLine(t *testing.T) {
	t.Parallel()
	line := strings.Repeat(" ", maxCodexMetaLineSize+1) + "\n"
	if got := parseCodexRootSessionMeta(strings.NewReader(line), ""); got != "" {
		t.Fatalf("parseCodexRootSessionMeta() = %q, want empty", got)
	}
}

func TestDetectCodexSessionIDFromProcessTree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procRoot := filepath.Join(tempDir, "proc")
	sessionsRoot := filepath.Join(tempDir, "sessions")
	cwd := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	rootRollout := filepath.Join(sessionsRoot, "2026", "07", "24", "root.jsonl")
	subagentRollout := filepath.Join(sessionsRoot, "2026", "07", "24", "subagent.jsonl")
	writeTestFile(t, rootRollout, codexMetaLine("root-id", "root-id", cwd, `"cli"`, "user", ""))
	writeTestFile(t, subagentRollout, codexMetaLine("child-id", "root-id", cwd, `{"subagent":{}}`, "subagent", "root-id"))

	writeProcessChildren(t, procRoot, 100, "101")
	writeProcessChildren(t, procRoot, 101, "")
	linkProcessFD(t, procRoot, 101, "7", rootRollout)
	linkProcessFD(t, procRoot, 101, "8", subagentRollout)

	if got := detectCodexSessionIDFromProcessTree(procRoot, sessionsRoot, 100, cwd); got != "root-id" {
		t.Fatalf("detectCodexSessionIDFromProcessTree() = %q, want root-id", got)
	}
}

func TestDetectCodexSessionIDFromProcessTreeFailsClosedOnAmbiguity(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procRoot := filepath.Join(tempDir, "proc")
	sessionsRoot := filepath.Join(tempDir, "sessions")
	cwd := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(sessionsRoot, "first.jsonl")
	second := filepath.Join(sessionsRoot, "second.jsonl")
	writeTestFile(t, first, codexMetaLine("first-id", "", cwd, `"cli"`, "user", ""))
	writeTestFile(t, second, codexMetaLine("second-id", "", cwd, `"cli"`, "user", ""))
	writeProcessChildren(t, procRoot, 200, "")
	linkProcessFD(t, procRoot, 200, "3", first)
	linkProcessFD(t, procRoot, 200, "4", second)

	if got := detectCodexSessionIDFromProcessTree(procRoot, sessionsRoot, 200, cwd); got != "" {
		t.Fatalf("detectCodexSessionIDFromProcessTree() = %q, want empty for ambiguity", got)
	}
}

func TestDetectCodexSessionIDFromPlatformOpenPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sessionsRoot := filepath.Join(tempDir, "sessions")
	cwd := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessionsRoot, "rollout.jsonl")
	writeTestFile(t, rollout, codexMetaLine("platform-id", "", cwd, `"cli"`, "user", ""))
	outside := filepath.Join(tempDir, "outside.jsonl")
	writeTestFile(t, outside, codexMetaLine("wrong-id", "", cwd, `"cli"`, "user", ""))

	if got := detectCodexSessionIDFromOpenPaths(sessionsRoot, cwd, []string{outside, rollout}); got != "platform-id" {
		t.Fatalf("detectCodexSessionIDFromOpenPaths() = %q, want platform-id", got)
	}
}

func TestCaptureCodexResumeIDs(t *testing.T) {
	t.Parallel()

	instance := &Instance{
		ID:    "instance",
		Path:  "/work/main",
		Agent: AgentCodex,
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentCodex, Name: "codex", WorkDir: "/work/tab"},
			{Index: 3, Agent: AgentCodex, Name: "manual", ResumeSessionID: "manual-id"},
			{Index: 4, Agent: AgentCodex, Name: "stopped", Stopped: true},
			{Index: 5, Agent: AgentClaude, Name: "claude"},
		},
	}
	var calls []string
	detector := func(_ string, windowIdx int, expectedCWD string) string {
		calls = append(calls, fmt.Sprintf("%d:%s", windowIdx, expectedCWD))
		switch windowIdx {
		case 0:
			return "main-id"
		case 2:
			return "tab-id"
		case 3:
			// This pane already holds an id, and the live process reports a
			// different one — which is what happens when the user resumes an
			// earlier conversation inside Codex. The new one wins: the stored
			// one now points at the conversation they moved away from.
			return "resumed-id"
		default:
			return ""
		}
	}

	if !instance.captureCodexResumeIDsAtMainWindow(detector, 0, true) {
		t.Fatal("captureCodexResumeIDs() reported no change")
	}
	if instance.ResumeSessionID != "main-id" {
		t.Fatalf("main ResumeSessionID = %q, want main-id", instance.ResumeSessionID)
	}
	if instance.FollowedWindows[0].ResumeSessionID != "tab-id" {
		t.Fatalf("followed ResumeSessionID = %q, want tab-id", instance.FollowedWindows[0].ResumeSessionID)
	}
	// Updated, not preserved. Resuming inside Codex starts a new session, so a
	// stored id that disagrees with the running process is the stale one —
	// keeping it reopened the wrong conversation, or an empty one, on restart.
	if instance.FollowedWindows[1].ResumeSessionID != "resumed-id" {
		t.Fatalf("ResumeSessionID = %q, want it updated to resumed-id",
			instance.FollowedWindows[1].ResumeSessionID)
	}
	// A stopped pane has no process to read, and a Claude pane is not ours to
	// touch, so neither is probed.
	if got, want := strings.Join(calls, ","), "0:/work/main,2:/work/tab,3:/work/main"; got != want {
		t.Fatalf("detector calls = %q, want %q", got, want)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeProcessChildren(t *testing.T, procRoot string, pid int, children string) {
	t.Helper()
	path := filepath.Join(procRoot, fmt.Sprint(pid), "task", fmt.Sprint(pid), "children")
	writeTestFile(t, path, children)
}

func linkProcessFD(t *testing.T, procRoot string, pid int, fd, target string) {
	t.Helper()
	fdDir := filepath.Join(procRoot, fmt.Sprint(pid), "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(fdDir, fd)); err != nil {
		t.Fatal(err)
	}
}

// Resuming a conversation inside Codex starts a new session, and the id we hold
// then points at the one the user moved away from.
//
// The capture used to run only for panes with no id recorded, so that stale id
// survived: restarting the tab reopened the old conversation, or an empty one,
// rather than what was on screen.
func TestCodexResumeIDFollowsTheLiveSession(t *testing.T) {
	t.Parallel()

	instance := &Instance{
		ID:              "instance",
		Path:            "/work",
		Status:          StatusRunning,
		Agent:           AgentCodex,
		ResumeSessionID: "the-one-we-started-with",
	}

	// A pane that already has an id must still be probed, or a switch inside
	// Codex can never be noticed.
	if !instance.NeedsCodexResumeCapture() {
		t.Fatal("a running Codex pane with an id recorded is not probed, so resuming " +
			"inside Codex can never be picked up")
	}

	detector := func(_ string, _ int, _ string) string { return "the-one-in-use-now" }
	if !instance.captureCodexResumeIDsAtMainWindow(detector, 0, true) {
		t.Fatal("no change reported although the live session differs")
	}
	if instance.ResumeSessionID != "the-one-in-use-now" {
		t.Errorf("ResumeSessionID = %q, want the live session", instance.ResumeSessionID)
	}
}

// Detection reads the running process's open files, so finding nothing means
// nothing was found — it must not clear what is already recorded.
func TestCodexResumeIDSurvivesAFailedDetection(t *testing.T) {
	t.Parallel()

	instance := &Instance{
		ID:              "instance",
		Path:            "/work",
		Status:          StatusRunning,
		Agent:           AgentCodex,
		ResumeSessionID: "known-good",
	}

	detector := func(_ string, _ int, _ string) string { return "" }
	if instance.captureCodexResumeIDsAtMainWindow(detector, 0, true) {
		t.Error("a change was reported although nothing was detected")
	}
	if instance.ResumeSessionID != "known-good" {
		t.Errorf("ResumeSessionID = %q; a failed probe must not discard a working id",
			instance.ResumeSessionID)
	}
}
