package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const heldID = "01a0c8ba-c835-7092-9030-75b378658b7c"

// fakeProc lays out a /proc with one process: its command line and the files
// it has open.
func fakeProc(t *testing.T, root, pid string, argv []string, open ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in /proc uses symbolic links")
	}
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, target := range open {
		if err := os.Symlink(target, filepath.Join(dir, "fd", string(rune('3'+n)))); err != nil {
			t.Fatal(err)
		}
	}
}

var daemonArgv = []string{
	"/home/u/.codex/packages/app-server-daemon/0.157.0/codex",
	"app-server", "--listen", "unix://", "--managed-daemon",
}

func rolloutPath(id string) string {
	return "/home/u/.codex/sessions/2026/09/22/rollout-2026-09-22T12-47-59-" + id + ".jsonl"
}

func TestTheDaemonHoldingTheRolloutIsFound(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "100", []string{"bash"})
	fakeProc(t, root, "200", daemonArgv, "/dev/null", rolloutPath(heldID))

	if !codexDaemonHoldsInProc(context.Background(), root, heldID) {
		t.Error("the daemon holds the conversation, but it was not found")
	}
	if codexDaemonHoldsInProc(context.Background(), root, "01a0c668-6789-7290-a4f2-301ba044eb36") {
		t.Error("another conversation was reported as held")
	}
}

// The pane's own codex has the file open too; only the daemon's hold matters.
func TestAClientHoldingTheRolloutIsNotTheDaemon(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "300", []string{"/usr/bin/codex", "resume", heldID, "--no-daemon"}, rolloutPath(heldID))
	fakeProc(t, root, "301", []string{"codex", "app-server", "--listen", "stdio://"}, rolloutPath(heldID))

	if codexDaemonHoldsInProc(context.Background(), root, heldID) {
		t.Error("a process that is not the managed daemon was taken for it")
	}
}

func TestTheDaemonIsRecognisedByItsPackagePath(t *testing.T) {
	if !isCodexDaemonCommand([]string{"/home/u/.codex/packages/app-server-daemon/0.158.0/bin/codex-daemon"}) {
		t.Error("the daemon binary under app-server-daemon was not recognised")
	}
	if isCodexDaemonCommand([]string{"codex", "resume", "--no-daemon"}) {
		t.Error("a client was taken for the daemon")
	}
}

// macOS: ps finds the daemon, lsof says what it has open.
func TestLsofAndPsOutputIsRead(t *testing.T) {
	ps := "  101 /bin/zsh\r\n  202 /Users/u/.codex/packages/app-server-daemon/0.157.0/codex app-server --listen unix:// --managed-daemon\n  303 codex resume x\n"
	if got := codexDaemonPIDsFromPS(ps); len(got) != 1 || got[0] != "202" {
		t.Errorf("daemon pids = %v, want [202]", got)
	}
	lsof := "p202\nfcwd\nn/Users/u\nf23\nn" + rolloutPath(heldID) + "\r\n"
	if !codexDaemonHoldsInLsof(lsof, heldID) {
		t.Error("the rollout in lsof's output was missed")
	}
	if codexDaemonHoldsInLsof(lsof, "01a0c668-6789-7290-a4f2-301ba044eb36") {
		t.Error("another conversation was reported as held")
	}
}

