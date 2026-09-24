package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// The checkpoint tests run against real repositories: what matters is what
// git does to the files, the index and the refs, and a fake would only check
// that the commands were spelled as expected.

func checkpointTestRepo(t *testing.T) string {
	t.Helper()
	repo := resolvedTempDir(t)
	dashboardGit(t, repo, "init", "--initial-branch=main")
	dashboardGit(t, repo, "config", "user.name", "Checkpoint Tester")
	dashboardGit(t, repo, "config", "user.email", "checkpoint@example.invalid")
	dashboardGit(t, repo, "config", "commit.gpgsign", "false")
	return repo
}

func writeRepoFile(t *testing.T, repo, name, contents string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// treeState is the work tree as a map from path to what is there: contents
// and executable bit for a file, the target for a symlink. .git is left out.
func treeState(t *testing.T, repo string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			return filepath.SkipDir
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			state[rel] = "link:" + target
		case info.IsDir():
			state[rel+"/"] = "dir"
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			mode := "file"
			if runtime.GOOS != "windows" && info.Mode()&0o111 != 0 {
				mode = "exec"
			}
			state[rel] = mode + ":" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// repoBookkeeping is everything a checkpoint must leave alone besides the
// files: the index byte for byte, HEAD, the branch, the stash.
type repoBookkeeping struct {
	index  string
	head   string
	branch string
	stash  string
	refs   string
}

func readBookkeeping(t *testing.T, repo string) repoBookkeeping {
	t.Helper()
	index, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	head, _ := os.ReadFile(filepath.Join(repo, ".git", "HEAD"))
	stash, _ := runGitStdout(context.Background(), repo, "stash", "list", "--format=%H %gs")
	refs, _ := runGitStdout(context.Background(), repo, "for-each-ref", "--format=%(refname) %(objectname)",
		"refs/heads", "refs/tags", "refs/stash")
	branch, _ := runGitStdout(context.Background(), repo, "rev-parse", "HEAD")
	return repoBookkeeping{index: string(index), head: string(head), branch: branch, stash: stash, refs: refs}
}

func checkpointFiles(t *testing.T, repo, hash string) []string {
	t.Helper()
	out := dashboardGit(t, repo, "ls-tree", "-r", "-z", "--name-only", hash)
	var names []string
	for _, name := range strings.Split(out, "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func newCheckpoint(t *testing.T, repo, label string) Checkpoint {
	t.Helper()
	created, _, err := createCheckpoint(context.Background(), repo, checkpointMeta{Kind: checkpointKindManual, Label: label})
	if err != nil {
		t.Fatalf("createCheckpoint: %v", err)
	}
	return created
}

func TestCreateCheckpointLeavesRepositoryAlone(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, ".gitignore", "*.log\nbuild/\n")
	writeRepoFile(t, repo, "tracked.txt", "one\n")
	writeRepoFile(t, repo, "staged.txt", "base\n")
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")

	// Something in the stash, something staged, something only modified,
	// something untracked and something ignored: each is a different part of
	// git's state a snapshot could disturb.
	writeRepoFile(t, repo, "tracked.txt", "stashed change\n")
	dashboardGit(t, repo, "stash", "push", "-m", "user stash")
	writeRepoFile(t, repo, "staged.txt", "staged change\n")
	dashboardGit(t, repo, "add", "staged.txt")
	writeRepoFile(t, repo, "tracked.txt", "modified, not staged\n")
	writeRepoFile(t, repo, "new file.txt", "untracked\n")
	writeRepoFile(t, repo, "debug.log", "ignored\n")
	writeRepoFile(t, repo, "build/out.bin", "ignored too\n")

	filesBefore := treeState(t, repo)
	bookBefore := readBookkeeping(t, repo)

	created := newCheckpoint(t, repo, "before the agent")

	if got := treeState(t, repo); !reflect.DeepEqual(got, filesBefore) {
		t.Errorf("the work tree changed:\nbefore %v\nafter  %v", filesBefore, got)
	}
	if got := readBookkeeping(t, repo); got != bookBefore {
		t.Errorf("index/HEAD/branch/stash changed:\nbefore %+v\nafter  %+v", bookBefore, got)
	}

	files := checkpointFiles(t, repo, created.Hash)
	want := []string{".gitignore", "new file.txt", "staged.txt", "tracked.txt"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("checkpoint holds %v, want %v (untracked in, ignored out)", files, want)
	}
	if got := dashboardGit(t, repo, "show", created.Hash+":tracked.txt"); got != "modified, not staged\n" {
		t.Errorf("the checkpoint saved %q, not the file on disk", got)
	}

	// Private: not a branch, not a tag, but a ref that keeps it alive.
	if out := dashboardGit(t, repo, "branch", "-a") + dashboardGit(t, repo, "tag"); strings.Contains(out, "checkpoint") {
		t.Errorf("the checkpoint shows as a branch or tag: %q", out)
	}
	if out := dashboardGit(t, repo, "for-each-ref", "refs/asmgr/checkpoints/"); !strings.Contains(out, created.Hash) {
		t.Errorf("no ref holds the checkpoint: %q", out)
	}
	if parent := strings.TrimSpace(dashboardGit(t, repo, "rev-parse", created.Hash+"^")); parent != strings.TrimSpace(bookBefore.branch) {
		t.Errorf("parent = %s, want HEAD %s", parent, bookBefore.branch)
	}
}

func TestRestoreCheckpointBringsBackTheTree(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, ".gitignore", "*.log\n")
	writeRepoFile(t, repo, "keep.txt", "keep\n")
	writeRepoFile(t, repo, "edit me.txt", "original\n")
	writeRepoFile(t, repo, "mappa/ünï cödé.txt", "unicode original\n")
	writeRepoFile(t, repo, "script.sh", "#!/bin/sh\necho hi\n")
	symlinks := runtime.GOOS != "windows"
	if symlinks {
		if err := os.Chmod(filepath.Join(repo, "script.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("keep.txt", filepath.Join(repo, "link")); err != nil {
			t.Fatal(err)
		}
	}
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")
	writeRepoFile(t, repo, "untracked at checkpoint.txt", "was untracked\n")
	writeRepoFile(t, repo, "notes.log", "ignored before\n")

	saved := treeState(t, repo)
	created := newCheckpoint(t, repo, "")

	// What an agent might do: edit, delete, add, change a mode and a link.
	writeRepoFile(t, repo, "edit me.txt", "agent rewrote this\n")
	if err := os.Remove(filepath.Join(repo, "mappa", "ünï cödé.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "untracked at checkpoint.txt")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "agent dir/új fájl.txt", "added since\n")
	writeRepoFile(t, repo, "added.txt", "added since\n")
	writeRepoFile(t, repo, "notes.log", "ignored, changed since\n")
	if symlinks {
		if err := os.Chmod(filepath.Join(repo, "script.sh"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(repo, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("edit me.txt", filepath.Join(repo, "link")); err != nil {
			t.Fatal(err)
		}
	}
	// And a commit and a staged change, which a restore must not undo.
	dashboardGit(t, repo, "add", "added.txt")
	dashboardGit(t, repo, "commit", "-m", "agent commit")
	writeRepoFile(t, repo, "keep.txt", "staged by the user\n")
	dashboardGit(t, repo, "add", "keep.txt")
	writeRepoFile(t, repo, "keep.txt", "keep\n")

	bookBefore := readBookkeeping(t, repo)

	result, err := restoreCheckpoint(context.Background(), repo, created.ID, "")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	want := map[string]string{}
	for k, v := range saved {
		want[k] = v
	}
	// Ignored files are neither saved nor touched: the log keeps what it has
	// now, not what it had.
	want["notes.log"] = "file:ignored, changed since\n"
	if got := treeState(t, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("tree after restore:\n got %v\nwant %v", got, want)
	}
	if got := readBookkeeping(t, repo); got != bookBefore {
		t.Errorf("restore moved HEAD/index/branch/stash:\nbefore %+v\nafter  %+v", bookBefore, got)
	}
	if result.Removed == 0 || result.Written == 0 {
		t.Errorf("result = %+v, expected both writes and removals", result)
	}
	// git status shows the restored state against the unchanged HEAD.
	status := dashboardGit(t, repo, "status", "--porcelain")
	if !strings.Contains(status, "added.txt") {
		t.Errorf("status does not show the committed-since file as deleted: %q", status)
	}
}

func TestRestoreIsItselfUndoable(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "first\n")
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")
	created := newCheckpoint(t, repo, "good state")

	writeRepoFile(t, repo, "a.txt", "agent work worth keeping after all\n")
	writeRepoFile(t, repo, "b/new.txt", "new\n")
	preRestore := treeState(t, repo)

	result, err := restoreCheckpoint(context.Background(), repo, created.ID, "sess")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Before.Kind != checkpointKindBeforeRestore || result.Before.RestoredFrom != created.ID {
		t.Fatalf("before-restore checkpoint = %+v", result.Before)
	}

	list, err := listCheckpoints(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Checkpoints) != 2 {
		t.Fatalf("expected two checkpoints, got %+v", list.Checkpoints)
	}
	newest := list.Checkpoints[0]
	if newest.ID != result.Before.ID || newest.Kind != checkpointKindBeforeRestore ||
		newest.RestoredFrom != created.ID || newest.Session != "sess" {
		t.Errorf("newest checkpoint = %+v, want the before-restore one", newest)
	}
	if list.Checkpoints[1].Label != "good state" || list.Checkpoints[1].Files != 0 || !list.Checkpoints[1].StatsKnown {
		t.Errorf("the restored checkpoint should equal now: %+v", list.Checkpoints[1])
	}
	if newest.Files != 2 {
		t.Errorf("before-restore differs from now in %d files, want 2", newest.Files)
	}

	if _, err := restoreCheckpoint(context.Background(), repo, result.Before.ID, ""); err != nil {
		t.Fatalf("undoing the restore: %v", err)
	}
	if got := treeState(t, repo); !reflect.DeepEqual(got, preRestore) {
		t.Errorf("undo did not return to the pre-restore tree:\n got %v\nwant %v", got, preRestore)
	}
}

func TestCheckpointInRepositoryWithoutCommits(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "first.txt", "first\n")
	before := readBookkeeping(t, repo)

	created := newCheckpoint(t, repo, "empty repo")
	if got := readBookkeeping(t, repo); got != before {
		t.Errorf("repository state changed: %+v -> %+v", before, got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index")); err == nil {
		t.Error("the checkpoint created the user's index")
	}
	if out, err := runGitStdout(context.Background(), repo, "rev-parse", "--verify", "--quiet", created.Hash+"^"); err == nil {
		t.Errorf("a first checkpoint has a parent: %q", out)
	}

	writeRepoFile(t, repo, "first.txt", "changed\n")
	writeRepoFile(t, repo, "second.txt", "second\n")
	if _, err := restoreCheckpoint(context.Background(), repo, created.ID, ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
	want := map[string]string{"first.txt": "file:first\n"}
	if got := treeState(t, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
}

func TestRestoreRefusesToDeleteADirectoryHoldingIgnoredFiles(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, ".gitignore", "*.secret\n")
	writeRepoFile(t, repo, "config", "a file here\n")
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")
	created := newCheckpoint(t, repo, "")

	// Now "config" is a directory, holding an ignored file checkout-index
	// would delete with it.
	if err := os.Remove(filepath.Join(repo, "config")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "config/tracked.txt", "x\n")
	writeRepoFile(t, repo, "config/key.secret", "do not lose me\n")
	before := treeState(t, repo)

	_, err := restoreCheckpoint(context.Background(), repo, created.ID, "")
	if err == nil || !strings.HasPrefix(err.Error(), "error.checkpointDirectoryInTheWay|") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if got := treeState(t, repo); !reflect.DeepEqual(got, before) {
		t.Errorf("a refused restore still changed files:\n got %v\nwant %v", got, before)
	}
}

func TestDeleteCheckpointRemovesItsRef(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	first := newCheckpoint(t, repo, "one")
	second := newCheckpoint(t, repo, "two")

	if err := deleteCheckpoint(context.Background(), repo, first.ID); err != nil {
		t.Fatal(err)
	}
	list, err := listCheckpoints(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Checkpoints) != 1 || list.Checkpoints[0].ID != second.ID {
		t.Errorf("after delete: %+v", list.Checkpoints)
	}
	if out := dashboardGit(t, repo, "for-each-ref", "refs/asmgr/"); strings.Contains(out, first.Hash) {
		t.Errorf("the deleted checkpoint's ref is still there: %q", out)
	}
	if err := deleteCheckpoint(context.Background(), repo, first.ID); err == nil {
		t.Error("deleting a deleted checkpoint succeeded")
	}
	if err := deleteCheckpoint(context.Background(), repo, "../../heads/main"); err == nil {
		t.Error("a malformed id was accepted")
	}
}

func TestCheckpointLabelCannotForgeMetadata(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	created := newCheckpoint(t, repo, "try this\nAsmgr-Checkpoint-Kind: beforeRestore")
	list, err := listCheckpoints(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Checkpoints) != 1 || list.Checkpoints[0].Kind != checkpointKindManual {
		t.Fatalf("a label turned into metadata: %+v", list.Checkpoints)
	}
	if created.Label != "try this Asmgr-Checkpoint-Kind: beforeRestore" {
		t.Errorf("label = %q", created.Label)
	}
}

// Each linked worktree has its own list: a checkpoint of one branch's files
// restored into another worktree would overwrite it wholesale.
func TestCheckpointsArePerWorktree(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")
	linked := filepath.Join(resolvedTempDir(t), "linked tree")
	dashboardGit(t, repo, "worktree", "add", "-b", "other", linked)

	newCheckpoint(t, repo, "main one")
	list, err := listCheckpoints(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Checkpoints) != 0 {
		t.Errorf("the linked worktree lists the main one's checkpoints: %+v", list.Checkpoints)
	}
	newCheckpoint(t, linked, "linked one")
	list, _ = listCheckpoints(context.Background(), linked)
	if len(list.Checkpoints) != 1 || list.Checkpoints[0].Label != "linked one" {
		t.Errorf("linked list = %+v", list.Checkpoints)
	}
}

func checkpointTestApp(t *testing.T, inst *session.Instance) *App {
	t.Helper()
	storage := guardedTestStorage(t)
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	return &App{storage: storage, projectLocked: true}
}

func TestCheckpointAPIRejectsRootsOutsideTheTab(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	other := checkpointTestRepo(t)
	writeRepoFile(t, other, "b.txt", "b\n")

	app := checkpointTestApp(t, &session.Instance{ID: "cp", Name: "cp", Path: repo, Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()

	if _, err := app.ListCheckpoints("cp", -1, other); err == nil {
		t.Error("listing accepted a root that is not the tab's")
	}
	if _, err := app.CreateCheckpoint("cp", -1, other, "x", project); err == nil {
		t.Error("creating accepted a root that is not the tab's")
	}
	if out := dashboardGit(t, other, "for-each-ref", "refs/asmgr/"); out != "" {
		t.Errorf("a checkpoint was written into the other repository: %q", out)
	}
	if _, err := app.ListCheckpoints("cp", -1, ""); err == nil {
		t.Error("listing accepted no root at all")
	}

	created, err := app.CreateCheckpoint("cp", -1, repo, "ok", project)
	if err != nil {
		t.Fatalf("the tab's own root was rejected: %v", err)
	}
	if _, err := app.RestoreCheckpoint("cp", -1, other, created.ID, project); err == nil {
		t.Error("restoring accepted a root that is not the tab's")
	}
	if err := app.DeleteCheckpoint("cp", -1, other, created.ID, project); err == nil {
		t.Error("deleting accepted a root that is not the tab's")
	}
	if _, err := app.CreateCheckpoint("cp", -1, repo, "x", "another-project"); err == nil {
		t.Error("creating ignored the expected project")
	}
}

func TestCheckpointAPIRefusesRemoteAndNonRepository(t *testing.T) {
	plain := resolvedTempDir(t)
	app := checkpointTestApp(t, &session.Instance{ID: "plain", Name: "plain", Path: plain, Status: session.StatusStopped})
	if _, err := app.ListCheckpoints("plain", -1, plain); err == nil || err.Error() != "error.checkpointsNotRepository" {
		t.Errorf("non-repository: err = %v", err)
	}

	repo := checkpointTestRepo(t)
	remote := checkpointTestApp(t, &session.Instance{ID: "far", Name: "far", Path: repo, ServerID: "srv", Status: session.StatusStopped})
	if _, err := remote.ListCheckpoints("far", -1, repo); err == nil || err.Error() != "error.checkpointsRemote" {
		t.Errorf("remote session: err = %v", err)
	}
}

func TestParseRawDiffZ(t *testing.T) {
	out := ":000000 100644 0000000 1111111 A\x00new file.txt\x00" +
		":100644 000000 1111111 0000000 D\x00gone/ü.txt\x00" +
		":100644 100755 1111111 1111111 M\x00mode.sh\x00" +
		":000000 160000 0000000 2222222 A\x00sub\x00" +
		":160000 000000 2222222 0000000 D\x00nested\x00"
	changes, err := parseRawDiffZ(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []checkpointChange{
		{path: "new file.txt", write: true},
		{path: "gone/ü.txt"},
		{path: "mode.sh", write: true},
		{path: "sub", write: true, gitlink: true},
		{path: "nested", gitlink: true},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("got %+v\nwant %+v", changes, want)
	}
}

func TestCheckpointPathStaysInside(t *testing.T) {
	top := filepath.FromSlash("/repo")
	for _, bad := range []string{"", "../x", "a/../../x", "/etc/passwd", ".git/config", "a/.git/hooks/x", "a//b"} {
		if _, err := checkpointPath(top, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if got, err := checkpointPath(top, "dir/ü file.txt"); err != nil || got != filepath.Join(top, "dir", "ü file.txt") {
		t.Errorf("a normal path: %q, %v", got, err)
	}
}

func TestParseShortStat(t *testing.T) {
	files, ins, del := parseShortStat(" 3 files changed, 10 insertions(+), 2 deletions(-)\n")
	if files != 3 || ins != 10 || del != 2 {
		t.Errorf("got %d %d %d", files, ins, del)
	}
	files, ins, del = parseShortStat(" 1 file changed, 1 deletion(-)\n")
	if files != 1 || ins != 0 || del != 1 {
		t.Errorf("got %d %d %d", files, ins, del)
	}
	if files, _, _ := parseShortStat(""); files != 0 {
		t.Errorf("empty output means no difference, got %d files", files)
	}
}
