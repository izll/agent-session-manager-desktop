package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const bypassFlag = "--dangerously-bypass-approvals-and-sandbox"

// codexServer is a server with codex on it, answering `codex --help` the way
// the installed version would.
type codexServer struct {
	scriptedExecutor
	helpMu sync.Mutex
	// help is what `codex --help` prints; empty with helpFails set means the
	// probe itself fails.
	help      string
	helpFails bool
	helpAsked int
}

func newCodexServer(help string) *codexServer {
	server := &codexServer{help: help}
	server.answers = map[string]string{"new-window": "100\n"}
	server.failWith = map[string]error{}
	return server
}

func (c *codexServer) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	if len(args) == 2 && args[0] == "codex" && args[1] == "--help" {
		c.helpMu.Lock()
		c.helpAsked++
		c.helpMu.Unlock()
		if c.helpFails {
			return nil, nil, 0, errors.New("connection dropped")
		}
		return []byte(c.help), nil, 0, nil
	}
	if len(args) == 2 && args[0] == "id" && args[1] == "-u" {
		return []byte("1000\n"), nil, 0, nil
	}
	// command -v codex, and anything else: present, fine.
	return nil, nil, 0, nil
}

func (c *codexServer) asked() int {
	c.helpMu.Lock()
	defer c.helpMu.Unlock()
	return c.helpAsked
}

// The help of a codex that has the background server (0.157), trimmed.
const helpWithDaemon = `Codex CLI

Usage: codex [OPTIONS] [PROMPT]

Options:
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing
      --no-daemon
          Run without the shared background server, even if it is already running
`

// The same from a codex that predates it, and would refuse the flag.
const helpWithoutDaemon = `Codex CLI

Usage: codex [OPTIONS] [PROMPT]

Options:
      --dangerously-bypass-approvals-and-sandbox
          Skip all confirmation prompts and execute commands without sandboxing
`

// freshDaemonState starts a test from the defaults: no daemon, nothing probed.
func freshDaemonState(t *testing.T) {
	t.Helper()
	SetCodexUseDaemon(false)
	resetAgentFlagProbes()
	t.Cleanup(func() {
		SetCodexUseDaemon(false)
		resetAgentFlagProbes()
	})
}

// launchedArgv returns the agent command a new-window was given: everything
// from the agent's name on.
func launchedArgv(t *testing.T, server *codexServer) []string {
	t.Helper()
	windows := server.commandsNamed("new-window")
	if len(windows) != 1 {
		t.Fatalf("expected one window, got %d: %v", len(windows), windows)
	}
	for index, arg := range windows[0] {
		if arg == "codex" {
			return windows[0][index:]
		}
	}
	t.Fatalf("the window does not run codex: %v", windows[0])
	return nil
}

func startCodexTab(t *testing.T, sessionID string, server *codexServer, req NewTabRequest) []string {
	t.Helper()
	SetTabExecutor(sessionID, "srvX", server)
	t.Cleanup(func() { ClearTabExecutor(sessionID, "srvX") })

	inst := &Instance{ID: sessionID, Status: StatusRunning, Path: "/work", AutoYes: true}
	req.ServerID, req.Name, req.Agent, req.WorkDir = "srvX", "codex tab", AgentCodex, "/srv/work"
	if _, err := inst.NewAgentTab(req); err != nil {
		t.Fatalf("the tab was not created: %v", err)
	}
	return launchedArgv(t, server)
}

// By default Codex is kept off its background server — asked of the server the
// tab runs on — and the YOLO flag is still passed alongside.
//
// In daemon mode the session's approval and sandbox settings do not reach the
// thread, so without --no-daemon a YOLO tab asks for approval in a sandbox.
func TestCodexStartsWithoutItsDaemonByDefault(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cd1", server, NewTabRequest{})

	if indexOf(argv, "--no-daemon") < 0 {
		t.Errorf("codex was started on its background server: %v", argv)
	}
	if indexOf(argv, bypassFlag) < 0 {
		t.Errorf("the YOLO flag went missing: %v", argv)
	}
	if server.asked() != 1 {
		t.Errorf("the server's codex was asked %d times, want once", server.asked())
	}
}

