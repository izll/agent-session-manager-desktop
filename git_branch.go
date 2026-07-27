package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// The UI asks for this on every session/tab switch and on window focus, so
	// the git calls must be short and the answers reusable for a moment.
	gitBranchTimeout  = 2 * time.Second
	gitBranchCacheTTL = 5 * time.Second
)

// GitBranchInfo is the branch snapshot the UI shows next to a session.
// Repository is false for paths that aren't a work tree at all; the frontend
// then renders nothing.
type GitBranchInfo struct {
	Path       string `json:"path"`
	Repository bool   `json:"repository"`
	Branch     string `json:"branch"`
	Upstream   string `json:"upstream"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
}

type gitBranchCacheEntry struct {
	info      GitBranchInfo
	expiresAt time.Time
}

var (
	gitBranchMu    sync.Mutex
	gitBranchCache = map[string]gitBranchCacheEntry{}
)

// GetGitBranch returns the branch of the given working directory, cached for a
// few seconds. Non-repositories answer Repository=false and are cached too, so
// a session outside git costs one git call per TTL rather than one per render.
func (a *App) GetGitBranch(path string) GitBranchInfo {
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
	ctx, cancel := context.WithTimeout(parent, gitBranchTimeout)
	defer cancel()

	info := readGitBranch(ctx, normalized)

	// A timed-out or cancelled lookup is not an answer: caching it would keep
	// the branch hidden for the whole TTL after a transient stall.
	if ctx.Err() == nil {
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
// on every tab switch.
func readGitBranch(ctx context.Context, path string) GitBranchInfo {
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
		if counts, countErr := runDashboardGit(ctx, path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); countErr == nil {
			fields := strings.Fields(counts)
			if len(fields) == 2 {
				info.Ahead, _ = strconv.Atoi(fields[0])
				info.Behind, _ = strconv.Atoi(fields[1])
			}
		}
	}

	return info
}
