package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRunner runs git in a fixed directory and fails the test if git does.
func gitRunner(t *testing.T, dir string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
}

// unpushedServer is a bare "server" repository holding one commit.
func unpushedServer(t *testing.T) string {
	t.Helper()
	server := t.TempDir()
	gitRunner(t, server)("init", "-q", "--bare")
	seed := filepath.Join(t.TempDir(), "seed")
	if out, err := exec.Command("git", "clone", "-q", server, seed).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}
	run := gitRunner(t, seed)
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	commit(t, run, seed, "pushed")
	run("push", "-q", "origin", "HEAD")
	return server
}

// cloneOf clones source with the given extra clone options and returns a git
// runner for the clone, configured to commit.
func cloneOf(t *testing.T, source string, options ...string) (string, func(args ...string) string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	args := append(append([]string{"clone", "-q"}, options...), source, clone)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}
	run := gitRunner(t, clone)
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	return clone, run
}

// unpushedRepo builds an ordinary clone of a bare "server" repository with one
// pushed commit, its branch tracking the server's, and returns a git runner
// for the clone.
func unpushedRepo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()
	return cloneOf(t, unpushedServer(t))
}

func commit(t *testing.T, run func(...string) string, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", name+".txt")
	run("commit", "-qm", name)
	return strings.TrimSpace(run("rev-parse", "HEAD"))
}

func branchInfo(t *testing.T, repo string) GitBranchInfo {
	t.Helper()
	info, complete := readGitBranch(context.Background(), repo)
	if !complete {
		t.Fatalf("reading the branch of %s did not complete", repo)
	}
	return info
}

func historyMarks(t *testing.T, repo, branch string) (GitHistoryPage, map[string]bool) {
	t.Helper()
	page, err := getGitHistoryAtPath(repo, branch, 0)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]bool{}
	for _, c := range page.Commits {
		marked[c.Subject] = c.Unpushed
	}
	return page, marked
}

// Commits made since the last push are counted, and a pushed one is not.
func TestUnpushedCommitsAreCounted(t *testing.T) {
	repo, run := unpushedRepo(t)
	commit(t, run, repo, "local-1")
	commit(t, run, repo, "local-2")

	info := branchInfo(t, repo)
	if info.Upstream == "" {
		t.Fatal("the clone's branch has no upstream")
	}
	if !info.UnpushedKnown || info.Unpushed != 2 {
		t.Fatalf("unpushed = %d (known %v), want 2", info.Unpushed, info.UnpushedKnown)
	}

	page, marked := historyMarks(t, repo, "")
	if page.Unpushed != 2 {
		t.Errorf("history says %d unpushed, want 2", page.Unpushed)
	}
	if !marked["local-1"] || !marked["local-2"] || marked["pushed"] {
		t.Errorf("marks per commit = %v; want the two local ones only", marked)
	}

	run("push", "-q")
	if info := branchInfo(t, repo); info.Unpushed != 0 {
		t.Errorf("after pushing, %d still reported unpushed", info.Unpushed)
	}
}

// A branch that was never pushed has no upstream, so "ahead of upstream" says
// nothing — on exactly the branch whose every commit is only on this machine.
func TestABranchNeverPushedStillCountsItsCommits(t *testing.T) {
	repo, run := unpushedRepo(t)
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")

	info := branchInfo(t, repo)
	if info.Upstream != "" {
		t.Fatalf("the new branch unexpectedly has upstream %q", info.Upstream)
	}
	if !info.UnpushedKnown || info.Unpushed != 1 {
		t.Errorf("a never-pushed branch reports %d unpushed (known %v), want 1",
			info.Unpushed, info.UnpushedKnown)
	}
	if page, marked := historyMarks(t, repo, ""); page.Unpushed != 1 || !marked["feature-1"] {
		t.Errorf("history says %d unpushed with marks %v; want feature-1 alone", page.Unpushed, marked)
	}
}

