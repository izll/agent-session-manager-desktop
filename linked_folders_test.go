package main

import (
	"os"
	"path/filepath"
	"testing"

	"asmgr-desktop/session"
)

// A tab can keep other folders beside its own, to see their diff and files.
func TestATabKeepsItsLinkedFolders(t *testing.T) {
	other := checkpointTestRepo(t)
	app, parent, inner, project := diffFolderTestApp(t,
		session.FollowedWindow{ID: "own", Index: 1, Name: "own", Agent: session.AgentTerminal})
	plain := filepath.Join(parent, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{inner, other + "/", plain, inner} {
		if _, err := app.AddTabLinkedFolder("s", 1, dir, project); err != nil {
			t.Fatalf("add %s: %v", dir, err)
		}
	}
	got := app.GetTabLinkedFolders("s", 1)
	if len(got) != 3 || got[0].Path != inner || got[1].Path != other || got[2].Path != plain {
		t.Fatalf("folders = %+v", got)
	}
	if !got[0].IsGitRepo || !got[1].IsGitRepo || got[2].IsGitRepo {
		t.Errorf("repositories = %+v", got)
	}
	// Another tab, and the session's own, keep their own lists.
	if len(app.GetTabLinkedFolders("s", 0)) != 0 {
		t.Error("the session's own tab got the folders too")
	}
	if _, err := app.AddTabLinkedFolder("s", 0, plain, project); err != nil {
		t.Fatalf("add to the session's own tab: %v", err)
	}
	if main := app.GetTabLinkedFolders("s", 0); len(main) != 1 || main[0].Path != plain {
		t.Errorf("session's own tab = %+v", main)
	}

	if _, err := app.RemoveTabLinkedFolder("s", 1, other, project); err != nil {
		t.Fatal(err)
	}
	if got := app.GetTabLinkedFolders("s", 1); len(got) != 2 || got[1].Path != plain {
		t.Errorf("after removing: %+v", got)
	}
}

func TestALinkedFolderIsChecked(t *testing.T) {
	app, parent, _, project := diffFolderTestApp(t)
	file := filepath.Join(parent, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", "relative", file, filepath.Join(parent, "missing")} {
		if _, err := app.AddTabLinkedFolder("s", 0, dir, project); err == nil {
			t.Errorf("%q was linked", dir)
		}
	}
}

// A folder deleted since it was linked stays listed, marked, so it can be removed.
func TestADeletedLinkedFolderIsMarked(t *testing.T) {
	app, _, inner, project := diffFolderTestApp(t)
	if _, err := app.AddTabLinkedFolder("s", 0, inner, project); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(inner); err != nil {
		t.Fatal(err)
	}
	if got := app.GetTabLinkedFolders("s", 0); len(got) != 1 || !got[0].Missing {
		t.Errorf("folders = %+v", got)
	}
}

// The diff and the files of a linked folder are the tab's to read; an unrelated
// folder still is not.
func TestALinkedFolderIsARootOfItsTab(t *testing.T) {
	other := checkpointTestRepo(t)
	app, _, inner, project := diffFolderTestApp(t,
		session.FollowedWindow{ID: "own", Index: 1, Name: "own", Agent: session.AgentTerminal})
	if _, err := app.AddTabLinkedFolder("s", 1, inner, project); err != nil {
		t.Fatal(err)
	}
	if files, err := app.GetFullDiffFileList("s", 1, inner); err != nil || len(files) != 1 || files[0].Path != "a.txt" {
		t.Errorf("linked diff: %+v, %v", files, err)
	}
	if listing, err := app.ListSessionDirectory("s", "", 1, inner); err != nil || len(listing.Entries) == 0 {
		t.Errorf("linked files: %+v, %v", listing, err)
	}
	if _, err := app.GetFullDiffFileList("s", 0, inner); err == nil {
		t.Error("another tab read the linked folder")
	}
	if _, err := app.GetFullDiffFileList("s", 1, other); err == nil {
		t.Error("an unlinked folder was read")
	}
}

// Not on a server yet: its files would be read on this computer instead.
func TestALinkedFolderOnAServerIsRefusedForNow(t *testing.T) {
	app, parent, _, project := diffFolderTestApp(t)
	inst, _ := app.storage.GetInstance("s")
	inst.ServerID = "srv1"
	if err := app.storage.UpdateInstance(inst); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddTabLinkedFolder("s", 0, parent, project); err == nil {
		t.Error("a folder was linked to a tab on a server")
	}
}

// A root this computer cannot resolve is refused, as before linked folders.
func TestAnUnresolvableRootIsRefused(t *testing.T) {
	if _, ok := sameRoot("/no/such/dir", "/no/such/dir"); ok {
		t.Error("a missing folder was accepted as a root")
	}
}

// The commit history of a linked folder is the tab's to read, like its diff.
func TestALinkedFoldersHistoryCanBeRead(t *testing.T) {
	linked := checkpointTestRepo(t)
	unlinked := checkpointTestRepo(t)
	for _, repo := range []string{linked, unlinked} {
		writeRepoFile(t, repo, "a.txt", "a\n")
		dashboardGit(t, repo, "add", ".")
		dashboardGit(t, repo, "commit", "-m", "first")
	}
	app, _, _, project := diffFolderTestApp(t,
		session.FollowedWindow{ID: "own", Index: 1, Name: "own", Agent: session.AgentTerminal})
	if _, err := app.AddTabLinkedFolder("s", 1, linked, project); err != nil {
		t.Fatal(err)
	}
	page, err := app.GetGitHistory("s", "", 0, 1, linked)
	if err != nil || len(page.Commits) == 0 {
		t.Fatalf("linked history: %+v, %v", page, err)
	}
	if _, err := app.ListGitBranches("s", 1, linked); err != nil {
		t.Errorf("linked branches: %v", err)
	}
	if _, err := app.GetGitHistory("s", "", 0, 1, unlinked); err == nil {
		t.Error("an unlinked folder's history was read")
	}
}
