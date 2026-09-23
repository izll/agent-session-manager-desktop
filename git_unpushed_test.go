package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unpushedRepo builds a clone of a bare "server" repository with one pushed
// commit, and returns a git runner for the clone.
func unpushedRepo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()
	server := t.TempDir()
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	gitIn(server, "init", "-q", "--bare")
	if out, err := exec.Command("git", "clone", "-q", server, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}
	run := func(args ...string) string { t.Helper(); return gitIn(clone, args...) }
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	commit(t, run, clone, "pushed")
	run("push", "-q", "origin", "HEAD")
	run("branch", "-q", "--set-upstream-to=origin/"+strings.TrimSpace(run("rev-parse", "--abbrev-ref", "HEAD")))
	return clone, run
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

// Commits made since the last push are counted, and a pushed one is not.
func TestUnpushedCommitsAreCounted(t *testing.T) {
	repo, run := unpushedRepo(t)
	commit(t, run, repo, "local-1")
	commit(t, run, repo, "local-2")

	info := readGitBranch(context.Background(), repo)
	if !info.HasRemote || info.Unpushed != 2 {
		t.Fatalf("unpushed = %d (hasRemote %v), want 2", info.Unpushed, info.HasRemote)
	}

	page, err := getGitHistoryAtPath(repo, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Unpushed != 2 {
		t.Errorf("history says %d unpushed, want 2", page.Unpushed)
	}
	marked := map[string]bool{}
	for _, c := range page.Commits {
		marked[c.Subject] = c.Unpushed
	}
	if !marked["local-1"] || !marked["local-2"] || marked["pushed"] {
		t.Errorf("marks per commit = %v; want the two local ones only", marked)
	}

	run("push", "-q")
	if info := readGitBranch(context.Background(), repo); info.Unpushed != 0 {
		t.Errorf("after pushing, %d still reported unpushed", info.Unpushed)
	}
}

// A branch that was never pushed has no upstream, so "ahead of upstream" says
// nothing — on exactly the branch whose every commit is only on this machine.
func TestABranchNeverPushedStillCountsItsCommits(t *testing.T) {
	repo, run := unpushedRepo(t)
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")

	info := readGitBranch(context.Background(), repo)
	if info.Upstream != "" {
		t.Fatalf("the new branch unexpectedly has upstream %q", info.Upstream)
	}
	if info.Unpushed != 1 {
		t.Errorf("a never-pushed branch reports %d unpushed, want 1", info.Unpushed)
	}
}

// Without a remote there is nothing to push to, and flagging every commit
// would be noise.
func TestARepositoryWithoutARemoteFlagsNothing(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	commit(t, run, repo, "only-here")

	if info := readGitBranch(context.Background(), repo); info.HasRemote || info.Unpushed != 0 {
		t.Errorf("a repository with no remote reports hasRemote=%v unpushed=%d",
			info.HasRemote, info.Unpushed)
	}
	page, err := getGitHistoryAtPath(repo, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range page.Commits {
		if c.Unpushed {
			t.Errorf("commit %q marked unpushed in a repository with no remote", c.Subject)
		}
	}
}
