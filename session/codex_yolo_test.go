package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Rollout lines as Codex 0.157 writes them, trimmed to what matters.
const (
	yoloTurn     = `{"timestamp":"t","type":"turn_context","payload":{"turn_id":"a","cwd":"/w","approval_policy":"never","sandbox_policy":{"type":"danger-full-access"},"permission_profile":{"type":"disabled"}}}`
	managedTurn  = `{"timestamp":"t","type":"turn_context","payload":{"turn_id":"b","cwd":"/w","approval_policy":"on-request","sandbox_policy":{"type":"workspace-write","writable_roots":["/w"]},"permission_profile":{"type":"managed","file_system":{"type":"restricted"}}}}`
	olderYolo    = `{"timestamp":"t","type":"turn_context","payload":{"cwd":"/w","approval_policy":"never","sandbox_policy":{"type":"danger-full-access"}}}`
	yoloSettings = `{"timestamp":"t","type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"x","thread_settings":{"approval_policy":"never","permission_profile":{"type":"disabled"}}}}`
	askSettings  = `{"timestamp":"t","type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"x","thread_settings":{"approval_policy":"on-request","permission_profile":{"type":"managed"},"active_permission_profile":{"id":"ws"}}}}`
	chatter      = `{"timestamp":"t","type":"event_msg","payload":{"type":"token_count","info":null}}`
)

func TestARolloutLineSaysWhetherYoloIsOn(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  CodexYolo
		found bool
	}{
		{"turn with no sandbox", yoloTurn, CodexYoloOn, true},
		{"turn in a managed sandbox", managedTurn, CodexYoloOff, true},
		{"older turn, sandbox policy only", olderYolo, CodexYoloOn, true},
		{"settings applied on open", yoloSettings, CodexYoloOn, true},
		{"settings applied by the daemon", askSettings, CodexYoloOff, true},
		{"never asking, but sandboxed", `{"type":"turn_context","payload":{"approval_policy":"never","sandbox_policy":{"type":"workspace-write"}}}`, CodexYoloOff, true},
		{"a structured approval policy", `{"type":"turn_context","payload":{"approval_policy":{"granular":{}},"permission_profile":{"type":"disabled"}}}`, CodexYoloOff, true},
		{"unrelated line", chatter, CodexYoloUnknown, false},
		{"a message quoting the marker", `{"type":"response_item","payload":{"type":"message","text":"\"turn_context\""}}`, CodexYoloUnknown, false},
		{"torn line", yoloTurn[:60], CodexYoloUnknown, false},
	}
	for _, c := range cases {
		got, found := codexYoloFromLine([]byte(c.line))
		if got != c.want || found != c.found {
			t.Errorf("%s: got %d/%v, want %d/%v", c.name, got, found, c.want, c.found)
		}
	}
}

func TestTheLastSettingsLineWins(t *testing.T) {
	data := strings.Join([]string{yoloTurn, chatter, askSettings, chatter, ""}, "\n")
	if got, found, _ := lastCodexYolo([]byte(data), true); !found || got != CodexYoloOff {
		t.Errorf("got %d/%v, want the later daemon settings (off)", got, found)
	}
	data = strings.Join([]string{managedTurn, askSettings, yoloSettings, chatter, ""}, "\n")
	if got, _, _ := lastCodexYolo([]byte(data), true); got != CodexYoloOn {
		t.Errorf("got %d, want the later YOLO settings (on)", got)
	}
}

// A window that starts mid-line skips the fragment; a line still being written
// is not read either, and is left for the next read.
func TestFragmentsAreSkipped(t *testing.T) {
	data := yoloTurn[20:] + "\n" + chatter + "\n" + managedTurn[:80]
	got, found, consumed := lastCodexYolo([]byte(data), false)
	if found {
		t.Errorf("a fragment was read as settings: %d", got)
	}
	if want := len(yoloTurn[20:]) + 1 + len(chatter) + 1; consumed != want {
		t.Errorf("consumed %d, want %d (up to the last complete line)", consumed, want)
	}
}

