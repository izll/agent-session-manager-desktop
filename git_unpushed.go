package main

import (
	"context"
	"strconv"
	"strings"

	"asmgr-desktop/session"
)

// Which commits have not been pushed.
//
// "Unpushed" means on no remote-tracking branch at all, rather than ahead of
// the branch's upstream. The two agree whenever there is an upstream, but only
// the first answers for a branch that has never been pushed: it has no
// upstream, so ahead-of-upstream said nothing — on exactly the branch where
// every commit is still only on this machine.
//
// Counting against the remote-tracking branches is only right, though, when
// they actually mirror what the server holds, and often they do not: a
// shallow or single-branch clone fetches one branch, a checkout of somebody's
// pull request tracks a ref no refspec maps, a push to a URL updates nothing
// locally. There every commit the server already has would stay flagged
// "not pushed" forever, however often the user fetched. A missing mark is a
// much smaller harm than a wrong one, so the count is only claimed when it can
// be trusted (see unpushedIsKnown), and is "unknown" — no badge — otherwise.
//
// A repository with no remote says nothing either. Every one of its commits is
// "on no remote", and flagging all of them would be noise, not news.

// maxUnpushedListed bounds how many unpushed commits one history page marks.
// Far more than anyone keeps unpushed; the count itself is not bounded. A
// variable rather than a constant only so a test can reach the bound without
// making thousands of commits.
var maxUnpushedListed = 5000

// runGitStdout runs git and returns its standard output alone. The dashboard's
// runner merges stderr in, which is fine for text shown to a person but not for
// output parsed as data: a warning such as "refname 'x' is ambiguous" would be
// read as a commit hash, or break a number.
func runGitStdout(ctx context.Context, path string, args ...string) (string, error) {
	cmd := session.GitCommandContext(ctx, append([]string{"-C", path}, args...)...)
	var stdout boundedProjectGitOutput
	cmd.Stdout = &stdout
	// Left nil, stderr goes to the null device.
	cmd.Stderr = nil
	err := cmd.Run()
	if stdout.truncated {
		return "", errProjectGitOutputLimit
	}
	return string(stdout.data), err
}

// gitRemoteNames lists the configured remotes.
func gitRemoteNames(ctx context.Context, path string) []string {
	output, err := runGitStdout(ctx, path, "remote")
	if err != nil {
		return nil
	}
	return strings.Fields(output)
}

// unpushedIsKnown decides whether "on no remote-tracking branch" can be
// trusted to mean "not on the server" for rev ("HEAD", or a local branch name
// already validated by the caller).
//
//   - No remote: nothing to push to, so nothing is claimed.
//   - The branch's upstream is a remote-tracking branch: trusted. The upstream
//     is among the refs counted against, so the answer is never more than
//     "ahead of upstream" — git's own notion of unpushed.
//   - The branch is configured to track a remote branch that has no
//     remote-tracking ref here (single-branch clone, pull-request checkout,
//     remote given as a URL): unknown. It has been pushed somewhere this
//     repository cannot see.
//   - Otherwise — a branch never pushed, or a detached HEAD — trusted only when
//     every remote maps all of its branches to remote-tracking refs. Then a
//     commit on none of them really is on none of the server's branches, as of
//     the last fetch; with a narrower refspec it may well be on one this clone
//     never fetched.
func unpushedIsKnown(ctx context.Context, path, rev string) bool {
	remotes := gitRemoteNames(ctx, path)
	if len(remotes) == 0 {
		return false
	}
	if rev == "" {
		rev = "HEAD"
	}

	upstream, err := runGitStdout(ctx, path, "rev-parse", "--symbolic-full-name", rev+"@{upstream}")
	if err == nil && strings.HasPrefix(strings.TrimSpace(upstream), "refs/remotes/") {
		return true
	}

	if branch := localBranchName(ctx, path, rev); branch != "" {
		remote, err := runGitStdout(ctx, path, "config", "--get", "branch."+branch+".remote")
		// "." is a local upstream: tracking another branch of this repository
		// says nothing about the server, so it is treated as no upstream.
		if remote = strings.TrimSpace(remote); err == nil && remote != "" && remote != "." {
			return false
		}
	}

	return remotesMirrorAllBranches(ctx, path, remotes)
}

// localBranchName is the branch rev names, or "" for a detached HEAD. The full
// ref is read rather than --short, which answers "heads/x" when a tag shares
// the branch's name.
func localBranchName(ctx context.Context, path, rev string) string {
	if rev != "HEAD" {
		return rev
	}
	output, err := runGitStdout(ctx, path, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(output), "refs/heads/")
}

// remotesMirrorAllBranches reports whether every remote fetches all of its
// branches into remote-tracking refs — the refspec a normal clone writes, and
// the one a --depth, --single-branch or tag clone does not.
func remotesMirrorAllBranches(ctx context.Context, path string, remotes []string) bool {
	output, err := runGitStdout(ctx, path, "config", "--get-regexp", `^remote\..*\.fetch$`)
	if err != nil {
		return false
	}
	mirrored := map[string]bool{}
	excluded := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		key, spec, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found {
			continue
		}
		// Remote names may contain dots, so the name is what lies between the
		// fixed prefix and suffix rather than the second field.
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".fetch")
		spec = strings.TrimSpace(spec)
		if strings.HasPrefix(spec, "^") {
			// A negative refspec leaves some branch unfetched.
			excluded[name] = true
			continue
		}
		src, dst, ok := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
		if ok && src == "refs/heads/*" && strings.HasPrefix(dst, "refs/remotes/") && strings.HasSuffix(dst, "*") {
			mirrored[name] = true
		}
	}
	for _, remote := range remotes {
		if !mirrored[remote] || excluded[remote] {
			return false
		}
	}
	return true
}

// countUnpushed counts the commits on rev that are on no remote branch.
// ok is false when the answer means nothing: see unpushedIsKnown, or git
// failed.
func countUnpushed(ctx context.Context, path, rev string) (count int, ok bool) {
	if !unpushedIsKnown(ctx, path, rev) {
		return 0, false
	}
	return countUnpushedKnown(ctx, path, rev)
}

// countUnpushedKnown is countUnpushed for a caller that has already decided
// the answer can be trusted.
func countUnpushedKnown(ctx context.Context, path, rev string) (int, bool) {
	if rev == "" {
		rev = "HEAD"
	}
	output, err := runGitStdout(ctx, path, "rev-list", "--count",
		"--not", "--remotes", "--not", "--end-of-options", rev, "--")
	if err != nil {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, false
	}
	return count, true
}

// unpushedCommitSet lists the commits reachable from rev that are on no remote
// branch, at most maxUnpushedListed of them. rev is "" for HEAD, or a branch
// name already validated by the caller, whose unpushedIsKnown must be true.
func unpushedCommitSet(ctx context.Context, path, rev string) map[string]bool {
	if rev == "" {
		rev = "HEAD"
	}
	// The second --not turns the rev back to a positive one, so it can come
	// last, after --end-of-options: a branch name is untrusted input, and one
	// such as "--output=..." must be read as a revision, never as an option.
	output, err := runGitStdout(ctx, path, "rev-list",
		"--max-count="+strconv.Itoa(maxUnpushedListed),
		"--not", "--remotes", "--not", "--end-of-options", rev, "--")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, hash := range strings.Fields(output) {
		// Belt and braces on top of reading stdout only: anything that is not
		// a whole object id is not a commit, whatever git printed it for.
		if isFullObjectID(hash) {
			set[hash] = true
		}
	}
	return set
}

// isFullObjectID reports whether s is a full SHA-1 or SHA-256 object id.
func isFullObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
