//go:build !windows

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func backgroundInstance(t *testing.T, tmux func(args ...string) ([]byte, error)) *Instance {
	t.Helper()
	// A home of its own: the command runs in the user's shell, which reads its rc files.
	t.Setenv("HOME", t.TempDir())
	inst := &Instance{ID: "bgcmd", Name: "bgcmd", Path: t.TempDir(), Agent: AgentClaude, Status: StatusRunning}
	if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	return inst
}

func waitForJob(t *testing.T, inst *Instance, job BackgroundJob) BackgroundJobState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := inst.BackgroundJobState(context.Background(), job)
		if state.Finished {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("still running: %+v", state)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// No tab: the session keeps its windows, and the command runs where its tab works.
func TestABackgroundCommandRunsWithoutATab(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	job, err := inst.StartBackgroundCommand(context.Background(), 0, "", `printf '%s' "it's here" > marker`)
	if err != nil {
		t.Fatal(err)
	}
	state := waitForJob(t, inst, job)
	if state.Failed {
		t.Fatalf("state = %+v", state)
	}
	data, err := os.ReadFile(filepath.Join(inst.Path, "marker"))
	if err != nil || string(data) != "it's here" {
		t.Fatalf("marker = %q, %v", data, err)
	}
	windows, _ := tmux("list-windows", "-t", inst.TmuxSessionName(), "-F", "#{window_index}")
	if strings.TrimSpace(string(windows)) != "0" {
		t.Fatalf("windows = %q", windows)
	}
	// Gone without a trace once it worked.
	if sessions, _ := tmux("list-sessions", "-F", "#{session_name}"); strings.Contains(string(sessions), job.Name) {
		t.Fatalf("left behind: %s", sessions)
	}
}

// A failure is the only time the user hears of it, with what it printed last.
func TestAFailedBackgroundCommandIsReported(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	job, err := inst.StartBackgroundCommand(context.Background(), 0, "", "echo first; echo boom >&2; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	state := waitForJob(t, inst, job)
	if !state.Failed || state.ExitCode != 3 || !strings.Contains(state.Output, "boom") || strings.Contains(state.Output, "__ASMGR") {
		t.Fatalf("state = %+v", state)
	}
	inst.ForgetBackgroundJob(context.Background(), job)
	if sessions, _ := tmux("list-sessions", "-F", "#{session_name}"); strings.Contains(string(sessions), job.Name) {
		t.Fatalf("not cleaned up: %s", sessions)
	}
}

// Stopped by the user is not a failure.
func TestAnInterruptedBackgroundCommandIsNoFailure(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	for _, command := range []string{"exit 130", "exit 143"} {
		job, err := inst.StartBackgroundCommand(context.Background(), 0, "", command)
		if err != nil {
			t.Fatal(err)
		}
		if state := waitForJob(t, inst, job); state.Failed {
			t.Fatalf("%s: %+v", command, state)
		}
	}
}

// What the command line is made of stays the command's, whatever it holds.
func TestABackgroundCommandKeepsItsQuotesAndOperators(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	job, err := inst.StartBackgroundCommand(context.Background(), 0, "", `echo "a b" | tr ' ' '_' > out && echo ok >> out # note`)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, inst, job)
	data, _ := os.ReadFile(filepath.Join(inst.Path, "out"))
	if string(data) != "a_b\nok\n" {
		t.Fatalf("out = %q", data)
	}
}

// A hidden session is no tab and no session of the app's: the cleanup of the
// terminal's mirror sessions must leave it alone.
func TestBackgroundJobNamesAreTheirOwn(t *testing.T) {
	name := backgroundJobName()
	if !strings.HasPrefix(name, "asm_bg_") || strings.Contains(name, "_gui_") || name == backgroundJobName() {
		t.Fatalf("name = %q", name)
	}
}

func startJob(t *testing.T, inst *Instance, command string) BackgroundJob {
	t.Helper()
	job, err := inst.StartBackgroundCommand(context.Background(), 0, "", command)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// remain-on-exit set server-wide (as the app does on a remote server, or a user's
// tmux.conf) keeps an ended pane: it still has to count as ended.
func TestABackgroundCommandEndsUnderRemainOnExit(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	if _, err := tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	if state := waitForJob(t, inst, startJob(t, inst, "true")); state.Failed {
		t.Fatalf("success: %+v", state)
	}
	state := waitForJob(t, inst, startJob(t, inst, "echo nope; exit 4"))
	if !state.Failed || state.ExitCode != 4 || !strings.Contains(state.Output, "nope") || strings.Contains(state.Output, "Pane is dead") {
		t.Fatalf("failure: %+v", state)
	}
}

// A command that does not even parse failed: it did not do what it was asked.
func TestABackgroundCommandThatDoesNotParseFails(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	if state := waitForJob(t, inst, startJob(t, inst, `echo "unclosed`)); !state.Failed {
		t.Fatalf("state = %+v", state)
	}
}

// Backslashes, quotes and letters beyond ASCII reach the command as written.
func TestABackgroundCommandKeepsItsBackslashes(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	waitForJob(t, inst, startJob(t, inst, `printf '%s' 'a\b\'"'"'c árvíztűrő' > out`))
	data, _ := os.ReadFile(filepath.Join(inst.Path, "out"))
	if string(data) != `a\b\'c árvíztűrő` {
		t.Fatalf("out = %q", data)
	}
}

// tmux hands the line to the user's default-shell, fish for some. Inside fish's
// single quotes \\ and \' are escapes, so the line must hold neither, nor a quote.
func TestTheBackgroundLineReadsTheSameInEveryShell(t *testing.T) {
	line := backgroundScript(`/tmp/x'y\z`, `echo 'a\b' "c\\d" \' done`)
	body, ok := strings.CutPrefix(line, "exec sh -c '")
	body, ok2 := strings.CutSuffix(body, "'")
	if !ok || !ok2 || strings.ContainsAny(body, "'") || strings.Contains(body, `\\`) || !utf8.ValidString(line) {
		t.Fatalf("line = %s", line)
	}
}

// tmux starts a pane in the home folder when the folder is gone. Run there, a
// cleanup command would clean the wrong place, unseen.
func TestABackgroundCommandInAMissingFolderFails(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	if err := os.RemoveAll(inst.Path); err != nil {
		t.Fatal(err)
	}
	state := waitForJob(t, inst, startJob(t, inst, "touch ran-here"))
	if !state.Failed {
		t.Fatalf("state = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "ran-here")); err == nil {
		t.Fatal("ran in the home folder")
	}
}

// Nobody can answer a question asked by a command with no tab: it gets no input
// rather than waiting for ever.
func TestABackgroundCommandGetsNoInput(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	if state := waitForJob(t, inst, startJob(t, inst, "read answer")); !state.Failed {
		t.Fatalf("state = %+v", state)
	}
}

// It runs the way it would in a tab: in the user's own shell, with its rc files.
func TestABackgroundCommandRunsInTheUsersShell(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := tmux("set-environment", "-g", "SHELL", bash); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".bashrc"), []byte("export FROM_RC=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".bash_profile"), []byte(". ~/.bashrc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, inst, startJob(t, inst, `[[ $FROM_RC == yes ]] && echo bash > out`))
	data, _ := os.ReadFile(filepath.Join(inst.Path, "out"))
	if string(data) != "bash\n" {
		t.Fatalf("out = %q", data)
	}
}

// Output that happens to name how failures were once marked is just output.
func TestOutputLikeAMarkerIsNoFailure(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	if state := waitForJob(t, inst, startJob(t, inst, "echo __ASMGR_BACKGROUND_FAILED__ 1; echo asmgr-bg-exit:1")); state.Failed {
		t.Fatalf("state = %+v", state)
	}
}

// A server that cannot be asked is no answer: the job is neither done nor failed.
func TestAnUnreachableServerLeavesTheJobOpen(t *testing.T) {
	inst := &Instance{ID: "bgcmd"}
	state := inst.BackgroundJobState(context.Background(), BackgroundJob{Name: "asm_bg_x", Server: "nowhere"})
	if state.Finished || !state.Unknown {
		t.Fatalf("state = %+v", state)
	}
}

// Jobs started before the app last quit are found again, with their names.
func TestLeftoverBackgroundJobsAreFound(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := backgroundInstance(t, tmux)
	job, err := inst.StartBackgroundCommand(context.Background(), 0, "Build", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	found := FindBackgroundJobs(context.Background(), LocalExecutor)
	if len(found) != 1 || found[0].Name != job.Name || found[0].Label != "Build" {
		t.Fatalf("found = %+v", found)
	}
}

// On a Windows machine psmux runs no POSIX shell: there it is a tab again.
func TestBackgroundNeedsATabOnlyOnLocalWindows(t *testing.T) {
	if !BackgroundNeedsATab("windows", "") || BackgroundNeedsATab("windows", "srv") || BackgroundNeedsATab("linux", "") {
		t.Fatal("wrong")
	}
}

// A pane kept after it ended carries a line from tmux, not from the command.
func TestTheLinesOfAKeptPaneAreTheCommands(t *testing.T) {
	got := lastPrintedLines("one\ntwo\n\nPane is dead (status 4, Tue Nov 11 10:00:00 2026)\n\n")
	if got != "one\ntwo" {
		t.Fatalf("got %q", got)
	}
}

// A saved command run in a new tab gets a terminal tab of its
// own. Against a real multiplexer of the test's own (isolatedTmux), since what
// matters is which window tmux makes current and whether a shell runs what is
// typed into it before it has finished starting.

func TestABackgroundTabLeavesTheCurrentWindowAlone(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := startedTerminalSession(t, "bg-current")
	before := paneFormat(t, tmux, inst.TmuxSessionName(), "#{window_index}")

	index, err := inst.NewBackgroundWindowOn("", "build", "")
	if err != nil {
		t.Fatalf("background tab: %v", err)
	}
	if got := paneFormat(t, tmux, inst.TmuxSessionName(), "#{window_index}"); got != before {
		t.Errorf("current window = %s, want it left at %s", got, before)
	}
	if got := paneFormat(t, tmux, inst.TmuxSessionName()+":"+itoa(index), "#{window_name}"); got != "build" {
		t.Errorf("new window is named %q, want %q", got, "build")
	}
	if tab := tabNamed(t, inst, "build"); tab.Index != index || tab.Agent != AgentTerminal {
		t.Errorf("recorded tab = %+v, want a terminal at %d", tab, index)
	}
}

func TestACommandTypedIntoAFreshBackgroundTabRuns(t *testing.T) {
	isolatedTmux(t)
	inst := startedTerminalSession(t, "bg-runs")
	marker := filepath.Join(t.TempDir(), "ran")

	index, err := inst.NewBackgroundWindowOn("", "build", "")
	if err != nil {
		t.Fatalf("background tab: %v", err)
	}
	// Straight after opening, as RunCommand does: the shell has not finished
	// its startup files yet, and what arrives before that can be flushed.
	// The test runs the user's own shell configuration, which is the point.
	if err := inst.TypeIntoFreshShell(index, "echo ok > '"+marker+"'", 10*time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForFile(t, marker)
}

// A tab on a server is typed into on the server. The session's own
// multiplexer has no window of that number, so the text used to go nowhere.
func TestTextForAServerTabIsTypedOnTheServer(t *testing.T) {
	isolatedTmux(t)
	server := fakeServerTmux(t)
	inst := startedTerminalSession(t, "bg-server")
	SetTabExecutor(inst.ID, "srv1", server)
	t.Cleanup(func() { ClearTabExecutor(inst.ID, "srv1") })
	marker := filepath.Join(t.TempDir(), "ran")

	index, err := inst.NewBackgroundWindowOn("srv1", "remote", "/")
	if err != nil {
		t.Fatalf("server tab: %v", err)
	}
	if err := inst.TypeIntoFreshShell(index, "echo ok > '"+marker+"'", 10*time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForFile(t, marker)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the command never ran: %s was not created", path)
}
