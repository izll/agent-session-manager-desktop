package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session"
)

const (
	// One tmux round-trip, but a wedged tmux server can block indefinitely, so
	// it gets the same short leash as the git helpers.
	tabWorkingDirTimeout = 2 * time.Second

	// The working directory changes on every `cd`, far more often than a branch
	// does, so the 5s TTL of the branch caches would leave the status bar
	// visibly behind the terminal. 1s still collapses the burst of lookups that
	// one session/tab switch produces (path, branch, badge) into a single tmux
	// call, while a user who types `cd` and glances at the bar sees the new
	// directory on the next refresh trigger rather than seconds later.
	tabWorkingDirCacheTTL = time.Second
)

type tabWorkingDirCacheEntry struct {
	path      string
	expiresAt time.Time
}

var (
	tabWorkingDirMu    sync.Mutex
	tabWorkingDirCache = map[string]tabWorkingDirCacheEntry{}
)

// GetTabWorkingDirectory returns the directory a tab's pane is really in, which
// drifts from the configured path as soon as the user types `cd`. The configured
// path is the fallback, so the answer is never empty: a stopped session has no
// pane to ask, and a pane in a since-deleted directory reports a path that would
// only produce noise if handed to git.
func (a *App) GetTabWorkingDirectory(sessionID string, windowIdx int) string {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return ""
	}

	// A followed window may have been opened in its own directory; anything
	// else — including the main window — is configured to the session path.
	// Resolved by index alone, so no extra `tmux list-windows` call is needed
	// just to learn which index the main window currently sits at.
	configured := inst.Path
	for _, fw := range inst.FollowedWindows {
		if fw.Index == windowIdx && fw.WorkDir != "" {
			configured = fw.WorkDir
			break
		}
	}

	if inst.Status != session.StatusRunning {
		return configured
	}

	target := fmt.Sprintf("%s:%d", inst.TmuxSessionName(), windowIdx)
	reported := a.cachedPaneCurrentPath(target, queryTmuxPaneCurrentPath)
	return resolveTabWorkingDir(reported, configured)
}

// paneCurrentPathQuery is the exec seam: tests substitute a stub so the cache
// and fallback behaviour can be exercised without a live tmux server.
type paneCurrentPathQuery func(ctx context.Context, target string) string

// cachedPaneCurrentPath returns tmux's raw answer for a target, reusing a recent
// one when there is one. Unvalidated on purpose — validation depends on the
// tab's configured fallback, which is not part of the cache key.
func (a *App) cachedPaneCurrentPath(target string, query paneCurrentPathQuery) string {
	now := time.Now()
	tabWorkingDirMu.Lock()
	if entry, ok := tabWorkingDirCache[target]; ok && now.Before(entry.expiresAt) {
		tabWorkingDirMu.Unlock()
		return entry.path
	}
	tabWorkingDirMu.Unlock()

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, tabWorkingDirTimeout)
	defer cancel()

	reported := query(ctx, target)

	// A timed-out or cancelled query is not an answer: caching it would pin the
	// bar to the configured path for the whole TTL after a transient stall.
	if ctx.Err() == nil {
		tabWorkingDirMu.Lock()
		tabWorkingDirCache[target] = tabWorkingDirCacheEntry{
			path:      reported,
			expiresAt: now.Add(tabWorkingDirCacheTTL),
		}
		tabWorkingDirMu.Unlock()
	}

	return reported
}

// queryTmuxPaneCurrentPath asks tmux for its own view of the pane's directory.
// tmux tracks this itself — on Linux by reading the pane process's /proc cwd —
// so there is no process tree to walk and no PID to chase here.
func queryTmuxPaneCurrentPath(ctx context.Context, target string) string {
	output, err := session.TmuxCommandContext(ctx, "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// resolveTabWorkingDir decides whether tmux's answer may be used. It is rejected
// unless it is an absolute path that still exists as a directory: a pane whose
// directory was deleted keeps reporting the old path, and `git -C` on a missing
// or relative path produces errors rather than a branch.
func resolveTabWorkingDir(reported, configured string) string {
	trimmed := strings.TrimSpace(reported)
	if trimmed == "" || !filepath.IsAbs(trimmed) {
		return configured
	}
	if info, err := os.Stat(trimmed); err != nil || !info.IsDir() {
		return configured
	}
	return trimmed
}
