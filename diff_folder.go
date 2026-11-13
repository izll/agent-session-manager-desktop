package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"asmgr-desktop/session"
)

// The folder the diff shows.
//
// By default that is the tab's working directory. Another one can be chosen: for
// the tabs that work in the session's own directory the session's choice holds —
// a repository inside a folder that holds several — and a tab with a folder of its
// own makes its own choice. The diff and the git history then show that folder;
// the Files view, the terminal and the rest stay with the tab. Only on this
// computer for now: see checkFolderOn.

// DiffFolder describes the folder the diff shows for one tab.
type DiffFolder struct {
	// Path is the folder shown.
	Path string `json:"path"`
	// TabDir is the tab's own working directory, the one the Files view shows.
	TabDir string `json:"tabDir"`
	// Custom is true when Path is a chosen folder rather than the tab's.
	Custom bool `json:"custom"`
	// Scope is where a choice is kept: "session" for the tabs in the session's
	// directory, "tab" for a tab with a folder of its own.
	Scope string `json:"scope"`
	// ServerID is the server the folder is on; empty for this computer.
	ServerID string `json:"serverId"`
}

// GetDiffFolder returns the folder the diff and the git history show for a tab.
func (a *App) GetDiffFolder(sessionID string, windowIdx int) DiffFolder {
	tabDir := a.GetTabWorkingDirectory(sessionID, windowIdx)
	folder := DiffFolder{Path: tabDir, TabDir: tabDir, Scope: "session"}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return folder
	}
	folder.ServerID = inst.ServerForWindow(windowIdx)
	if !inst.UsesSessionDirectory(windowIdx) {
		folder.Scope = "tab"
	}
	if dir := diffDirFor(inst, windowIdx); dir != "" {
		folder.Path = dir
		folder.Custom = true
	}
	return folder
}

// SetDiffDir chooses the folder a tab's diff shows: for a tab in the session's
// directory that is the session's choice, for one with a folder of its own the
// tab's. Empty, or the tab's own folder, goes back to following the tab.
func (a *App) SetDiffDir(id string, windowIdx int, dir, expectedProjectID string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if inst.UsesSessionDirectory(windowIdx) {
		return a.SetSessionDiffDir(id, dir, expectedProjectID)
	}
	dir, err = a.checkFolderOn(inst.ServerForWindow(windowIdx), dir)
	if err != nil {
		return err
	}

	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err = a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	// Its configured folder, not the one its pane is in now: choosing where the
	// shell has cd-ed to is a choice, which a later cd must not undo.
	if _, tabDir := inst.TabPlacement(windowIdx); dir != "" && session.CanonicalProjectPath(dir) == session.CanonicalProjectPath(tabDir) {
		dir = ""
	}
	if !inst.SetTabDiffDir(windowIdx, dir) {
		return fmt.Errorf("no tab %d in this session", windowIdx)
	}
	return a.storage.UpdateInstance(inst)
}

// SetSessionDiffDir chooses the folder the diff shows for the session's tabs in its
// own directory. Empty, or the session's own directory, goes back to following them.
func (a *App) SetSessionDiffDir(id, dir, expectedProjectID string) error {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	dir, err = a.checkFolderOn(inst.ServerID, dir)
	if err != nil {
		return err
	}

	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err = a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if dir != "" && session.CanonicalProjectPath(dir) == session.CanonicalProjectPath(inst.Path) {
		dir = ""
	}
	inst.DiffDir = dir
	return a.storage.UpdateInstance(inst)
}

// checkFolderOn answers the folder cleaned, once it is known to be one on this
// computer. Empty stays empty.
//
// A folder on a server is refused for now. It could be checked there, but the diff,
// the file browser and their writes do not all run on the tab's server yet: some
// would read a same-named folder on this computer instead, under the server's name.
func (a *App) checkFolderOn(serverID, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if serverID != "" {
		return "", fmt.Errorf("error.folderOnServer")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("the folder must be an absolute path")
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("could not open %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", dir)
	}
	return dir, nil
}

// diffDirFor is the diff folder chosen for a tab, if it applies and still exists.
// A folder deleted since it was chosen falls back to the tab rather than leaving
// the diff pointing at nothing.
func diffDirFor(inst *session.Instance, windowIdx int) string {
	if inst.ServerForWindow(windowIdx) != "" {
		return ""
	}
	dir := inst.DiffDirFor(windowIdx)
	if dir == "" {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}
