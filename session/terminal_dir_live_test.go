package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Directories were saved only when a session or tab was stopped. A machine
// that shuts down or restarts stops nothing — the multiplexer simply dies — so
// every terminal came back where it had last been stopped, not where it was.
// They are now read while the session runs; these cover the reading.
func TestTerminalDirsAreReadWhileRunning(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustMkdir(t, sub)

	inst := &Instance{
		ID:     "live",
		Name:   "live",
		Path:   root,
		Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal, WorkDir: ""},
			{Index: 2, Agent: AgentTerminal, WorkDir: sub},
			{Index: 3, Agent: AgentTerminal},
			{Index: 4, Agent: AgentClaude},
			{Index: 5, Agent: AgentTerminal, Stopped: true},
			{Index: 10000, Agent: AgentTerminal, ServerID: "srv"},
		},
	}
	panes := map[string]string{
		inst.TmuxSessionName() + ":1":     sub + "\n", // cd'd into a subdirectory
		inst.TmuxSessionName() + ":2":     root,       // cd'd back to the root
		inst.TmuxSessionName() + ":3":     "",         // could not be read
		inst.TmuxSessionName() + ":4":     sub,        // an agent: not ours to move
		inst.TmuxSessionName() + ":5":     sub,        // parked
		inst.TmuxSessionName() + ":10000": sub,        // on a server
	}
	query := func(_ context.Context, target string) string { return panes[target] }

	got := inst.terminalDirsNow(context.Background(), query)

	if dir, ok := got[1]; !ok || dir != sub {
		t.Errorf("tab 1 moved into %s, read as %q (present %v)", sub, dir, ok)
	}
	// Back at the root means "no directory of its own". Treating that as
	// nothing to save left the tab stuck in the subdirectory it had left.
	if dir, ok := got[2]; !ok || dir != "" {
		t.Errorf("tab 2 went back to the session root, read as %q (present %v)", dir, ok)
	}
	for _, idx := range []int{3, 4, 5, 10000} {
		if _, ok := got[idx]; ok {
			t.Errorf("tab %d should be left alone, got %q", idx, got[idx])
		}
	}
}

// A session on a server has its panes in that server's multiplexer; the local
// one cannot say where they are.
func TestTerminalDirsSkipASessionOnAServer(t *testing.T) {
	inst := &Instance{ID: "remote", Path: t.TempDir(), Status: StatusRunning, ServerID: "srv",
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}}}
	query := func(context.Context, string) string { return t.TempDir() }
	if got := inst.terminalDirsNow(context.Background(), query); len(got) != 0 {
		t.Errorf("read a server session's panes from this computer: %v", got)
	}
}

// Saved onto what is on disk now, and only the directory: the poll read its
// instances a moment earlier, and writing them back whole would undo changes
// made in between.
func TestRecordingTerminalDirsTouchesNothingElse(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustMkdir(t, sub)
	current := &Instance{
		ID:   "session-1",
		Name: "renamed meanwhile",
		Path: root,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal, Name: "shell", WorkDir: sub},
			{Index: 2, Agent: AgentClaude, Name: "agent", WorkDir: sub},
		},
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	// Tab 1 went back to the root; tab 2 is an agent and must not move.
	if err := storage.RecordTerminalDirsForProject("", map[string]map[int]string{"session-1": {1: "", 2: root}}); err != nil {
		t.Fatal(err)
	}

	saved, err := storage.GetInstance("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "renamed meanwhile" {
		t.Errorf("the save overwrote the name with %q", saved.Name)
	}
	for _, fw := range saved.FollowedWindows {
		switch fw.Index {
		case 1:
			if fw.WorkDir != "" {
				t.Errorf("the terminal tab was not moved back to the root: %q", fw.WorkDir)
			}
		case 2:
			if fw.WorkDir != sub {
				t.Errorf("an agent tab's directory was changed to %q", fw.WorkDir)
			}
		}
	}
}

// The case the review found: the poll saved a subdirectory, the user went back
// to the session root, and the session was stopped before the next save. The
// stop capture treated "at the root" as nothing to save, so the tab came back
// in the subdirectory it had left. Both stop paths now save it, as the poll
// does.
func TestStoppingRemembersAReturnToTheRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustMkdir(t, sub)
	atRoot := func(context.Context, string) string { return root }

	whole := &Instance{ID: "stop-all", Name: "stop-all", Path: root, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal, WorkDir: sub}}}
	if !whole.captureTerminalWorkingDirs(atRoot) || whole.FollowedWindows[0].WorkDir != "" {
		t.Errorf("stopping the session kept %q for a tab back at the root",
			whole.FollowedWindows[0].WorkDir)
	}

	single := &Instance{ID: "stop-one", Name: "stop-one", Path: root, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal, WorkDir: sub}}}
	if !single.captureTerminalWorkingDir(1, atRoot) || single.FollowedWindows[0].WorkDir != "" {
		t.Errorf("stopping the tab kept %q for a tab back at the root",
			single.FollowedWindows[0].WorkDir)
	}
}

