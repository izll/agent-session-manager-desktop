package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newDiffRepo builds a repo with one committed file and returns its path.
func newDiffRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "ASMGR test")
	return repo
}

func writeFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, repo, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseDiffFilesSplitsPerFileAndHunk(t *testing.T) {
	repo := newDiffRepo(t)
	writeFile(t, repo, "a.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n")
	writeFile(t, repo, "b.txt", "keep\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	// Two separate edits, far enough apart to become distinct hunks.
	writeFile(t, repo, "a.txt", "CHANGED\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\nALSO\n")
	writeFile(t, repo, "new.txt", "brand new\n")

	inst := &Instance{Path: repo}
	files, err := inst.GetFullDiffFiles()
	if err != nil {
		t.Fatalf("GetFullDiffFiles: %v", err)
	}

	byPath := map[string]DiffFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	a, ok := byPath["a.txt"]
	if !ok {
		t.Fatalf("a.txt missing from %v", byPath)
	}
	if a.Status != "modified" {
		t.Errorf("a.txt status = %q, want modified", a.Status)
	}
	if len(a.Hunks) != 2 {
		t.Errorf("a.txt hunks = %d, want 2", len(a.Hunks))
	}
	if a.Added != 2 || a.Removed != 2 {
		t.Errorf("a.txt +%d -%d, want +2 -2", a.Added, a.Removed)
	}

	n, ok := byPath["new.txt"]
	if !ok {
		t.Fatal("new.txt missing (untracked files must appear)")
	}
	if n.Status != "added" {
		t.Errorf("new.txt status = %q, want added", n.Status)
	}

	if _, ok := byPath["b.txt"]; ok {
		t.Error("b.txt is unchanged and must not appear")
	}
}

func TestRevertHunkLeavesOtherHunksAlone(t *testing.T) {
	repo := newDiffRepo(t)
	original := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n"
	writeFile(t, repo, "a.txt", original)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, repo, "a.txt", "TOP\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\nBOTTOM\n")

	inst := &Instance{Path: repo}
	files, err := inst.GetFullDiffFiles()
	if err != nil {
		t.Fatalf("GetFullDiffFiles: %v", err)
	}
	if len(files) != 1 || len(files[0].Hunks) != 2 {
		t.Fatalf("expected one file with 2 hunks, got %+v", files)
	}

	// Revert only the first hunk: "TOP" goes back to "1", "BOTTOM" stays.
	patch, err := BuildHunkPatch(files[0], 0)
	if err != nil {
		t.Fatalf("BuildHunkPatch: %v", err)
	}
	if err := inst.RevertHunk(patch); err != nil {
		t.Fatalf("RevertHunk: %v", err)
	}

	got := readFile(t, repo, "a.txt")
	if strings.Contains(got, "TOP") {
		t.Error("first hunk was not reverted")
	}
	if !strings.Contains(got, "BOTTOM") {
		t.Error("second hunk was reverted too; only the requested one should change")
	}
	if !strings.HasPrefix(got, "1\n") {
		t.Errorf("original first line not restored:\n%s", got)
	}
}

