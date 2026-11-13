package main

import (
	"context"
	"fmt"
	"os"

	"asmgr-desktop/session"
)

// Folders linked to a tab: kept beside its own, so their diff and files can be seen
// from the tab without opening another. On this computer only, for now: see
// checkFolderOn.

// LinkedFolder is one folder linked to a tab.
type LinkedFolder struct {
	Path      string `json:"path"`
	IsGitRepo bool   `json:"isGitRepo"`
	// Missing: deleted since it was linked.
	Missing bool `json:"missing"`
}

// GetTabLinkedFolders lists the folders linked to a tab, in the order they were added.
func (a *App) GetTabLinkedFolders(id string, windowIdx int) []LinkedFolder {
	inst, err := a.storage.GetInstance(id)
	if err != nil || !a.tabExists(inst, windowIdx) {
		return []LinkedFolder{}
	}
	folders := []LinkedFolder{}
	for _, dir := range inst.LinkedDirsFor(windowIdx) {
		folder := LinkedFolder{Path: dir}
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			folder.Missing = true
		}
		if !folder.Missing {
			inst.BrowseRoot = dir
			folder.IsGitRepo = inst.IsGitRepo()
		}
		folders = append(folders, folder)
	}
	return folders
}

// AddTabLinkedFolder links a folder to a tab, once: one already linked stays where it is.
func (a *App) AddTabLinkedFolder(id string, windowIdx int, dir, expectedProjectID string) ([]LinkedFolder, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}
	dir, err = a.checkFolderOn(inst.ServerForWindow(windowIdx), dir)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, fmt.Errorf("choose a folder to link")
	}
	err = a.changeLinkedDirs(id, windowIdx, expectedProjectID, func(dirs []string) []string {
		for _, known := range dirs {
			if session.CanonicalProjectPath(known) == session.CanonicalProjectPath(dir) {
				return dirs
			}
		}
		return append(dirs, dir)
	})
	if err != nil {
		return nil, err
	}
	return a.GetTabLinkedFolders(id, windowIdx), nil
}

// RemoveTabLinkedFolder unlinks a folder from a tab. The folder itself is left alone.
func (a *App) RemoveTabLinkedFolder(id string, windowIdx int, dir, expectedProjectID string) ([]LinkedFolder, error) {
	err := a.changeLinkedDirs(id, windowIdx, expectedProjectID, func(dirs []string) []string {
		kept := []string{}
		for _, known := range dirs {
			if known != dir {
				kept = append(kept, known)
			}
		}
		return kept
	})
	if err != nil {
		return nil, err
	}
	return a.GetTabLinkedFolders(id, windowIdx), nil
}

func (a *App) changeLinkedDirs(id string, windowIdx int, expectedProjectID string, change func([]string) []string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	if !a.tabExists(inst, windowIdx) {
		return fmt.Errorf("error.tabClosed")
	}
	current := append([]string(nil), inst.LinkedDirsFor(windowIdx)...)
	inst.SetLinkedDirs(windowIdx, change(current))
	return a.storage.UpdateInstance(inst)
}

// tabExists asks tmux, within the usual bound, whether a tab without a record is the
// session's own window rather than one closed since.
func (a *App) tabExists(inst *session.Instance, windowIdx int) bool {
	ctx, cancel := context.WithTimeout(a.lifecycleContext(), session.TmuxCommandTimeout)
	defer cancel()
	return inst.HasTabContext(ctx, windowIdx)
}