// What the app records about itself while running is not a user's edit and
// makes no backup. The activity time was saved on every tick while an agent
// worked, and each save added an automatic backup, crowding the real recovery
// history out.
func TestBookkeepingWritesMakeNoBackup(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustMkdir(t, sub)
	inst := &Instance{ID: "s", Name: "s", Path: root,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}}}
	if err := storage.SaveAll([]*Instance{inst}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	before, err := storage.ListBackups()
	if err != nil {
		t.Fatal(err)
	}

	for tick := 1; tick <= 3; tick++ {
		at := time.Now().Add(time.Duration(tick) * time.Second)
		if err := storage.RecordActivityForProject("", map[string]time.Time{"s": at}); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.RecordTerminalDirsForProject("", map[string]map[int]string{"s": {1: sub}}); err != nil {
		t.Fatal(err)
	}

	after, err := storage.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("bookkeeping writes added %d backups", len(after)-len(before))
	}
	// And they were saved all the same.
	saved, err := storage.GetInstance("s")
	if err != nil {
		t.Fatal(err)
	}
	if saved.FollowedWindows[0].WorkDir != sub || saved.LastActiveAt.IsZero() {
		t.Errorf("the bookkeeping was not saved: %+v", saved)
	}
}

// The stop-time captures asked the local multiplexer about a tab on a server;
// it answered for a local pane, and that path — or "" for the session root —
// overwrote the server tab's own directory.
func TestStoppingLeavesAServerTabsDirectoryAlone(t *testing.T) {
	root := t.TempDir()
	inst := &Instance{ID: "mixed", Name: "mixed", Path: root, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 10000, Agent: AgentTerminal, ServerID: "srv", WorkDir: "/srv/app"},
		}}
	atLocalRoot := func(context.Context, string) string { return root }

	inst.captureTerminalWorkingDirs(atLocalRoot)
	inst.captureTerminalWorkingDir(10000, atLocalRoot)
	if got := inst.FollowedWindows[0].WorkDir; got != "/srv/app" {
		t.Errorf("stopping overwrote the server tab's directory with %q", got)
	}
}

// The poll saves these while holding the project lock, so a pane that does
// not answer must not hold it.
func TestReadingTerminalDirsIsBounded(t *testing.T) {
	inst := &Instance{ID: "slow", Name: "slow", Path: t.TempDir(), Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 1, Agent: AgentTerminal}}}
	stuck := func(ctx context.Context, _ string) string {
		<-ctx.Done() // a multiplexer that never answers
		return "/somewhere"
	}
	started := time.Now()
	dirs := inst.terminalDirsNow(context.Background(), stuck)
	if elapsed := time.Since(started); elapsed > terminalDirCaptureTimeout+2*time.Second {
		t.Errorf("reading took %v with no answer", elapsed)
	}
	if len(dirs) != 0 {
		t.Errorf("a path that arrived after the deadline was used: %v", dirs)
	}
}

// For a window that no longer exists tmux answers for window 0, with status
// 0. The answer names its window, and one about another window is refused.
func TestAnAnswerAboutAnotherWindowIsRefused(t *testing.T) {
	if got := paneAnswerFor("sess:1", "1\t/home/u/proj/sub\n"); got != "/home/u/proj/sub" {
		t.Errorf("the right window's answer was refused: %q", got)
	}
	if got := paneAnswerFor("sess:7", "0\t/home/u/elsewhere\n"); got != "" {
		t.Errorf("an answer about window 0 was taken for window 7: %q", got)
	}
	if got := paneAnswerFor("sess:7", "/no/index/in/it\n"); got != "" {
		t.Errorf("an answer without its window was accepted: %q", got)
	}
}

// Back at the root is stored as "", and a restart has to put the shell there,
// not in the directory the pane was first created in.
func TestATerminalAtTheRootRestartsAtTheRoot(t *testing.T) {
	root := t.TempDir()
	inst := &Instance{ID: "r", Name: "r", Path: root,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Agent: AgentTerminal, WorkDir: ""},
			{Index: 10000, Agent: AgentTerminal, ServerID: "srv", WorkDir: "/srv/only/there"},
		}}
	if got := inst.terminalRestartDirArgs(inst.FollowedWindows[0]); len(got) != 2 || got[1] != root {
		t.Errorf("a tab at the root restarts with %v, want -c %s", got, root)
	}
	// A server path does not exist here and must not be checked here.
	if got := inst.terminalRestartDirArgs(inst.FollowedWindows[1]); len(got) != 2 || got[1] != "/srv/only/there" {
		t.Errorf("a server tab restarts with %v, want its own directory", got)
	}
}

// One listing answers every window. The active pane is the one display-message
// answered for, so it wins when a window is split.
func TestPaneListingPrefersTheActivePane(t *testing.T) {
	dirs := parsePaneDirs("0\t1\t/tmp\n1\t0\t/usr\n1\t1\t/etc\n2\t1\t/srv/a b\n")
	want := map[int]string{0: "/tmp", 1: "/etc", 2: "/srv/a b"}
	for index, dir := range want {
		if dirs[index] != dir {
			t.Errorf("window %d read as %q, want %q", index, dirs[index], dir)
		}
	}
}

// Against a real multiplexer: one listing, and a window that does not exist
// answers nothing rather than another window's directory.
func TestSessionPaneDirsAgainstTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs tmux")
	}
	name := fmt.Sprintf("asmgr_panedirs_%d", os.Getpid())
	first, second := t.TempDir(), t.TempDir()
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", first).CombinedOutput(); err != nil {
		t.Skipf("cannot start tmux: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	if out, err := exec.Command("tmux", "new-window", "-d", "-t", name+":3", "-c", second).CombinedOutput(); err != nil {
		t.Fatalf("new-window: %v %s", err, out)
	}

	query := sessionPaneDirs()
	ctx := context.Background()
	if got := query(ctx, name+":3"); !samePath(got, second) {
		t.Errorf("window 3 read as %q, want %q", got, second)
	}
	if got := query(ctx, name+":0"); !samePath(got, first) {
		t.Errorf("window 0 read as %q, want %q", got, first)
	}
	if got := query(ctx, name+":7"); got != "" {
		t.Errorf("a window that does not exist answered %q", got)
	}
}
