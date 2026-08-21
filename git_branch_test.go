package main

import (
	"context"
	"testing"
	"time"
)

func TestReadGitBranchReportsBranchName(t *testing.T) {
	repo := t.TempDir()
	dashboardGit(t, repo, "init", "--initial-branch=main")
	dashboardGit(t, repo, "config", "user.name", "Branch Tester")
	dashboardGit(t, repo, "config", "user.email", "branch@example.invalid")
	dashboardWriteFile(t, repo, "tracked.txt", "initial\n")
	dashboardGit(t, repo, "add", "tracked.txt")
	dashboardGit(t, repo, "commit", "-m", "initial branch commit")

	info := readGitBranch(context.Background(), repo)
	if !info.Repository {
		t.Fatalf("expected a repository, got %#v", info)
	}
	if info.Branch != "main" {
		t.Fatalf("expected branch main, got %q", info.Branch)
	}
	if info.Upstream != "" || info.Ahead != 0 || info.Behind != 0 {
		t.Fatalf("expected no upstream tracking, got %#v", info)
	}
}

func TestReadGitBranchDetachedHead(t *testing.T) {
	repo := t.TempDir()
	dashboardGit(t, repo, "init", "--initial-branch=main")
	dashboardGit(t, repo, "config", "user.name", "Branch Tester")
	dashboardGit(t, repo, "config", "user.email", "branch@example.invalid")
	dashboardWriteFile(t, repo, "tracked.txt", "initial\n")
	dashboardGit(t, repo, "add", "tracked.txt")
	dashboardGit(t, repo, "commit", "-m", "initial branch commit")
	dashboardGit(t, repo, "checkout", "--detach", "HEAD")

	info := readGitBranch(context.Background(), repo)
	if !info.Repository {
		t.Fatalf("expected a repository, got %#v", info)
	}
	if got := info.Branch; len(got) < len("detached@")+1 || got[:len("detached@")] != "detached@" {
		t.Fatalf("expected a detached@<hash> branch, got %q", got)
	}
}

func TestReadGitBranchNonRepository(t *testing.T) {
	info := readGitBranch(context.Background(), t.TempDir())
	if info.Repository {
		t.Fatalf("expected a non-repository, got %#v", info)
	}
	if info.Branch != "" || info.Upstream != "" {
		t.Fatalf("expected empty branch data, got %#v", info)
	}
}

func TestGetGitBranchCachesByPath(t *testing.T) {
	repo := t.TempDir()
	dashboardGit(t, repo, "init", "--initial-branch=main")
	dashboardGit(t, repo, "config", "user.name", "Branch Tester")
	dashboardGit(t, repo, "config", "user.email", "branch@example.invalid")
	dashboardWriteFile(t, repo, "tracked.txt", "initial\n")
	dashboardGit(t, repo, "add", "tracked.txt")
	dashboardGit(t, repo, "commit", "-m", "initial branch commit")

	app := &App{}
	first := app.getGitBranchAtPath(repo)
	if first.Branch != "main" {
		t.Fatalf("expected branch main, got %q", first.Branch)
	}

	// The cached answer must survive a rename that the cache hasn't expired for.
	dashboardGit(t, repo, "branch", "-m", "renamed")
	if cached := app.getGitBranchAtPath(repo); cached.Branch != "main" {
		t.Fatalf("expected the cached branch main, got %q", cached.Branch)
	}

	gitBranchMu.Lock()
	for key, entry := range gitBranchCache {
		entry.expiresAt = time.Now().Add(-time.Second)
		gitBranchCache[key] = entry
	}
	gitBranchMu.Unlock()

	if refreshed := app.getGitBranchAtPath(repo); refreshed.Branch != "renamed" {
		t.Fatalf("expected the refreshed branch renamed, got %q", refreshed.Branch)
	}
}

func TestGetGitBranchEmptyPath(t *testing.T) {
	app := &App{}
	if info := app.getGitBranchAtPath(""); info.Repository {
		t.Fatalf("expected an empty path to be a non-repository, got %#v", info)
	}
}
