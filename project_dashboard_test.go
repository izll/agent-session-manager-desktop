package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestInspectGitRepositoryIncludesUntrackedAndCommitMetadata(t *testing.T) {
	repo := t.TempDir()
	dashboardGit(t, repo, "init")
	dashboardGit(t, repo, "config", "user.name", "Dashboard Tester")
	dashboardGit(t, repo, "config", "user.email", "dashboard@example.invalid")
	dashboardWriteFile(t, repo, "tracked.txt", "initial\n")
	dashboardGit(t, repo, "add", "tracked.txt")
	dashboardGit(t, repo, "commit", "-m", "initial dashboard commit")

	dashboardWriteFile(t, repo, "tracked.txt", "changed\n")
	dashboardWriteFile(t, repo, "untracked.txt", "new\n")
	if err := os.Mkdir(filepath.Join(repo, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	dashboardWriteFile(t, repo, filepath.Join("nested", "also-untracked.txt"), "nested new\n")

	summary := inspectGitRepository(context.Background(), repo)
	if !summary.Repository {
		t.Fatalf("expected repository, got %#v", summary)
	}
	if !summary.Dirty || summary.ModifiedFiles != 3 {
		t.Fatalf("expected three modified files including nested untracked files, got %#v", summary)
	}
	if summary.Branch == "" {
		t.Fatal("branch is empty")
	}
	if summary.RepositoryRoot != repo {
		t.Fatalf("unexpected repository root: %q, want %q", summary.RepositoryRoot, repo)
	}
	if summary.LastCommitHash == "" || summary.LastCommitMessage != "initial dashboard commit" {
		t.Fatalf("unexpected commit metadata: %#v", summary)
	}
	if summary.LastCommitAuthor != "Dashboard Tester" || summary.LastCommitAt == "" {
		t.Fatalf("missing commit author/time: %#v", summary)
	}
	if _, err := time.Parse(time.RFC3339, summary.LastCommitAt); err != nil {
		t.Fatalf("last commit time is not RFC3339: %q: %v", summary.LastCommitAt, err)
	}
}

func TestInspectGitRepositoryNonRepo(t *testing.T) {
	summary := inspectGitRepository(context.Background(), t.TempDir())
	if summary.Repository || summary.Dirty || summary.ModifiedFiles != 0 {
		t.Fatalf("non-repository reported as repository: %#v", summary)
	}
}

func TestInspectGitRepositoryInvalidPathReportsError(t *testing.T) {
	summary := inspectGitRepository(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if summary.Repository || summary.Error == "" {
		t.Fatalf("invalid path should be isolated as an error: %#v", summary)
	}
}

func TestInspectGitRepositoryAheadAndBehind(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	local := filepath.Join(root, "local")
	other := filepath.Join(root, "other")
	dashboardGit(t, root, "init", "--bare", origin)
	dashboardGit(t, root, "init", local)
	dashboardGit(t, local, "config", "user.name", "Dashboard Tester")
	dashboardGit(t, local, "config", "user.email", "dashboard@example.invalid")
	dashboardWriteFile(t, local, "base.txt", "base\n")
	dashboardGit(t, local, "add", "base.txt")
	dashboardGit(t, local, "commit", "-m", "base")
	dashboardGit(t, local, "remote", "add", "origin", origin)
	dashboardGit(t, local, "push", "-u", "origin", "HEAD")

	dashboardGit(t, root, "clone", origin, other)
	dashboardGit(t, other, "config", "user.name", "Other Tester")
	dashboardGit(t, other, "config", "user.email", "other@example.invalid")
	dashboardWriteFile(t, local, "local.txt", "ahead\n")
	dashboardGit(t, local, "add", "local.txt")
	dashboardGit(t, local, "commit", "-m", "local ahead")
	dashboardWriteFile(t, other, "remote.txt", "behind\n")
	dashboardGit(t, other, "add", "remote.txt")
	dashboardGit(t, other, "commit", "-m", "remote ahead")
	dashboardGit(t, other, "push")
	dashboardGit(t, local, "fetch", "origin")

	summary := inspectGitRepository(context.Background(), local)
	if summary.Upstream == "" || summary.Ahead != 1 || summary.Behind != 1 {
		t.Fatalf("unexpected upstream divergence: %#v", summary)
	}
}

func TestCollectProjectGitSummariesDeduplicatesPathsAndRunsConcurrently(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "shared-alias")
	if err := os.Symlink(shared, alias); err != nil {
		t.Fatal(err)
	}
	instances := []*session.Instance{
		{ID: "shared-1", Path: shared},
		{ID: "shared-2", Path: alias},
	}
	for idx := 0; idx < 7; idx++ {
		path := filepath.Join(root, "repo-"+string(rune('a'+idx)))
		instances = append(instances, &session.Instance{ID: "session-" + string(rune('a'+idx)), Path: path})
	}

	var calls, active, maxActive atomic.Int32
	inspector := func(ctx context.Context, path string) ProjectGitSummary {
		calls.Add(1)
		current := active.Add(1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		defer active.Add(-1)
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
		}
		return ProjectGitSummary{Path: path, Repository: true}
	}

	summaries := collectProjectGitSummariesWithInspector(context.Background(), instances, inspector)
	if len(summaries) != len(instances) {
		t.Fatalf("got %d summaries for %d sessions", len(summaries), len(instances))
	}
	if calls.Load() != 8 {
		t.Fatalf("expected one inspection per normalized path (8), got %d", calls.Load())
	}
	if maxActive.Load() < 2 || maxActive.Load() > projectGitWorkers {
		t.Fatalf("unexpected worker concurrency: %d", maxActive.Load())
	}
	for idx, summary := range summaries {
		if summary.SessionID != instances[idx].ID || summary.Path != instances[idx].Path {
			t.Fatalf("input order/session identity was not preserved at %d: %#v", idx, summary)
		}
	}
}

func TestCollectProjectGitSummariesHonorsParentTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	instances := []*session.Instance{{ID: "slow", Path: t.TempDir()}}
	inspector := func(ctx context.Context, path string) ProjectGitSummary {
		<-ctx.Done()
		return ProjectGitSummary{Path: path, Error: ctx.Err().Error()}
	}

	started := time.Now()
	summaries := collectProjectGitSummariesWithInspector(ctx, instances, inspector)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("collection ignored parent timeout: %s", elapsed)
	}
	if len(summaries) != 1 || !strings.Contains(summaries[0].Error, "deadline") {
		t.Fatalf("timeout was not isolated in summary: %#v", summaries)
	}
}

func dashboardWriteFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dashboardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
