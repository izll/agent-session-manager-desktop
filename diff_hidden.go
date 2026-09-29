package main

import (
	"sync"

	"asmgr-desktop/diffhidden"
)

// Files kept out of the diff view — by the view only. See package diffhidden:
// nothing here writes to git.

// DiffHiddenRules is one repository's list, with the key it is stored under.
// The key comes back so the frontend can share one list between two diff
// views of the same repository opened from different directories.
type DiffHiddenRules struct {
	Repo  string   `json:"repo"`
	Rules []string `json:"rules"`
}

var diffHiddenStore = sync.OnceValue(func() *diffhidden.Store {
	return diffhidden.New(diffhidden.DefaultDir())
})

// diffHiddenRepoKey names the repository the rules belong to: the top of its
// working tree, so a tab opened in a subdirectory shares the list with one at
// the root. A tab on a server is keyed by the server as well — the same path
// on two machines is two checkouts.
func diffHiddenRepoKey(serverID, topLevel string) string {
	if topLevel == "" {
		return ""
	}
	if serverID != "" {
		return serverID + ":" + topLevel
	}
	return topLevel
}

// diffHiddenRepo resolves the repository behind a diff the frontend has on
// screen, checking the directory it was loaded from is still the tab's.
func (a *App) diffHiddenRepo(id string, windowIdx int, expectedRoot string) (string, error) {
	inst, err := a.browseInstance(id, windowIdx)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := validateDiffRoot(inst, expectedRoot)
	if err != nil {
		return "", err
	}
	serverID := inst.ServerForWindow(windowIdx)
	key := diffHiddenRepoKey(serverID, inst.RepoRootOn(serverID, resolvedRoot))
	if key == "" {
		return "", diffhidden.ErrNoRepository
	}
	return key, nil
}

// GetDiffHiddenRules returns the rules for the repository the tab's diff is of.
func (a *App) GetDiffHiddenRules(id string, windowIdx int, expectedRoot string) (DiffHiddenRules, error) {
	repo, err := a.diffHiddenRepo(id, windowIdx, expectedRoot)
	if err != nil {
		return DiffHiddenRules{Rules: []string{}}, err
	}
	rules, err := diffHiddenStore().Rules(repo)
	return DiffHiddenRules{Repo: repo, Rules: rules}, err
}

// AddDiffHiddenRule hides a file, a folder ("dir/") or a pattern ("*.lock")
// from the repository's diff view, and returns the list as it now stands.
func (a *App) AddDiffHiddenRule(id string, windowIdx int, expectedRoot, rule string) (DiffHiddenRules, error) {
	repo, err := a.diffHiddenRepo(id, windowIdx, expectedRoot)
	if err != nil {
		return DiffHiddenRules{Rules: []string{}}, err
	}
	rules, err := diffHiddenStore().Add(repo, rule)
	return DiffHiddenRules{Repo: repo, Rules: rules}, err
}

// RemoveDiffHiddenRules shows again what the given rules hid.
func (a *App) RemoveDiffHiddenRules(id string, windowIdx int, expectedRoot string, rules []string) (DiffHiddenRules, error) {
	repo, err := a.diffHiddenRepo(id, windowIdx, expectedRoot)
	if err != nil {
		return DiffHiddenRules{Rules: []string{}}, err
	}
	left, err := diffHiddenStore().Remove(repo, rules)
	return DiffHiddenRules{Repo: repo, Rules: left}, err
}
