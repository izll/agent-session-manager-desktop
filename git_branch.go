package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// The UI asks for this on every session/tab switch, on window focus and on
	// a slow poll, so the git calls must be short and the answers reusable for
	// a moment. The poll deliberately runs less often than the cache lives, so
	// each poll reads git afresh while a burst of switches and focus events
	// shares one answer.
	gitBranchTimeout  = 2 * time.Second
	gitBranchCacheTTL = 5 * time.Second
)

// gitUnpushedTimeout is the unpushed count's own budget. The count walks
// history and is the one query here whose cost grows with the repository, so
// it runs after the branch and upstream queries and cannot use up their time:
// a slow count costs the badge its number, never its branch name. A variable
// only so a test can shorten it.
var gitUnpushedTimeout = gitBranchTimeout

// countUnpushedFunc is countUnpushed, replaceable so a test can make the count
// slow without needing a slow repository.
var countUnpushedFunc = countUnpushed

// GitBranchInfo is the branch snapshot the UI shows next to a session.
// Repository is false for paths that aren't a work tree at all; the frontend
// then renders nothing.
type GitBranchInfo struct {
	Path       string `json:"path"`
	Repository bool   `json:"repository"`
	Branch     string `json:"branch"`
	Upstream   string `json:"upstream"`
	Behind     int    `json:"behind"`
	// Unpushed counts the commits on no remote branch, including on a branch
	// that has never been pushed and so has no upstream to be ahead of.
	// Meaningful only when UnpushedKnown is set: without a remote, when the
	// remote-tracking branches cannot be trusted to mirror the server, or when
	// the count ran out of time, there is no number worth showing.
	Unpushed      int  `json:"unpushed"`
	UnpushedKnown bool `json:"unpushedKnown"`
}

type gitBranchCacheEntry struct {
	info      GitBranchInfo
	expiresAt time.Time
}

var (
	gitBranchMu    sync.Mutex
	gitBranchCache = map[string]gitBranchCacheEntry{}
)

// GetGitBranch returns the branch of the selected session/tab root. The
// expected canonical root is mandatory: accepting a raw webview path here
// would let this API bypass the file browser's project/root boundary.
func (a *App) GetGitBranch(sessionID string, windowIdx int, expectedRoot string) (GitBranchInfo, error) {
	root, err := a.gitRootSnapshot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return GitBranchInfo{}, err
	}
	return a.getGitBranchAtPath(root), nil
}

func (a *App) gitRootSnapshot(sessionID string, windowIdx int, expectedRoot string) (string, error) {
	inst, err := a.browseInstance(sessionID, windowIdx)
	if err != nil {
		return "", err
	}
	return validateRootSnapshot(inst, expectedRoot, "the tab working directory changed; reopen Git history")
}

// getGitBranchAtPath returns the branch of the given working directory, cached for a
// few seconds. Non-repositories answer Repository=false and are cached too, so
// a session outside git costs one git call per TTL rather than one per render.
func (a *App) getGitBranchAtPath(path string) GitBranchInfo {
	normalized := normalizedDashboardPath(path)
	if normalized == "" {
		return GitBranchInfo{Path: path}
	}

	now := time.Now()
	gitBranchMu.Lock()
	if entry, ok := gitBranchCache[normalized]; ok && now.Before(entry.expiresAt) {
		gitBranchMu.Unlock()
		entry.info.Path = path
		return entry.info
	}
	gitBranchMu.Unlock()

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	info, complete := readGitBranch(parent, normalized)

	// A timed-out or cancelled branch lookup is not an answer: caching it would
	// keep the branch hidden for the whole TTL after a transient stall. A count
	// that ran out of its own time is different. The branch beside it is right,
	// so the entry is cached with the count unknown, and the count is tried
	// again when the entry expires rather than on every request in between.
	if complete {
		gitBranchMu.Lock()
		gitBranchCache[normalized] = gitBranchCacheEntry{
			info:      info,
			expiresAt: now.Add(gitBranchCacheTTL),
		}
		gitBranchMu.Unlock()
	}

	info.Path = path
	return info
}

// readGitBranch runs the branch/upstream queries only — the dashboard's full
// inspection also walks the work tree status, which is far too slow to repeat
// on every tab switch. complete is false when the branch queries did not
// finish; the unpushed count running out of its own time does not make it so.
func readGitBranch(parent context.Context, path string) (info GitBranchInfo, complete bool) {
	ctx, cancel := context.WithTimeout(parent, gitBranchTimeout)
	defer cancel()
	info = readGitBranchName(ctx, path)
	if ctx.Err() != nil {
		return info, false
	}
	if !info.Repository {
		return info, true
	}

	countCtx, countCancel := context.WithTimeout(parent, gitUnpushedTimeout)
	defer countCancel()
	info.Unpushed, info.UnpushedKnown = countUnpushedFunc(countCtx, path, "")
	return info, parent.Err() == nil
}

// readGitBranchName fills in everything but the unpushed count.
func readGitBranchName(ctx context.Context, path string) GitBranchInfo {
	info := GitBranchInfo{Path: path}

	output, err := runDashboardGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		return info
	}
	info.Repository = true

	if output, err = runDashboardGit(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(output)
	} else if output, err = runDashboardGit(ctx, path, "rev-parse", "--short", "HEAD"); err == nil {
		info.Branch = "detached@" + strings.TrimSpace(output)
	}

	if output, err = runDashboardGit(ctx, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		info.Upstream = strings.TrimSpace(output)
		// Only behind: what is ahead is the unpushed count, which also covers
		// a branch with no upstream to be ahead of.
		if count, countErr := runGitStdout(ctx, path, "rev-list", "--count", "HEAD..@{upstream}"); countErr == nil {
			info.Behind, _ = strconv.Atoi(strings.TrimSpace(count))
		}
	}

	return info
}
