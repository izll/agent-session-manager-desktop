package main

import (
	"context"
	"strconv"
	"strings"
)

// Which commits have not been pushed.
//
// "Unpushed" means on no remote-tracking branch at all, rather than ahead of
// the branch's upstream. The two agree whenever there is an upstream, but only
// the first answers for a branch that has never been pushed: it has no
// upstream, so ahead-of-upstream said nothing — on exactly the branch where
// every commit is still only on this machine.
//
// A repository with no remote says nothing either. Every one of its commits is
// "on no remote", and flagging all of them would be noise, not news.

// maxUnpushedListed bounds how many unpushed commits one history page marks.
// Far more than anyone keeps unpushed; the count itself is not bounded.
const maxUnpushedListed = 5000

// hasGitRemote reports whether the repository has any remote configured.
func hasGitRemote(ctx context.Context, path string) bool {
	output, err := runDashboardGit(ctx, path, "remote")
	return err == nil && strings.TrimSpace(output) != ""
}

// countUnpushed counts the commits on HEAD that are on no remote branch.
// ok is false when the answer means nothing: no remote, or git failed.
func countUnpushed(ctx context.Context, path string) (count int, ok bool) {
	if !hasGitRemote(ctx, path) {
		return 0, false
	}
	output, err := runDashboardGit(ctx, path, "rev-list", "--count", "HEAD", "--not", "--remotes")
	if err != nil {
		return 0, false
	}
	count, err = strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, false
	}
	return count, true
}

// unpushedCommitSet lists the commits reachable from rev that are on no remote
// branch. rev is "" for HEAD, or a branch name already validated by the caller.
// Nil when there is no remote to compare against.
func unpushedCommitSet(ctx context.Context, path, rev string) map[string]bool {
	if !hasGitRemote(ctx, path) {
		return nil
	}
	if rev == "" {
		rev = "HEAD"
	}
	// The second --not turns the rev back to a positive one, so it can come
	// last, after --end-of-options: a branch name is untrusted input, and one
	// such as "--output=..." must be read as a revision, never as an option.
	output, err := runDashboardGit(ctx, path, "rev-list",
		"--max-count="+strconv.Itoa(maxUnpushedListed),
		"--not", "--remotes", "--not", "--end-of-options", rev, "--")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, hash := range strings.Fields(output) {
		set[hash] = true
	}
	return set
}
