package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A new tab can start on an existing conversation, so the work carries over
// from wherever it was left. These cover the two halves: finding the
// conversations to offer, and starting the agent on the one chosen.

// fakeFiles is a machine's records held in memory.
type fakeFiles struct {
	homeDir string
	byPath  map[string]string
	// reads records what was asked for, so a test can show the listing went
	// to the right machine rather than quietly reading this one.
	mu    sync.Mutex
	reads []string
}

func (f *fakeFiles) home() (string, error) { return f.homeDir, nil }

func (f *fakeFiles) readFile(path string) ([]byte, error) {
	f.mu.Lock()
	f.reads = append(f.reads, path)
	f.mu.Unlock()
	contents, found := f.byPath[path]
	if !found {
		return nil, fmt.Errorf("no such file: %s", path)
	}
	return []byte(contents), nil
}

func (f *fakeFiles) exists(path string) bool {
	_, found := f.byPath[path]
	return found
}

func (f *fakeFiles) join(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func historyLine(t *testing.T, sessionID, project, display string, at time.Time) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"sessionId": sessionID,
		"project":   project,
		"display":   display,
		"timestamp": at.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

// transcript is the smallest conversation the parser accepts as real: one
// exchange. A transcript with no messages is filtered out by design.
func transcript(t *testing.T, sessionID string) string {
	t.Helper()
	at := time.Now().UTC().Format(time.RFC3339)
	lines := []string{
		fmt.Sprintf(`{"type":"user","sessionId":%q,"timestamp":%q,"message":{"role":"user","content":"look at the parser"}}`,
			sessionID, at),
		fmt.Sprintf(`{"type":"assistant","sessionId":%q,"timestamp":%q,"message":{"role":"assistant","content":"looking"}}`,
			sessionID, at),
	}
	return strings.Join(lines, "\n") + "\n"
}

// The point of the whole feature: the conversations offered are the ones on
// the machine the tab will run on, read from that machine.
func TestConversationsAreListedFromTheServer(t *testing.T) {
	const sessionID = "11111111-2222-3333-4444-555555555555"
	const project = "/srv/work/api"

	files := &fakeFiles{
		homeDir: "/home/deploy",
		byPath: map[string]string{
			"/home/deploy/.claude/history.jsonl": historyLine(
				t, sessionID, project, "look at the parser on the server", time.Now()) + "\n",
			"/home/deploy/.claude/projects/-srv-work-api/" + sessionID + ".jsonl": transcript(t, sessionID),
		},
	}

	found, err := listAgentSessionsByHistoryFrom(files, project)
	if err != nil {
		t.Fatalf("listing failed: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("got %d conversations, want the one the server holds", len(found))
	}
	if found[0].SessionID != sessionID {
		t.Errorf("conversation id = %q", found[0].SessionID)
	}

	// The home directory is the server's. Reading /home/izll here would mean
	// the local disk answered, and the list would offer conversations this
	// tab cannot resume.
	for _, path := range files.reads {
		if !strings.HasPrefix(path, "/home/deploy/") {
			t.Errorf("read %q, which is not on the server being listed", path)
		}
	}
}

// The server is Unix whatever this computer is. Building its paths with the
// local separator finds nothing from a Windows desktop.
func TestServerPathsAreBuiltForTheServer(t *testing.T) {
	remote := remoteFiles{}
	if got := remote.join("/home/deploy", ".claude", "history.jsonl"); got != "/home/deploy/.claude/history.jsonl" {
		t.Errorf("a server path was built as %q", got)
	}
}

// A conversation whose transcript is gone is not offered: --resume would fail
// on it, and an entry that cannot be chosen is worse than no entry.
func TestAConversationWithNoTranscriptIsNotOffered(t *testing.T) {
	const sessionID = "99999999-8888-7777-6666-555555555555"
	const project = "/srv/work/api"

	files := &fakeFiles{
		homeDir: "/home/deploy",
		byPath: map[string]string{
			"/home/deploy/.claude/history.jsonl": historyLine(
				t, sessionID, project, "a conversation whose transcript was deleted", time.Now()) + "\n",
		},
	}

	found, err := listAgentSessionsByHistoryFrom(files, project)
	if err != nil {
		t.Fatalf("listing failed: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("offered %d conversations that cannot be resumed", len(found))
	}
}

// A project on the server is not a project on this computer, even at the same
// path spelled the same way.
func TestAnotherProjectsConversationsAreNotOffered(t *testing.T) {
	const sessionID = "11111111-2222-3333-4444-555555555555"

	files := &fakeFiles{
		homeDir: "/home/deploy",
		byPath: map[string]string{
			"/home/deploy/.claude/history.jsonl": historyLine(
				t, sessionID, "/srv/work/other", "belongs to a different project", time.Now()) + "\n",
			"/home/deploy/.claude/projects/-srv-work-other/" + sessionID + ".jsonl": transcript(t, sessionID),
		},
	}

	found, err := listAgentSessionsByHistoryFrom(files, "/srv/work/api")
	if err != nil {
		t.Fatalf("listing failed: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("offered %d conversations from another project", len(found))
	}
}

// Nothing stored yet is an empty list, not a failure: the dialog still has to
// offer "start a new conversation".
func TestAServerWithNoHistoryListsNothing(t *testing.T) {
	files := &fakeFiles{homeDir: "/home/deploy", byPath: map[string]string{}}

	found, err := listAgentSessionsByHistoryFrom(files, "/srv/work/api")
	if err != nil {
		t.Fatalf("a server with no history reported an error: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("got %d conversations from an empty server", len(found))
	}
}

// A server that is not connected must say so. Falling back to this computer
// would list conversations the tab could never resume — which looks like a
// working feature until the agent starts and finds nothing.
func TestAnUnconnectedServerIsAnErrorNotTheLocalDisk(t *testing.T) {
	if _, err := FilesOn("session-with-no-route", "srv-1"); err == nil {
		t.Error("an unconnected server was answered with the local disk")
	}

	local, err := FilesOn("any-session", "")
	if err != nil {
		t.Fatalf("the local machine was refused: %v", err)
	}
	if _, isLocal := local.(localFiles); !isLocal {
		t.Error("an empty server id did not give the local disk")
	}
}

// --- starting the tab on the chosen conversation ------------------------

// tabArgv creates a tab against a scripted multiplexer and returns the command
// the agent was started with.
func tabArgv(t *testing.T, inst *Instance, req NewTabRequest) []string {
	t.Helper()

	executor := newScriptedExecutor()
	// new-window prints the index the caller parses back.
	executor.answers["new-window"] = "7"
	// Registered under the session's own server, which is where a tab with no
	// server of its own runs.
	SetExecutor(inst.ID, executor)
	t.Cleanup(func() { ClearExecutor(inst.ID) })
	req.ServerID = inst.ServerID
	if req.WorkDir == "" {
		req.WorkDir = inst.Path
	}

	if _, err := inst.NewAgentTab(req); err != nil {
		t.Fatalf("creating the tab failed: %v", err)
	}

	for _, args := range executor.commandsNamed("new-window") {
		return args
	}
	t.Fatal("no window was created")
	return nil
}

func runningInstance(t *testing.T) *Instance {
	t.Helper()
	// A session with a server id, so its commands go through the registered
	// executor instead of this computer's real multiplexer.
	return &Instance{
		ID:       "session-1",
		ServerID: "srv-test",
		Path:     "/srv/work",
		Agent:    AgentClaude,
		Status:   StatusRunning,
	}
}

// The conversation chosen in the dialog is the one the agent is told to
// continue.
func TestAChosenConversationIsResumed(t *testing.T) {
	const resumeID = "11111111-2222-3333-4444-555555555555"

	argv := tabArgv(t, runningInstance(t), NewTabRequest{
		Name:     "claude tab",
		Agent:    AgentClaude,
		ResumeID: resumeID,
	})

	line := strings.Join(argv, " ")
	if !strings.Contains(line, "--resume "+resumeID) {
		t.Errorf("the tab did not resume the chosen conversation: %s", line)
	}
	// --session-id names a NEW conversation. Sent alongside --resume it asks
	// for both at once, and the agent refuses to start.
	if strings.Contains(line, "--session-id") {
		t.Errorf("a new conversation was requested as well as a resumed one: %s", line)
	}
}

// Choosing nothing still names a conversation, so the tab has something to
// resume from next time.
func TestNoChoiceStartsANamedNewConversation(t *testing.T) {
	argv := tabArgv(t, runningInstance(t), NewTabRequest{
		Name:  "claude tab",
		Agent: AgentClaude,
	})

	line := strings.Join(argv, " ")
	if !strings.Contains(line, "--session-id") {
		t.Errorf("a new tab was left with no conversation id: %s", line)
	}
	if strings.Contains(line, "--resume") {
		t.Errorf("a fresh tab was told to resume: %s", line)
	}
}

// Codex and Amazon Q spell resume as a subcommand, which has to come first.
func TestSubcommandAgentsPutResumeFirst(t *testing.T) {
	const resumeID = "01931f8a-1234-7890-abcd-ef1234567890"

	inst := runningInstance(t)
	inst.AutoYes = true
	argv := tabArgv(t, inst, NewTabRequest{
		Name:     "codex tab",
		Agent:    AgentCodex,
		ResumeID: resumeID,
	})

	config := AgentConfigs[AgentCodex]
	if !config.ResumeIsSubcommand {
		t.Skip("codex no longer uses a resume subcommand")
	}

	// The subcommand comes before its flags, and the id after them.
	resumeAt, idAt := -1, -1
	for i, arg := range argv {
		if arg == config.ResumeFlag && resumeAt == -1 {
			resumeAt = i
		}
		if arg == resumeID {
			idAt = i
		}
	}
	if resumeAt == -1 || idAt == -1 {
		t.Fatalf("resume subcommand or id missing: %v", argv)
	}
	if resumeAt > idAt {
		t.Errorf("the id came before the subcommand: %v", argv)
	}
}

// The id reaches a command line, so its shape is checked rather than trusted.
func TestAnIllShapedConversationIdIsRefused(t *testing.T) {
	executor := newScriptedExecutor()
	executor.answers["new-window"] = "7"
	inst := runningInstance(t)
	SetExecutor(inst.ID, executor)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	_, err := inst.NewAgentTab(NewTabRequest{
		Name:     "claude tab",
		Agent:    AgentClaude,
		ResumeID: "; rm -rf ~",
	})
	if err == nil {
		t.Fatal("an id that is not a conversation id was accepted")
	}
	if len(executor.commandsNamed("new-window")) != 0 {
		t.Error("a window was created for a refused id")
	}
}

// An agent that cannot resume must say so rather than start a fresh
// conversation while the user believes they resumed one.
func TestAnAgentThatCannotResumeRefuses(t *testing.T) {
	config := AgentConfigs[AgentAider]
	if config.SupportsResume {
		t.Skip("aider now supports resume")
	}

	executor := newScriptedExecutor()
	executor.answers["new-window"] = "7"
	inst := runningInstance(t)
	SetExecutor(inst.ID, executor)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	_, err := inst.NewAgentTab(NewTabRequest{
		Name:     "aider tab",
		Agent:    AgentAider,
		ResumeID: "11111111-2222-3333-4444-555555555555",
	})
	if err == nil {
		t.Error("an agent with no resume support accepted a conversation to resume")
	}
}

// The tab remembers which conversation it is on, so restarting it later
// continues the same one rather than starting over.
func TestTheResumedConversationIsRecordedOnTheTab(t *testing.T) {
	const resumeID = "11111111-2222-3333-4444-555555555555"

	inst := runningInstance(t)
	executor := newScriptedExecutor()
	executor.answers["new-window"] = "7"
	SetExecutor(inst.ID, executor)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	idx, err := inst.NewAgentTab(NewTabRequest{
		ServerID: inst.ServerID,
		WorkDir:  inst.Path,
		Name:     "claude tab",
		Agent:    AgentClaude,
		ResumeID: resumeID,
	})
	if err != nil {
		t.Fatalf("creating the tab failed: %v", err)
	}

	for _, window := range inst.FollowedWindows {
		if window.Index == idx {
			if window.ResumeSessionID != resumeID {
				t.Errorf("the tab recorded conversation %q, want %q",
					window.ResumeSessionID, resumeID)
			}
			return
		}
	}
	t.Error("the new tab was not recorded")
}

// The local listing must keep reading the local disk exactly as before.
func TestTheLocalListingIsUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("home directory cannot be overridden on this platform")
	}

	const sessionID = "11111111-2222-3333-4444-555555555555"
	project := filepath.Join(home, "work")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	claudeDir := filepath.Join(home, ".claude")
	projectDir := filepath.Join(claudeDir, "projects", claudeProjectDirName(project))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "history.jsonl"),
		[]byte(historyLine(t, sessionID, project, "a local conversation to resume", time.Now())+"\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"),
		[]byte(transcript(t, sessionID)), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := ListAgentSessionsByHistory(project)
	if err != nil {
		t.Fatalf("the local listing failed: %v", err)
	}
	if len(found) != 1 || found[0].SessionID != sessionID {
		t.Errorf("the local listing returned %d conversations", len(found))
	}
}
