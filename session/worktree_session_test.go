package session

import (
	"os"
	"path/filepath"
	"testing"
)

// A session given a worktree works there, not in the project directory. That
// is the whole point: two sessions on the same project touch different files.
func TestASessionWithAWorktreeWorksInIt(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "s-wt", Name: "the work", Path: repo}

	plan, err := inst.CreateWorktree(PlanWorktree(inst.RepoRootOf(repo), inst.Name))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	inst.Path = plan.Dir
	inst.WorktreeDir = plan.Dir
	inst.WorktreeBranch = plan.Branch
	inst.WorktreeRepoRoot = plan.RepoRoot

	if inst.Path == repo {
		t.Error("the session still works in the project directory")
	}
	// The diff reads the session's path, so it must follow the worktree —
	// otherwise the session shows the project's changes, not its own.
	if inst.gitDir() != plan.Dir {
		t.Errorf("gitDir() = %q, want the worktree %q", inst.gitDir(), plan.Dir)
	}

	// Work done in one is invisible in the other.
	if err := os.WriteFile(filepath.Join(plan.Dir, "only-here.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "only-here.txt")); !os.IsNotExist(err) {
		t.Error("a file written in the worktree appeared in the project directory")
	}
}

// Two sessions on one project get separate checkouts, which is what makes
// running two agents on it at the same time safe.
func TestTwoSessionsOnOneProjectDoNotShareFiles(t *testing.T) {
	repo := newTestRepo(t)
	first := &Instance{ID: "s1", Name: "first"}
	second := &Instance{ID: "s2", Name: "second"}

	a, err := first.CreateWorktree(PlanWorktree(repo, first.Name))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := second.CreateWorktree(PlanWorktree(repo, second.Name))
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if a.Dir == b.Dir {
		t.Fatal("both sessions were given the same directory")
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "mine.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "mine.txt")); !os.IsNotExist(err) {
		t.Error("one session's file appeared in the other's checkout")
	}
}