func TestTheConversationIsReadFromTheArguments(t *testing.T) {
	codex := AgentConfigs[AgentCodex]
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"resume", bypassFlag, heldID}, heldID},
		{[]string{bypassFlag, "fork", heldID}, heldID},
		{[]string{bypassFlag}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := conversationArg(codex, c.args); got != c.want {
			t.Errorf("conversationArg(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

// stubDaemonHold answers the hold check and records who asked.
func stubDaemonHold(t *testing.T, held bool) *[]string {
	t.Helper()
	var mu sync.Mutex
	asked := []string{}
	previous := codexDaemonHoldsConversation
	codexDaemonHoldsConversation = func(_ *Instance, serverID, id string) bool {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, serverID+"/"+id)
		return held
	}
	t.Cleanup(func() { codexDaemonHoldsConversation = previous })
	return &asked
}

// captureDaemonNotices collects what the app would be told.
func captureDaemonNotices(t *testing.T) <-chan CodexDaemonHeldNotice {
	t.Helper()
	notices := make(chan CodexDaemonHeldNotice, 4)
	SetCodexDaemonHeldHandler(func(n CodexDaemonHeldNotice) { notices <- n })
	t.Cleanup(func() { SetCodexDaemonHeldHandler(nil) })
	return notices
}

// A conversation the daemon holds is continued through it — leaving out
// --no-daemon — and the user is told.
func TestAHeldConversationIsContinuedThroughTheDaemon(t *testing.T) {
	freshDaemonState(t)
	asked := stubDaemonHold(t, true)
	notices := captureDaemonNotices(t)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cdh1", server, NewTabRequest{ResumeID: heldID})

	if indexOf(argv, "--no-daemon") >= 0 {
		t.Errorf("a held conversation was opened with --no-daemon, which shows the lock screen: %v", argv)
	}
	if len(*asked) != 1 || (*asked)[0] != "srvX/"+heldID {
		t.Errorf("hold checks = %v, want one on srvX for the conversation", *asked)
	}
	select {
	case n := <-notices:
		if n.SessionID != "cdh1" || n.ServerID != "srvX" || n.ConversationID != heldID {
			t.Errorf("notice = %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Error("the user was not told that YOLO is not in effect")
	}
}

func TestAConversationTheDaemonDoesNotHoldKeepsTheFlag(t *testing.T) {
	freshDaemonState(t)
	stubDaemonHold(t, false)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cdh2", server, NewTabRequest{ResumeID: heldID})

	want := []string{"codex", "resume", bypassFlag, heldID, "--no-daemon"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

// A new conversation has nothing to be held, and the check is not made.
func TestANewConversationIsNotChecked(t *testing.T) {
	freshDaemonState(t)
	asked := stubDaemonHold(t, true)
	server := newCodexServer(helpWithDaemon)

	argv := startCodexTab(t, "cdh3", server, NewTabRequest{})

	if indexOf(argv, "--no-daemon") < 0 {
		t.Errorf("a new conversation lost --no-daemon: %v", argv)
	}
	if len(*asked) != 0 {
		t.Errorf("a new conversation was checked: %v", *asked)
	}
}

// With the daemon chosen in Settings nothing changes: no flag, no check.
func TestChoosingTheDaemonSkipsTheCheck(t *testing.T) {
	freshDaemonState(t)
	SetCodexUseDaemon(true)
	asked := stubDaemonHold(t, true)
	server := newCodexServer(helpWithDaemon)

	startCodexTab(t, "cdh4", server, NewTabRequest{ResumeID: heldID})

	if len(*asked) != 0 {
		t.Errorf("the check ran although the daemon is chosen: %v", *asked)
	}
}

// heldServer answers the server-side check the way a server whose daemon
// holds the conversation would.
type heldServer struct {
	*codexServer
	script string
}

func (h *heldServer) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	if len(args) == 3 && args[0] == "sh" && args[1] == "-c" && strings.Contains(args[2], "--managed-daemon") {
		h.script = args[2]
		return []byte("held\n"), nil, 0, nil
	}
	return h.codexServer.RunShell(ctx, dir, args...)
}

func TestAServerIsAskedThroughItsShell(t *testing.T) {
	freshDaemonState(t)
	server := &heldServer{codexServer: newCodexServer(helpWithDaemon)}
	SetTabExecutor("cdh5", "srvX", server)
	t.Cleanup(func() { ClearTabExecutor("cdh5", "srvX") })

	inst := &Instance{ID: "cdh5"}
	if !codexDaemonHoldsConversation(inst, "srvX", heldID) {
		t.Error("the server said held, but the answer was no")
	}
	if !strings.Contains(server.script, "'-"+heldID+".jsonl'") {
		t.Errorf("the script does not look for the conversation's rollout: %s", server.script)
	}
	if codexDaemonHoldsConversation(inst, "srvX", "bad id; rm -rf /") {
		t.Error("an unsafe id reached the server")
	}
}

// The script the server runs is valid sh, and on a machine with /proc it finds
// a daemon holding the file.
func TestTheServerScriptRuns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	out, err := exec.Command("sh", "-c", codexDaemonHeldScript("01a0ffff-0000-7000-8000-00000000dead")).CombinedOutput()
	if err == nil || strings.TrimSpace(string(out)) != "" {
		t.Errorf("a conversation nobody holds was reported: err=%v out=%q", err, out)
	}

	// A stand-in daemon: a shell whose command line reads like the daemon's,
	// holding a rollout open.
	dir := t.TempDir()
	id := "01a0ffff-0000-7000-8000-00000000beef"
	rollout := filepath.Join(dir, "rollout-2026-09-25T00-00-00-"+id+".jsonl")
	if err := os.WriteFile(rollout, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemon := exec.Command("sh", "-c", `exec 7<"$1"; while :; do sleep 1; done`, "app-server", rollout, "--managed-daemon")
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Process.Kill(); _ = daemon.Wait() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		out, _ := exec.Command("sh", "-c", codexDaemonHeldScript(id)).Output()
		if strings.TrimSpace(string(out)) == "held" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stand-in daemon's hold was not found: %q", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !codexDaemonHoldsInProc(context.Background(), "/proc", id) {
		t.Error("the local check did not find the stand-in daemon's hold")
	}
}

func TestStoppingTheDaemonReportsTheOutcome(t *testing.T) {
	previous := codexDaemonStopCommand
	t.Cleanup(func() { codexDaemonStopCommand = previous })

	var machine string
	codexDaemonStopCommand = func(_ *Instance, serverID string) (string, error) {
		machine = serverID
		return `{"status":"stopped"}`, nil
	}
	if err := (&Instance{}).StopCodexDaemon("srvX"); err != nil || machine != "srvX" {
		t.Errorf("stop on srvX: err=%v machine=%q", err, machine)
	}

	codexDaemonStopCommand = func(_ *Instance, _ string) (string, error) {
		return "error: no daemon", errors.New("exit status 1")
	}
	err := (&Instance{}).StopCodexDaemon("")
	if err == nil || !strings.Contains(err.Error(), "no daemon") {
		t.Errorf("a failed stop was not reported with its output: %v", err)
	}
}
