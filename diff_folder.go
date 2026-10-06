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
// By default that is the tab's working directory. A session can name another
// one for the tabs that work in its own directory — a repository inside a
// folder that holds several — and the diff and the git history then show that
// instead. The Files view, the terminal and the rest stay with the tab.

// DiffFolder describes the folder the diff shows for one tab.
type DiffFolder struct {
	// Path is the folder shown.
	Path string `json:"path"`
	// TabDir is the tab's own working directory, the one the Files view shows.
	TabDir string `json:"tabDir"`
	// Custom is true when Path is the session's chosen folder.
	Custom bool `json:"custom"`
	// Locked says why the folder cannot be chosen for this tab: "ownFolder"
	// when the tab works in a directory of its own, "remote" for a session on
	// a server. Empty when it can.
	Locked string `json:"locked"`
}

// GetDiffFolder returns the folder the diff and the git history show for a tab.
func (a *App) GetDiffFolder(sessionID string, windowIdx int) DiffFolder {
	tabDir := a.GetTabWorkingDirectory(sessionID, windowIdx)
	folder := DiffFolder{Path: tabDir, TabDir: tabDir}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return folder
	}
	switch {
	case inst.ServerID != "":
		folder.Locked = "remote"
	case !inst.UsesSessionDirectory(windowIdx):
		folder.Locked = "ownFolder"
	}
	if dir := diffDirFor(inst, windowIdx); dir != "" {
		folder.Path = dir
		folder.Custom = true
	}
	return folder
}

// SetSessionDiffDir chooses the folder the session's diff shows. Empty, or the
// session's own directory, goes back to following the tab.
func (a *App) SetSessionDiffDir(id, dir, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}

	dir = strings.TrimSpace(dir)
	if dir != "" {
		// Only a folder on this computer can be checked, and opened by git, here.
		if inst.ServerID != "" {
			return fmt.Errorf("a diff folder can only be chosen for a session on this computer")
		}
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("the diff folder must be an absolute path")
		}
		dir = filepath.Clean(dir)
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("could not open %s: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a folder", dir)
		}
		if session.CanonicalProjectPath(dir) == session.CanonicalProjectPath(inst.Path) {
			dir = ""
		}
	}
	inst.DiffDir = dir
	return a.storage.UpdateInstance(inst)
}

// diffDirFor is the session's diff folder for a tab, if it applies and still
// exists. A folder deleted since it was chosen falls back to the tab rather
// than leaving the diff pointing at nothing.
func diffDirFor(inst *session.Instance, windowIdx int) string {
	if inst.ServerID != "" {
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