// A shallow clone fetches one branch, so a branch pushed from it never gets a
// remote-tracking ref: counted against the remote-tracking refs, its commits
// would stay "not pushed" forever, however often the user fetched.
func TestAPushFromASingleBranchCloneIsNotFlagged(t *testing.T) {
	server := unpushedServer(t)
	repo, run := cloneOf(t, "file://"+server, "--depth", "1")
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")
	run("push", "-q", "-u", "origin", "feature")
	run("fetch", "-q")

	if refs := strings.TrimSpace(run("for-each-ref", "refs/remotes/origin/feature")); refs != "" {
		t.Fatalf("the clone unexpectedly tracks the pushed branch: %s", refs)
	}
	if info := branchInfo(t, repo); info.UnpushedKnown && info.Unpushed != 0 {
		t.Errorf("a pushed branch reports %d unpushed", info.Unpushed)
	}
	page, marked := historyMarks(t, repo, "")
	if page.Unpushed != 0 || marked["feature-1"] {
		t.Errorf("history says %d unpushed with marks %v; want none", page.Unpushed, marked)
	}
	if page, _ := historyMarks(t, repo, "feature"); page.Unpushed != 0 {
		t.Errorf("history of the named branch says %d unpushed; want none", page.Unpushed)
	}
}

// A single-branch clone cannot tell a branch never pushed from one pushed and
// not fetched, so it says nothing rather than risk the wrong answer.
func TestASingleBranchCloneLeavesANewBranchUnknown(t *testing.T) {
	server := unpushedServer(t)
	repo, run := cloneOf(t, server, "--single-branch")
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")

	if info := branchInfo(t, repo); info.UnpushedKnown {
		t.Errorf("a single-branch clone claims %d unpushed", info.Unpushed)
	}
}

// Without a remote there is nothing to push to, and flagging every commit
// would be noise.
func TestARepositoryWithoutARemoteFlagsNothing(t *testing.T) {
	repo := t.TempDir()
	run := gitRunner(t, repo)
	run("init", "-q")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	commit(t, run, repo, "only-here")

	if info := branchInfo(t, repo); info.UnpushedKnown || info.Unpushed != 0 {
		t.Errorf("a repository with no remote reports known=%v unpushed=%d",
			info.UnpushedKnown, info.Unpushed)
	}
	page, marked := historyMarks(t, repo, "")
	if page.Unpushed != 0 || marked["only-here"] {
		t.Errorf("a repository with no remote: %d unpushed, marks %v", page.Unpushed, marked)
	}
}

// Git warns on stderr when a branch shares its name with a tag. Parsed along
// with the hashes, the warning's words were counted as commits.
func TestAWarningIsNotCountedAsACommit(t *testing.T) {
	repo, run := unpushedRepo(t)
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")
	run("tag", "feature")

	if page, _ := historyMarks(t, repo, "feature"); page.Unpushed != 1 {
		t.Errorf("history of an ambiguous branch says %d unpushed, want 1", page.Unpushed)
	}
}

// The marks are capped; the total the history states must not be, or it
// disagrees with the header badge.
func TestTheHistoryTotalIsNotCappedWithTheMarks(t *testing.T) {
	repo, run := unpushedRepo(t)
	commit(t, run, repo, "local-1")
	commit(t, run, repo, "local-2")
	commit(t, run, repo, "local-3")

	saved := maxUnpushedListed
	maxUnpushedListed = 2
	t.Cleanup(func() { maxUnpushedListed = saved })

	page, _ := historyMarks(t, repo, "")
	if page.Unpushed != 3 {
		t.Errorf("history says %d unpushed past the cap, want 3", page.Unpushed)
	}
	if info := branchInfo(t, repo); info.Unpushed != page.Unpushed {
		t.Errorf("badge says %d, history says %d", info.Unpushed, page.Unpushed)
	}
}

// The count walks history and may be slow; it must not use up the time the
// branch and upstream queries need, and a count that ran out of time should
// not make every following request pay for it again.
func TestASlowCountLeavesTheBranchAndIsCached(t *testing.T) {
	repo, _ := unpushedRepo(t)

	calls := 0
	savedCount, savedTimeout := countUnpushedFunc, gitUnpushedTimeout
	countUnpushedFunc = func(ctx context.Context, _, _ string) (int, bool) {
		calls++
		<-ctx.Done()
		return 0, false
	}
	gitUnpushedTimeout = 50 * time.Millisecond
	t.Cleanup(func() { countUnpushedFunc, gitUnpushedTimeout = savedCount, savedTimeout })

	app := &App{}
	info := app.getGitBranchAtPath(repo)
	if info.Branch == "" || info.Upstream == "" {
		t.Fatalf("a slow count took the branch down with it: %#v", info)
	}
	if info.UnpushedKnown {
		t.Errorf("a count that ran out of time is reported as known")
	}
	app.getGitBranchAtPath(repo)
	if calls != 1 {
		t.Errorf("the count ran %d times within one cache lifetime, want 1", calls)
	}
}