func TestRevertFileRestoresTrackedAndDeletesUntracked(t *testing.T) {
	repo := newDiffRepo(t)
	writeFile(t, repo, "tracked.txt", "committed\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, repo, "tracked.txt", "modified\n")
	writeFile(t, repo, "untracked.txt", "temporary\n")

	inst := &Instance{Path: repo}

	if err := inst.RevertFile("tracked.txt", ""); err != nil {
		t.Fatalf("RevertFile(tracked): %v", err)
	}
	if got := readFile(t, repo, "tracked.txt"); got != "committed\n" {
		t.Errorf("tracked.txt = %q, want the committed content back", got)
	}

	if err := inst.RevertFile("untracked.txt", ""); err != nil {
		t.Fatalf("RevertFile(untracked): %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "untracked.txt")); !os.IsNotExist(err) {
		t.Error("untracked file should have been deleted; there is no version to restore")
	}
}

// The path arrives from the frontend, so it must not be able to reach outside
// the repository.
func TestRevertFileRejectsPathsOutsideRepo(t *testing.T) {
	repo := newDiffRepo(t)
	writeFile(t, repo, "f.txt", "x\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, filepath.Dir(outside), "secret.txt", "do not touch\n")

	inst := &Instance{Path: repo}
	for _, bad := range []string{"../secret.txt", "/etc/passwd", "", "../../x"} {
		if err := inst.RevertFile(bad, ""); err == nil {
			t.Errorf("RevertFile(%q) was allowed; it must be rejected", bad)
		}
	}

	if got, err := os.ReadFile(outside); err != nil || string(got) != "do not touch\n" {
		t.Error("a file outside the repository was modified")
	}
}

// A hunk that no longer matches the file must fail loudly rather than
// corrupting it.
func TestRevertHunkFailsWhenFileMovedOn(t *testing.T) {
	repo := newDiffRepo(t)
	writeFile(t, repo, "a.txt", "1\n2\n3\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	writeFile(t, repo, "a.txt", "CHANGED\n2\n3\n")

	inst := &Instance{Path: repo}
	files, err := inst.GetFullDiffFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("setup diff failed: %v %+v", err, files)
	}

	// Rewrite the file so the recorded hunk no longer applies.
	writeFile(t, repo, "a.txt", "something else entirely\n")

	patch, err := BuildHunkPatch(files[0], 0)
	if err != nil {
		t.Fatalf("BuildHunkPatch: %v", err)
	}
	if !strings.Contains(patch, "@@") {
		t.Fatalf("patch has no hunk header:\n%s", patch)
	}

	if err := inst.RevertHunk(patch); err == nil {
		t.Error("reverting a stale hunk should fail rather than corrupt the file")
	}
	if got := readFile(t, repo, "a.txt"); got != "something else entirely\n" {
		t.Errorf("file was modified by a failed revert: %q", got)
	}
}

func TestParseDiffFilesHandlesEmptyInput(t *testing.T) {
	if got := ParseDiffFiles(""); got != nil {
		t.Errorf("empty diff = %+v, want nil", got)
	}
	if got := ParseDiffFiles("   \n"); got != nil {
		t.Errorf("blank diff = %+v, want nil", got)
	}
}

// The frontend reverts by sending back the hunk's own Patch field, so that
// field must be directly usable.
func TestHunkPatchFieldIsRevertable(t *testing.T) {
	repo := newDiffRepo(t)
	writeFile(t, repo, "a.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	writeFile(t, repo, "a.txt", "TOP\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\nBOTTOM\n")

	inst := &Instance{Path: repo}
	files, err := inst.GetFullDiffFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("setup: %v %+v", err, files)
	}
	for _, h := range files[0].Hunks {
		if h.Patch == "" {
			t.Fatal("hunk Patch is empty; the UI would have nothing to send back")
		}
	}

	if err := inst.RevertHunk(files[0].Hunks[0].Patch); err != nil {
		t.Fatalf("reverting via the Patch field failed: %v", err)
	}
	got := readFile(t, repo, "a.txt")
	if strings.Contains(got, "TOP") || !strings.Contains(got, "BOTTOM") {
		t.Errorf("wrong hunk reverted:\n%s", got)
	}
}

// The diff tab is shown or hidden based on this, so it has to be right for
// the cases the UI actually meets: repos, plain folders, subdirectories, and
// paths that no longer exist.
func TestIsGitRepoDistinguishesFolders(t *testing.T) {
	repo := newDiffRepo(t)
	if !(&Instance{Path: repo}).IsGitRepo() {
		t.Error("a git repo was reported as non-git")
	}

	if (&Instance{Path: t.TempDir()}).IsGitRepo() {
		t.Error("a plain folder was reported as a git repo")
	}

	// A session opened in a subdirectory still belongs to the repo.
	sub := filepath.Join(repo, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !(&Instance{Path: sub}).IsGitRepo() {
		t.Error("a subdirectory of a repo should still count as git")
	}

	if (&Instance{Path: "/nonexistent/path/xyz"}).IsGitRepo() {
		t.Error("a missing path was reported as a git repo")
	}
}
