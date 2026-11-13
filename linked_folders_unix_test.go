//go:build !windows

package main

import "testing"

// A request for a tab that has since closed must not land on the session's own
// window, whose folders would answer for it.
func TestALinkedFolderRequestForAClosedTabIsRefused(t *testing.T) {
	app, _, inst := backgroundTestApp(t)
	project := app.storage.GetActiveProjectID()
	dir := t.TempDir()
	if _, err := app.AddTabLinkedFolder("cmds", 0, dir, project); err != nil {
		t.Fatalf("the session's own window: %v", err)
	}
	other := t.TempDir()
	if _, err := app.AddTabLinkedFolder("cmds", 5, other, project); err == nil {
		t.Error("a folder was linked to a tab that is not there")
	}
	if saved, _ := app.storage.GetInstance(inst.ID); len(saved.LinkedDirs) != 1 {
		t.Errorf("the session's own folders = %v", saved.LinkedDirs)
	}
	if _, err := app.RemoveTabLinkedFolder("cmds", 5, dir, project); err == nil {
		t.Error("a closed tab unlinked the session's folder")
	}
	if got := app.GetTabLinkedFolders("cmds", 5); len(got) != 0 {
		t.Errorf("a closed tab listed %+v", got)
	}
	browsed, err := app.browseInstance("cmds", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(browsed.LinkedRoots) != 0 {
		t.Errorf("a closed tab may read %v", browsed.LinkedRoots)
	}
	if got := app.GetTabLinkedFolders("cmds", 0); len(got) != 1 {
		t.Errorf("the session's own window lost its folder: %+v", got)
	}
}