// A resume is a subcommand, and the flag has to land on it.
func TestAResumedCodexTabGetsTheFlagOnTheSubcommand(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)
	id := "019a0000-0000-7000-8000-000000000001"

	argv := startCodexTab(t, "cd2", server, NewTabRequest{ResumeID: id})

	want := []string{"codex", "resume", bypassFlag, id, "--no-daemon"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("resume argv = %v, want %v", argv, want)
	}
}

// The user's own extra arguments come after the app's, flag included.
func TestTheFlagComesBeforeTheUsersExtraArgs(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cd3", server, NewTabRequest{ExtraArgs: "--model gpt-5"})

	if flag, extra := indexOf(argv, "--no-daemon"), indexOf(argv, "--model"); flag < 0 || extra < flag {
		t.Errorf("argv = %v, want --no-daemon before the extra arguments", argv)
	}
}

// Written by the user already: passing it twice makes codex refuse to start.
func TestAFlagTheUserAlreadyGaveIsNotRepeated(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cd4", server, NewTabRequest{ExtraArgs: "--no-daemon"})

	count := 0
	for _, arg := range argv {
		if arg == "--no-daemon" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("--no-daemon appears %d times: %v", count, argv)
	}
}

// With the daemon chosen in the settings, the flag is left out.
func TestChoosingTheDaemonLeavesTheFlagOut(t *testing.T) {
	freshDaemonState(t)
	SetCodexUseDaemon(true)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cd5", server, NewTabRequest{})

	if indexOf(argv, "--no-daemon") >= 0 {
		t.Errorf("the daemon was chosen, but codex was kept off it: %v", argv)
	}
	if indexOf(argv, bypassFlag) < 0 {
		t.Errorf("the YOLO flag went missing: %v", argv)
	}
}

// An older codex does not know the flag and would exit with "unexpected
// argument", so it is not given it.
func TestAnOlderCodexIsNotGivenTheFlag(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithoutDaemon)

	argv := startCodexTab(t, "cd6", server, NewTabRequest{})

	if indexOf(argv, "--no-daemon") >= 0 {
		t.Errorf("a codex without the flag was given it: %v", argv)
	}
}

// A probe that fails must not stop the tab: it starts, without the flag, and
// the next start asks again rather than trusting the failure.
func TestAFailedProbeStartsCodexWithoutTheFlag(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer("")
	server.helpFails = true

	argv := startCodexTab(t, "cd7", server, NewTabRequest{})

	if indexOf(argv, "--no-daemon") >= 0 {
		t.Errorf("the flag was added on a failed probe: %v", argv)
	}

	server.helpFails = false
	server.help = helpWithDaemon
	if !(&Instance{ID: "cd7"}).agentSupportsFlag("srvX", "codex", "--no-daemon") {
		t.Error("a failed probe was cached and the flag stays off after the server recovered")
	}
}

// Asked once, not on every start: several Codex tabs opened together probe
// the server once.
func TestTheAnswerIsKeptPerServer(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)
	SetTabExecutor("cd8", "srvX", server)
	t.Cleanup(func() { ClearTabExecutor("cd8", "srvX") })

	inst := &Instance{ID: "cd8"}
	for range 3 {
		if !inst.agentSupportsFlag("srvX", "codex", "--no-daemon") {
			t.Fatal("the flag was not found in the help")
		}
	}
	if server.asked() != 1 {
		t.Errorf("codex --help ran %d times, want once", server.asked())
	}

	// A different server is a different codex.
	other := newCodexServer(helpWithoutDaemon)
	SetTabExecutor("cd8", "srvY", other)
	t.Cleanup(func() { ClearTabExecutor("cd8", "srvY") })
	if inst.agentSupportsFlag("srvY", "codex", "--no-daemon") {
		t.Error("one server's answer was used for another")
	}
}

