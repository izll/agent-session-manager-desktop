package session

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// shellRecorder is a scriptedExecutor that can also run ordinary commands.
type shellRecorder struct {
	scriptedExecutor

	shellMu   sync.Mutex
	shellRuns [][]string
	shellDirs []string
	stdout    string
	stderr    string
	exitCode  int
}

func (s *shellRecorder) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	s.shellRuns = append(s.shellRuns, args)
	s.shellDirs = append(s.shellDirs, dir)
	return []byte(s.stdout), []byte(s.stderr), s.exitCode, nil
}

func (s *shellRecorder) lastRun() []string {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	if len(s.shellRuns) == 0 {
		return nil
	}
	return s.shellRuns[len(s.shellRuns)-1]
}

// A local session must not change behaviour at all: it reads its own disk,
// through the same code as before.
func TestLocalSessionsStillReadLocally(t *testing.T) {
	inst := &Instance{ID: "local-1"}
	if shell, err := inst.shellExec(); shell != nil || err != nil {
		t.Errorf("a local session was given a shell executor (%v) or an error (%v)", shell, err)
	}

	entries, err := inst.listDirectoryWhereSessionLives("/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Error("a local session returned a remote listing; the caller would " +
			"skip its own local path")
	}
}

// A remote session's files are on the server, and reading them must go there.
func TestRemoteReadsGoToTheServer(t *testing.T) {
	recorder := &shellRecorder{stdout: "file contents"}
	recorder.scriptedExecutor = *newScriptedExecutor()

	inst := &Instance{ID: "remote-1", ServerID: "srv1"}
	SetExecutor(inst.ID, recorder)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	data, err := inst.readFileWhereSessionLives("/srv/project/main.go", 4096)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "file contents" {
		t.Errorf("read back %q", data)
	}

	// A limit is honoured on the far side rather than by trimming afterwards:
	// pulling a gigabyte across the network to show the first few kilobytes is
	// the failure this avoids.
	run := recorder.lastRun()
	if len(run) == 0 || run[0] != "head" {
		t.Fatalf("the read did not limit what it fetched: %v", run)
	}
	if !containsArgument(run, "4096") {
		t.Errorf("the size limit was not passed to the server: %v", run)
	}
	if !containsArgument(run, "/srv/project/main.go") {
		t.Errorf("the wrong file was read: %v", run)
	}
}

// The listing is parsed from a fixed format rather than from ls, whose columns
// differ between systems and locales.
func TestRemoteListingIsParsed(t *testing.T) {
	recorder := &shellRecorder{stdout: "d\t4096\t.config\nf\t1024\tnotes.txt\nf\t0\tempty\n"}
	recorder.scriptedExecutor = *newScriptedExecutor()

	inst := &Instance{ID: "remote-2", ServerID: "srv1"}
	SetExecutor(inst.ID, recorder)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	entries, err := inst.listDirectoryWhereSessionLives("/srv/project")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}
	if !entries[0].IsDir || entries[0].Name != ".config" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Size != 1024 || entries[1].IsDir {
		t.Errorf("second entry = %+v", entries[1])
	}
}

// A path with a space or a quote in it is ordinary, and must not be able to
// become part of the command.
func TestRemotePathsAreQuoted(t *testing.T) {
	recorder := &shellRecorder{}
	recorder.scriptedExecutor = *newScriptedExecutor()

	inst := &Instance{ID: "remote-3", ServerID: "srv1"}
	SetExecutor(inst.ID, recorder)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	_, _ = inst.listDirectoryWhereSessionLives(`/srv/my project`)

	run := recorder.lastRun()
	script := strings.Join(run, " ")
	if !strings.Contains(script, `'/srv/my project'`) {
		t.Errorf("a path with a space was not quoted: %s", script)
	}
}

// git runs where the repository is.
func TestRemoteGitRunsOnTheServer(t *testing.T) {
	recorder := &shellRecorder{stdout: "1\t2\tmain.go\n"}
	recorder.scriptedExecutor = *newScriptedExecutor()

	inst := &Instance{ID: "remote-4", ServerID: "srv1"}
	SetExecutor(inst.ID, recorder)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	out, err := inst.gitOutput([]string{"-C", "/srv/project", "diff", "--numstat"}, nil)
	if err != nil {
		t.Fatalf("git: %v", err)
	}
	if string(out) != "1\t2\tmain.go\n" {
		t.Errorf("output = %q", out)
	}

	run := recorder.lastRun()
	if len(run) == 0 || run[0] != "git" {
		t.Fatalf("git was not the command: %v", run)
	}
}

// A non-zero exit from git is not always a failure — but the ones that are
// have to carry git's own message, or the user sees nothing useful.
func TestRemoteGitFailuresCarryTheirMessage(t *testing.T) {
	recorder := &shellRecorder{
		exitCode: 128,
		stderr:   "fatal: not a git repository",
	}
	recorder.scriptedExecutor = *newScriptedExecutor()

	inst := &Instance{ID: "remote-5", ServerID: "srv1"}
	SetExecutor(inst.ID, recorder)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	_, err := inst.gitOutput([]string{"-C", "/tmp", "diff"}, nil)
	if err == nil {
		t.Fatal("a failing git command reported success")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("the error does not carry git's message: %v", err)
	}
}
