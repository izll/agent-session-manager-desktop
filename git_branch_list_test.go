package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// gitBranchListGit runs git with extra environment entries, which the shared
// dashboardGit helper cannot do — the ordering tests need fixed commit dates.
func gitBranchListGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

// gitBranchListRepo builds a repo with an initial commit on main.
func gitBranchListRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	dashboardGit(t, repo, "init", "--initial-branch=main")
	dashboardGit(t, repo, "config", "user.name", "Branch Tester")
	dashboardGit(t, repo, "config", "user.email", "branch@example.invalid")
	dashboardWriteFile(t, repo, "tracked.txt", "initial\n")
	dashboardGit(t, repo, "add", "tracked.txt")
	// A fixed, oldest date keeps main's position in the recency order stable.
	gitBranchListGit(t, repo, []string{
		"GIT_AUTHOR_DATE=2023-01-01 12:00:00 +0000",
		"GIT_COMMITTER_DATE=2023-01-01 12:00:00 +0000",
	}, "commit", "-m", "initial branch commit")
	return repo
}

func TestReadGitBranchListOrdersByRecencyAndMarksCurrent(t *testing.T) {
	repo := gitBranchListRepo(t)

	// Each branch gets its own commit with an explicit, increasing committer
	// date so the -committerdate ordering is deterministic rather than
	// dependent on how fast the test machine runs.
	for i, name := range []string{"older", "newer"} {
		dashboardGit(t, repo, "checkout", "-b", name, "main")
		dashboardWriteFile(t, repo, name+".txt", "content\n")
		dashboardGit(t, repo, "add", name+".txt")
		date := fmt.Sprintf("2024-01-0%d 12:00:00 +0000", i+1)
		gitBranchListGit(t, repo, []string{
			"GIT_AUTHOR_DATE=" + date,
			"GIT_COMMITTER_DATE=" + date,
		}, "commit", "-m", "commit on "+name)
	}
	dashboardGit(t, repo, "checkout", "older")

	list := readGitBranchList(context.Background(), repo, gitBranchListLimit)
	if !list.Repository {
		t.Fatalf("expected a repository, got %#v", list)
	}
	if list.Total != 3 || len(list.Branches) != 3 {
		t.Fatalf("expected 3 branches, got total=%d len=%d", list.Total, len(list.Branches))
	}
	if list.Truncated {
		t.Fatalf("expected no truncation for 3 branches, got %#v", list)
	}

	if got := list.Branches[0].Name; got != "newer" {
		t.Fatalf("expected the most recent branch first, got %q", got)
	}
	if got := list.Branches[1].Name; got != "older" {
		t.Fatalf("expected older second, got %q", got)
	}
	if got := list.Branches[2].Name; got != "main" {
		t.Fatalf("expected main last, got %q", got)
	}

	for _, branch := range list.Branches {
		if branch.Current != (branch.Name == "older") {
			t.Fatalf("expected only 'older' to be current, got %#v", branch)
		}
		if branch.Hash == "" {
			t.Fatalf("expected a commit hash for %q, got %#v", branch.Name, branch)
		}
		if branch.Committed == "" {
			t.Fatalf("expected a committer date for %q, got %#v", branch.Name, branch)
		}
	}
}

func TestReadGitBranchListTruncates(t *testing.T) {
	repo := gitBranchListRepo(t)
	for i := range 4 {
		dashboardGit(t, repo, "branch", fmt.Sprintf("extra-%d", i))
	}

	const limit = 2
	list := readGitBranchList(context.Background(), repo, limit)
	if len(list.Branches) != limit {
		t.Fatalf("expected %d entries, got %d", limit, len(list.Branches))
	}
	if !list.Truncated {
		t.Fatalf("expected the list to be truncated, got %#v", list)
	}
	// Total must count every branch, not just the returned ones, or the UI
	// cannot say how many were hidden.
	if list.Total != 5 {
		t.Fatalf("expected total 5 (main + 4 extras), got %d", list.Total)
	}
}

func TestReadGitBranchListDetachedHeadHasNoCurrent(t *testing.T) {
	repo := gitBranchListRepo(t)
	dashboardGit(t, repo, "checkout", "--detach", "HEAD")

	list := readGitBranchList(context.Background(), repo, gitBranchListLimit)
	if len(list.Branches) != 1 {
		t.Fatalf("expected 1 branch, got %#v", list.Branches)
	}
	if list.Branches[0].Current {
		t.Fatalf("expected no current branch while detached, got %#v", list.Branches[0])
	}
}

func TestReadGitBranchListNonRepository(t *testing.T) {
	list := readGitBranchList(context.Background(), t.TempDir(), gitBranchListLimit)
	if list.Repository {
		t.Fatalf("expected a non-repository, got %#v", list)
	}
	if len(list.Branches) != 0 || list.Total != 0 || list.Truncated {
		t.Fatalf("expected an empty listing, got %#v", list)
	}
}

// A repo without any commit has no refs/heads at all; the menu must still open
// cleanly instead of erroring.
func TestReadGitBranchListEmptyRepository(t *testing.T) {
	repo := t.TempDir()
	dashboardGit(t, repo, "init", "--initial-branch=main")

	list := readGitBranchList(context.Background(), repo, gitBranchListLimit)
	if !list.Repository {
		t.Fatalf("expected a repository, got %#v", list)
	}
	if len(list.Branches) != 0 || list.Total != 0 {
		t.Fatalf("expected no branches before the first commit, got %#v", list)
	}
}

func TestListGitBranchesCachesByPath(t *testing.T) {
	repo := gitBranchListRepo(t)

	app := &App{}
	first := app.ListGitBranches(repo)
	if len(first.Branches) != 1 || first.Branches[0].Name != "main" {
		t.Fatalf("expected the single branch main, got %#v", first.Branches)
	}

	// The cached answer must survive a branch added within the TTL.
	dashboardGit(t, repo, "branch", "added-later")
	if cached := app.ListGitBranches(repo); len(cached.Branches) != 1 {
		t.Fatalf("expected the cached single-branch listing, got %#v", cached.Branches)
	}

	gitBranchListMu.Lock()
	for key, entry := range gitBranchListCache {
		entry.expiresAt = time.Now().Add(-time.Second)
		gitBranchListCache[key] = entry
	}
	gitBranchListMu.Unlock()

	if refreshed := app.ListGitBranches(repo); len(refreshed.Branches) != 2 {
		t.Fatalf("expected the refreshed listing to have 2 branches, got %#v", refreshed.Branches)
	}
}

func TestListGitBranchesEmptyPath(t *testing.T) {
	app := &App{}
	list := app.ListGitBranches("")
	if list.Repository {
		t.Fatalf("expected an empty path to be a non-repository, got %#v", list)
	}
	// The frontend iterates this without a nil guard.
	if list.Branches == nil {
		t.Fatalf("expected an empty slice rather than nil, got %#v", list)
	}
}