// A fork is `codex fork <id>`, and the flag goes on that subcommand too.
func TestAForkedCodexTabGetsTheFlagOnTheSubcommand(t *testing.T) {
	freshDaemonState(t)
	server := newCodexServer(helpWithDaemon)
	server.answers["new-window"] = "3\n"
	SetExecutor("cd9", server)
	t.Cleanup(func() { ClearExecutor("cd9") })

	inst := &Instance{ID: "cd9", ServerID: "srvX", Status: StatusRunning, Path: "/work",
		Agent: AgentCodex, AutoYes: true}
	id := "019a0000-0000-7000-8000-000000000002"
	if _, err := inst.NewForkedTab("fork", id); err != nil {
		t.Fatalf("the fork was not created: %v", err)
	}

	argv := launchedArgv(t, server)
	want := []string{"codex", bypassFlag, "fork", id, "--no-daemon"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("fork argv = %v, want %v", argv, want)
	}
}

// Every way a Codex pane is started goes through the one place that decides
// on the flag — start, restore, restart of the main window and of a tab, a new
// tab and a fork. A path that built its argv directly would start the daemon
// again, and only for that path.
func TestEveryAgentLaunchDecidesOnTheDaemon(t *testing.T) {
	source := readSource(t, "instance.go")
	if strings.Contains(source, "buildAgentArgv(") {
		t.Error("instance.go builds an agent command without agentArgv, " +
			"so that path does not keep Codex off its background server")
	}
	if got := strings.Count(source, "i.agentArgv("); got < 6 {
		t.Errorf("agentArgv is used by %d launch paths, want all 6", got)
	}
}

// Codex on this computer is probed too, through the codex on PATH.
func TestALocalCodexIsProbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in codex is a shell script")
	}
	freshDaemonState(t)

	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'EOF'\n" + helpWithDaemon + "EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	inst := &Instance{ID: "cd10"}
	argv, _ := inst.agentArgv(AgentConfigs[AgentCodex], "", []string{bypassFlag}, "")
	want := []string{"codex", bypassFlag, "--no-daemon"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("local argv = %v, want %v", argv, want)
	}

	// Another agent has no background server and is left alone.
	if got, _ := inst.agentArgv(AgentConfigs[AgentClaude], "", nil, ""); len(got) != 1 {
		t.Errorf("claude was given extra flags: %v", got)
	}
}

// The flag is matched as a word, not as part of a longer one.
func TestHelpListsFlagMatchesWholeWords(t *testing.T) {
	if !helpListsFlag(helpWithDaemon, "--no-daemon") {
		t.Error("the flag was not found in help that lists it")
	}
	if helpListsFlag("      --no-daemon-restart\n", "--no-daemon") {
		t.Error("a longer flag was taken for this one")
	}
	if !helpListsFlag("  --no-daemon\r\n", "--no-daemon") {
		t.Error("help with Windows line endings was not read")
	}

	// And that is how the server's answer is read.
	freshDaemonState(t)
	server := newCodexServer("      --no-daemon-restart\n")
	SetTabExecutor("cd11", "srvX", server)
	t.Cleanup(func() { ClearTabExecutor("cd11", "srvX") })
	if (&Instance{ID: "cd11"}).agentSupportsFlag("srvX", "codex", "--no-daemon") {
		t.Error("a codex that only has a longer flag was taken to have this one")
	}
}

// A settings file from before the setting existed means no daemon.
func TestAnOlderSettingsFileKeepsCodexOffItsDaemon(t *testing.T) {
	var settings Settings
	if err := json.Unmarshal([]byte(`{"compact_list":true}`), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.CodexUseDaemon {
		t.Error("a settings file without the field turned the daemon on")
	}

	settings.CodexUseDaemon = true
	out, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"codex_use_daemon":true`) {
		t.Errorf("the choice is not stored: %s", out)
	}
}