func writeRollout(t *testing.T, dir, id string, lines ...string) string {
	t.Helper()
	day := filepath.Join(dir, "sessions", "2026", "09", "25")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "rollout-2026-09-25T08-00-00-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendRollout(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

// filler is a turn's worth of output: lines that say nothing about settings.
func filler(bytes int) []string {
	var lines []string
	for total := 0; total < bytes; total += len(chatter) + 1 {
		lines = append(lines, chatter)
	}
	return lines
}

func TestALongTurnAfterTheSettingsIsSearchedBack(t *testing.T) {
	dir := t.TempDir()
	path := writeRollout(t, dir, "01a0aaaa-0000-7000-8000-000000000001",
		append([]string{`{"type":"session_meta","payload":{}}`, yoloTurn}, filler(2*codexYoloTail)...)...)
	if got := codexYoloFromFile(path); got != CodexYoloOn {
		t.Errorf("settings %d KB back were not found: %d", 2*codexYoloTail/1024, got)
	}
}

func TestSettingsBeyondTheWindowAreUnknown(t *testing.T) {
	dir := t.TempDir()
	path := writeRollout(t, dir, "01a0aaaa-0000-7000-8000-000000000002",
		append([]string{yoloTurn}, filler(codexYoloMaxWindow+64*1024)...)...)
	if got := codexYoloFromFile(path); got != CodexYoloUnknown {
		t.Errorf("got %d, want unknown rather than a whole-file read", got)
	}
}

func TestARolloutBeforeItsFirstTurnIsUnknown(t *testing.T) {
	dir := t.TempDir()
	path := writeRollout(t, dir, "01a0aaaa-0000-7000-8000-000000000003",
		`{"type":"session_meta","payload":{"id":"x"}}`, chatter)
	if got := codexYoloFromFile(path); got != CodexYoloUnknown {
		t.Errorf("got %d, want unknown", got)
	}
}

// An unchanged file is not read again, and a file that grew is read on from
// where the last read stopped.
func TestTheReadingIsCachedAndFollowsAppends(t *testing.T) {
	dir := t.TempDir()
	path := writeRollout(t, dir, "01a0aaaa-0000-7000-8000-000000000004", managedTurn, chatter)
	if got := codexYoloFromFile(path); got != CodexYoloOff {
		t.Fatalf("got %d, want off", got)
	}

	// Same size and time, different content: only a cache answers off now.
	info, _ := os.Stat(path)
	// "disabled" is one byte longer than "managed"; the spaces make up for it.
	swapped := strings.Replace(managedTurn, `"on-request"`, `"never"    `, 1)
	swapped = strings.Replace(swapped, `"type":"managed"`, `"type":"disabled"`, 1)
	if len(swapped) != len(managedTurn) {
		t.Fatalf("the stand-in line changed length: %d vs %d", len(swapped), len(managedTurn))
	}
	if err := os.WriteFile(path, []byte(swapped+"\n"+chatter+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := codexYoloFromFile(path); got != CodexYoloOff {
		t.Errorf("an unchanged file was read again: %d", got)
	}

	appendRollout(t, path, chatter, chatter)
	if got := codexYoloFromFile(path); got != CodexYoloOff {
		t.Errorf("appending chatter changed the reading: %d", got)
	}
	appendRollout(t, path, yoloSettings)
	if got := codexYoloFromFile(path); got != CodexYoloOn {
		t.Errorf("the appended settings were missed: %d", got)
	}
}

// codexTabInstance is a Claude session with a Codex tab on a conversation.
func codexTabInstance(id string, autoYes, tabAutoYes bool) *Instance {
	return &Instance{ID: "cy-" + id, Status: StatusRunning, Agent: AgentClaude, AutoYes: autoYes,
		FollowedWindows: []FollowedWindow{{Index: 2, Agent: AgentCodex, ResumeSessionID: id, AutoYes: tabAutoYes}}}
}

func TestTheBadgeForACodexTab(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	yolo, asking, fresh := "01a0bbbb-0000-7000-8000-000000000001", "01a0bbbb-0000-7000-8000-000000000002", "01a0bbbb-0000-7000-8000-000000000003"
	writeRollout(t, dir, yolo, `{"type":"session_meta"}`, yoloSettings)
	writeRollout(t, dir, asking, `{"type":"session_meta"}`, yoloTurn, askSettings)
	writeRollout(t, dir, fresh, `{"type":"session_meta"}`)

	cases := []struct {
		name string
		inst *Instance
		want YoloReading
	}{
		{"YOLO in effect", codexTabInstance(yolo, true, false), YoloReading{On: true}},
		{"in effect though not asked for", codexTabInstance(yolo, false, false), YoloReading{On: true}},
		{"asked for, not in effect", codexTabInstance(asking, true, false), YoloReading{NotInEffect: true}},
		{"asked for on the tab only", codexTabInstance(asking, false, true), YoloReading{NotInEffect: true}},
		{"not asked for, not in effect", codexTabInstance(asking, false, false), YoloReading{}},
		{"no settings yet", codexTabInstance(fresh, true, false), YoloReading{}},
		{"no conversation", codexTabInstance("", true, false), YoloReading{}},
		{"conversation without a file", codexTabInstance("01a0bbbb-0000-7000-8000-00000000ffff", true, false), YoloReading{}},
	}
	for _, c := range cases {
		if got := c.inst.codexYoloReading(context.Background(), 2); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// yoloServer runs the server-side script for real, against a home directory
// of the test's making.
type yoloServer struct {
	*scriptedExecutor
	home  string
	asked int
}

func (y *yoloServer) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	if len(args) == 3 && args[0] == "sh" && strings.Contains(args[2], "thread_settings_applied") {
		y.asked++
		cmd := exec.CommandContext(ctx, "sh", "-c", args[2])
		cmd.Env = append(os.Environ(), "HOME="+y.home)
		out, err := cmd.Output()
		if err != nil {
			return out, nil, 1, nil
		}
		return out, nil, 0, nil
	}
	return nil, nil, 0, nil
}

// A tab on a server is read there, and the badge lights up for it the same
// way, through the method the sidebar calls.
func TestACodexTabOnAServerIsReadThere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs the server's sh script")
	}
	home := t.TempDir()
	id := "01a0cccc-0000-7000-8000-000000000001"
	path := writeRollout(t, filepath.Join(home, ".codex"), id,
		append([]string{`{"type":"session_meta"}`, yoloTurn}, filler(64*1024)...)...)
	server := &yoloServer{scriptedExecutor: newScriptedExecutor(), home: home}
	SetExecutor("cy-remote", server)
	t.Cleanup(func() { ClearExecutor("cy-remote") })

	inst := codexTabInstance(id, true, false)
	inst.ID, inst.ServerID = "cy-remote", "srvY"
	if got := inst.DetectYoloStateForWindowContext(context.Background(), 2); got != (YoloReading{On: true}) {
		t.Errorf("got %+v, want YOLO on", got)
	}
	if !inst.DetectYoloForWindowContext(context.Background(), 2) {
		t.Error("the existing badge does not light up for a Codex tab")
	}

	// Kept for a while: the sidebar polls far more often than this changes.
	asked := server.asked
	appendRollout(t, path, askSettings)
	if got := inst.DetectYoloStateForWindowContext(context.Background(), 2); !got.On || server.asked != asked {
		t.Errorf("the server was asked again at once (%d → %d): %+v", asked, server.asked, got)
	}
	codexYoloRemoteMu.Lock()
	for key, answer := range codexYoloRemoteCache {
		answer.at = time.Now().Add(-codexYoloRemoteLifetime)
		codexYoloRemoteCache[key] = answer
	}
	codexYoloRemoteMu.Unlock()
	if got := inst.DetectYoloStateForWindowContext(context.Background(), 2); got != (YoloReading{NotInEffect: true}) {
		t.Errorf("after the daemon took over: got %+v, want the warning", got)
	}
}
