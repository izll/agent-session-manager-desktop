package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetFullDiffIncludesUntrackedWithoutMutatingIndex(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "ASMGR test")

	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	if err := os.WriteFile(tracked, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := runGit(t, repo, "status", "--porcelain=v1")
	result := (&Instance{Path: repo}).GetFullDiff()
	if result.Error != nil {
		t.Fatalf("GetFullDiff failed: %v", result.Error)
	}
	if !strings.Contains(result.Content, "tracked.txt") || !strings.Contains(result.Content, "untracked.txt") {
		t.Fatalf("diff does not contain tracked and untracked files:\n%s", result.Content)
	}
	after := runGit(t, repo, "status", "--porcelain=v1")
	if after != before {
		t.Fatalf("GetFullDiff mutated repository state\nbefore: %q\nafter:  %q", before, after)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
