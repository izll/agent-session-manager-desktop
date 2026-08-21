package main

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	// Listing is one git call, but it is triggered by a click, so it may not
	// keep the UI waiting any longer than the badge lookup does.
	gitBranchListTimeout  = 2 * time.Second
	gitBranchListCacheTTL = 5 * time.Second

	// A repo with thousands of branches would produce an unusable menu and a
	// large payload; the list is a "what else is here" glance, not an index.
	gitBranchListLimit = 100
)

// GitBranchEntry is one local branch in the dropdown.
type GitBranchEntry struct {
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	Committed string `json:"committed"`
	Current   bool   `json:"current"`
}

// GitBranchList is the read-only branch listing behind the badge dropdown.
// Repository is false for paths that aren't a work tree; Truncated says the
// repo has more branches than Total reports entries, so the UI can admit it
// rather than pretending the list is complete.
type GitBranchList struct {
	Path       string           `json:"path"`
	Repository bool             `json:"repository"`
	Branches   []GitBranchEntry `json:"branches"`
	Total      int              `json:"total"`
	Truncated  bool             `json:"truncated"`
}

type gitBranchListCacheEntry struct {
	list      GitBranchList
	expiresAt time.Time
}

var (
	gitBranchListMu    sync.Mutex
	gitBranchListCache = map[string]gitBranchListCacheEntry{}
)

// ListGitBranches returns branches only for a validated session/tab snapshot.
func (a *App) ListGitBranches(sessionID string, windowIdx int, expectedRoot string) (GitBranchList, error) {
	root, err := a.gitRootSnapshot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return GitBranchList{}, err
	}
	return a.listGitBranchesAtPath(root), nil
}

// listGitBranchesAtPath returns the local branches of the given working directory,
// most recently committed first, cached for a few seconds. The dropdown may be
// opened and closed repeatedly, and each open would otherwise fork a git
// process. Non-repositories answer Repository=false and are cached too.
func (a *App) listGitBranchesAtPath(path string) GitBranchList {
	normalized := normalizedDashboardPath(path)
	if normalized == "" {
		return GitBranchList{Path: path, Branches: []GitBranchEntry{}}
	}

	now := time.Now()
	gitBranchListMu.Lock()
	if entry, ok := gitBranchListCache[normalized]; ok && now.Before(entry.expiresAt) {
		gitBranchListMu.Unlock()
		entry.list.Path = path
		return entry.list
	}
	gitBranchListMu.Unlock()

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, gitBranchListTimeout)
	defer cancel()

	list := readGitBranchList(ctx, normalized, gitBranchListLimit)

	// A timed-out or cancelled lookup is not an answer: caching it would keep
	// the dropdown empty for the whole TTL after a transient stall.
	if ctx.Err() == nil {
		gitBranchListMu.Lock()
		gitBranchListCache[normalized] = gitBranchListCacheEntry{
			list:      list,
			expiresAt: now.Add(gitBranchListCacheTTL),
		}
		gitBranchListMu.Unlock()
	}

	list.Path = path
	return list
}

// readGitBranchList reads local branches via for-each-ref. `git branch` output
// is porcelain — its current-branch marker and decorations are meant for humans
// and shift with configuration — while for-each-ref takes an explicit format and
// does the recency sort inside git, so no ref dates have to be parsed here.
func readGitBranchList(ctx context.Context, path string, limit int) GitBranchList {
	list := GitBranchList{Path: path, Branches: []GitBranchEntry{}}

	output, err := runDashboardGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		return list
	}
	list.Repository = true

	// HEAD marks the current branch: %(HEAD) is "*" for it and " " otherwise.
	const refFormat = "%(HEAD)%00%(refname:short)%00%(objectname:short)%00%(committerdate:iso-strict)"
	output, err = runDashboardGit(ctx, path,
		"for-each-ref",
		"--sort=-committerdate",
		"--format="+refFormat,
		"refs/heads/",
	)
	if err != nil {
		return list
	}

	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 4)
		if len(fields) != 4 || fields[1] == "" {
			continue
		}
		list.Total++
		// Counting continues past the limit so Total stays honest about how
		// many branches the repo really has.
		if limit > 0 && len(list.Branches) >= limit {
			list.Truncated = true
			continue
		}
		list.Branches = append(list.Branches, GitBranchEntry{
			Name:      fields[1],
			Hash:      fields[2],
			Committed: fields[3],
			Current:   strings.TrimSpace(fields[0]) == "*",
		})
	}

	return list
}
