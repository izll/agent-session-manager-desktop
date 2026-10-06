package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// A session in a folder that holds several repositories: the folder itself is
// not one, the repository chosen for the diff is.
func diffFolderTestApp(t *testing.T, tabs ...session.FollowedWindow) (app *App, parent, inner, project string) {
	t.Helper()
	parent = resolvedTempDir(t)
	inner = filepath.Join(parent, "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	dashboardGit(t, inner, "init", "--initial-branch=main")
	dashboardGit(t, inner, "config", "user.name", "Diff Folder Tester")
	dashboardGit(t, inner, "config", "user.email", "diff@example.invalid")
	dashboardGit(t, inner, "config", "commit.gpgsign", "false")
	writeRepoFile(t, inner, "a.txt", "a\n")
	app = checkpointTestApp(t, &session.Instance{ID: "s", Name: "s", Path: parent,
		Status: session.StatusStopped, FollowedWindows: tabs})
	return app, parent, inner, app.storage.GetActiveProjectID()
}

func TestTheDiffFollowsTheTabUntilAFolderIsChosen(t *testing.T) {
	app, parent, inner, project := diffFolderTestApp(t)

	if got := app.GetDiffFolder("s", 0); got.Path != parent || got.Custom || got.Locked != "" {
		t.Errorf("before choosing: %+v, want the session's folder", got)
	}
	if app.TabIsGitRepo("s", 0) {
		t.Error("the session's folder is not a repository, but the diff was offered")
	}

	if err := app.SetSessionDiffDir("s", inner, project); err != nil {
		t.Fatalf("choose: %v", err)
	}
	got := app.GetDiffFolder("s", 0)
	if got.Path != inner || !got.Custom || got.TabDir != parent {
		t.Errorf("after choosing: %+v, want %s shown over %s", got, inner, parent)
	}
	if !app.TabIsGitRepo("s", 0) {
		t.Error("the chosen folder is a repository, but the diff was not offered")
	}
	files, err := app.GetFullDiffFileList("s", 0, inner)
	if err != nil {
		t.Fatalf("diff of the chosen folder: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("files = %+v, want a.txt", files)
	}

	// Back to the session's folder, by clearing or by choosing it.
	for _, dir := range []string{"", parent} {
		if err := app.SetSessionDiffDir("s", inner, project); err != nil {
			t.Fatal(err)
		}
		if err := app.SetSessionDiffDir("s", dir, project); err != nil {
			t.Fatalf("reset with %q: %v", dir, err)
		}
		if got := app.GetDiffFolder("s", 0); got.Path != parent || got.Custom {
			t.Errorf("reset with %q: %+v", dir, got)
		}
	}
}

// A tab opened in a folder of its own — a worktree, say — shows that folder:
// the session's choice is about the tabs that would show the session's.
func TestATabWithItsOwnFolderKeepsIt(t *testing.T) {
	other := checkpointTestRepo(t)
	app, _, inner, project := diffFolderTestApp(t,
		session.FollowedWindow{ID: "own", Index: 1, Name: "own", Agent: session.AgentTerminal, WorkDir: other})
	if err := app.SetSessionDiffDir("s", inner, project); err != nil {
		t.Fatal(err)
	}

	// The main tab does show the chosen folder; the other one does not.
	if _, err := app.GetFullDiffFileList("s", 0, inner); err != nil {
		t.Fatalf("main tab: %v", err)
	}
	got := app.GetDiffFolder("s", 1)
	if got.Path != other || got.Custom || got.Locked != "ownFolder" {
		t.Errorf("own-folder tab: %+v, want %s and locked", got, other)
	}
	if _, err := app.GetFullDiffFileList("s", 1, inner); err == nil {
		t.Error("the own-folder tab accepted the session's diff folder as its root")
	}
	if _, err := app.GetFullDiffFileList("s", 1, other); err != nil {
		t.Errorf("own-folder tab, its own root: %v", err)
	}
}

func TestAnUnrelatedRootIsStillRefused(t *testing.T) {
	app, _, inner, project := diffFolderTestApp(t)
	if err := app.SetSessionDiffDir("s", inner, project); err != nil {
		t.Fatal(err)
	}
	unrelated := checkpointTestRepo(t)
	if _, err := app.GetFullDiffFileList("s", 0, unrelated); err == nil || !strings.Contains(err.Error(), "reopen the diff") {
		t.Errorf("an unrelated root: %v, want it refused", err)
	}
	if err := app.RevertDiffFile("s", "a.txt", false, 0, unrelated, project); err == nil {
		t.Error("a revert accepted an unrelated root")
	}
}

func TestChoosingADiffFolderChecksIt(t *testing.T) {
	app, parent, _, project := diffFolderTestApp(t)
	file := filepath.Join(parent, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"inner", file, filepath.Join(parent, "missing")} {
		if err := app.SetSessionDiffDir("s", dir, project); err == nil {
			t.Errorf("%q was accepted as the diff folder", dir)
		}
	}
	if got := app.GetDiffFolder("s", 0); got.Custom {
		t.Errorf("a refused folder was kept: %+v", got)
	}
}

// A folder deleted since it was chosen gives way to the tab's, rather than
// leaving the diff pointing at nothing.
func TestADeletedDiffFolderFallsBackToTheTab(t *testing.T) {
	app, parent, inner, project := diffFolderTestApp(t)
	if err := app.SetSessionDiffDir("s", inner, project); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(inner); err != nil {
		t.Fatal(err)
	}
	if got := app.GetDiffFolder("s", 0); got.Path != parent || got.Custom {
		t.Errorf("after deleting the folder: %+v, want the session's", got)
	}
}

func TestADiffFolderIsForSessionsOnThisComputer(t *testing.T) {
	app, _, inner, project := diffFolderTestApp(t)
	inst, err := app.storage.GetInstance("s")
	if err != nil {
		t.Fatal(err)
	}
	inst.ServerID = "srv1"
	if err := app.storage.UpdateInstance(inst); err != nil {
		t.Fatal(err)
	}
	if err := app.SetSessionDiffDir("s", inner, project); err == nil {
		t.Error("a diff folder was accepted for a session on a server")
	}
	if got := app.GetDiffFolder("s", 0); got.Locked != "remote" {
		t.Errorf("server session: %+v, want it locked as remote", got)
	}
}
