package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session"
)

// Pushing and pulling from the branch badge.
//
// The badge counts what is not pushed and what has not been pulled; these
// make the counts something to act on. Both are deliberately narrow:
//
//   - A push sends this branch, and only this branch, to where git itself
//     would push it — never a force, never every matching branch because of a
//     push.default the user set for the command line.
//   - A pull only fast-forwards. A merge or a rebase started from a button
//     can leave the work tree mid-conflict with an agent working in it, and
//     that is not a state to put anyone in behind their back.
//
// Both go over the network, and both are run so that they can never wait for
// an answer nobody can give: see networkGitCommand.

// gitSyncTimeout bounds one push or pull. A push of a large history over a
// slow link can legitimately take a while, but one that has not finished in
// two minutes is stuck — on a server that stopped answering, or on a prompt
// that slipped past the suppression. A variable only so a test can shorten it.
var gitSyncTimeout = 2 * time.Minute

// gitSyncListLimit bounds the commits a preview lists. The count is not
// bounded; the panel says how many more there are.
const gitSyncListLimit = 200

// gitSyncMessageLimit keeps git's output readable in a small panel. The end is
// kept rather than the start: git puts the reason for a failure last.
const gitSyncMessageLimit = 4000

// Outcomes a push or pull reports. They are keys rather than sentences so the
// interface can explain each in the user's language; git's own words travel
// alongside in Message.
const (
	gitSyncPushed       = "pushed"
	gitSyncPulled       = "pulled"
	gitSyncUpToDate     = "upToDate"
	gitSyncRejected     = "rejected"
	gitSyncDiverged     = "diverged"
	gitSyncLocalChanges = "localChanges"
	gitSyncAuth         = "auth"
	gitSyncTimedOut     = "timeout"
	gitSyncCancelled    = "cancelled"
	gitSyncChanged      = "changed"
	gitSyncFailed       = "failed"
)

// GitSyncCommit is one commit a push would send or a pull would bring in.
type GitSyncCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"shortHash"`
	Subject   string `json:"subject"`
}

// GitSyncPreview is what a push or pull would do, shown before it is done.
type GitSyncPreview struct {
	Direction string `json:"direction"`
	Branch    string `json:"branch"`
	// Head is the commit the preview was taken at. A push hands it back, so
	// what is sent is what was shown: an agent committing between the preview
	// and the click must not have its commit published unseen.
	Head string `json:"head"`
	// Target is where the push goes ("origin/main"), or the upstream a pull
	// reads from. Empty for a push that must first choose where to go.
	Target string `json:"target"`
	// SetUpstream says the branch has nowhere to push to yet, so the push
	// will create the remote branch and track it.
	SetUpstream bool `json:"setUpstream"`
	// Remotes are the candidates for SetUpstream, and Remote the one picked
	// for the user — empty when the choice is not obvious and theirs to make.
	Remotes []string `json:"remotes"`
	Remote  string   `json:"remote"`
	// Diverged says the branch has commits the upstream lacks, so a pull
	// cannot fast-forward and is not offered.
	Diverged  bool            `json:"diverged"`
	Commits   []GitSyncCommit `json:"commits"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
}

// GitSyncResult reports a push or pull that ran. Failures of the operation
// itself — rejected, diverged, no credentials — are results, not errors: the
// panel explains each one differently, and Wails flattens an error to a
// string it could only print.
type GitSyncResult struct {
	OK      bool   `json:"ok"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
}

var (
	// One push or pull at a time. Two at once against the same repository
	// fight over its lock files, and the panel shows one progress anyway.
	gitSyncMu     sync.Mutex
	gitSyncCancel context.CancelFunc
)

// errGitSyncBusy is reported when a push or pull is already running.
var errGitSyncBusy = errors.New("error.gitSyncBusy")

// GetGitSyncPreview lists what a push ("push") or pull ("pull") of the tab's
// branch would move, without moving anything.
func (a *App) GetGitSyncPreview(sessionID string, windowIdx int, expectedRoot, direction string) (GitSyncPreview, error) {
	root, err := a.gitSyncRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return GitSyncPreview{}, err
	}
	ctx, cancel := context.WithTimeout(a.gitSyncParent(), gitHistoryTimeout)
	defer cancel()
	switch direction {
	case "push":
		return gitPushPreview(ctx, root)
	case "pull":
		return gitPullPreview(ctx, root)
	}
	return GitSyncPreview{}, fmt.Errorf("unknown direction %q", direction)
}

// GitPush pushes the tab's branch. expectedHead and expectedTarget are the
// preview's Head and Target; remote is only read when the preview asked for
// an upstream to be set.
func (a *App) GitPush(sessionID string, windowIdx int, expectedRoot, expectedProjectID, expectedHead, expectedTarget, remote string) (GitSyncResult, error) {
	root, err := a.gitSyncRootLocked(sessionID, windowIdx, expectedRoot, expectedProjectID)
	if err != nil {
		return GitSyncResult{}, err
	}
	ctx, finish, err := a.beginGitSync()
	if err != nil {
		return GitSyncResult{}, err
	}
	defer finish()
	result, err := gitPushAtPath(ctx, root, expectedHead, expectedTarget, remote)
	forgetGitBranch(root)
	return result, err
}

// GitPull fast-forwards the tab's branch to its upstream. expectedBranch is
// the preview's Branch: a pull lands in the work tree, and must land on the
// branch the user looked at.
func (a *App) GitPull(sessionID string, windowIdx int, expectedRoot, expectedProjectID, expectedBranch string) (GitSyncResult, error) {
	root, err := a.gitSyncRootLocked(sessionID, windowIdx, expectedRoot, expectedProjectID)
	if err != nil {
		return GitSyncResult{}, err
	}
	ctx, finish, err := a.beginGitSync()
	if err != nil {
		return GitSyncResult{}, err
	}
	defer finish()
	result, err := gitPullAtPath(ctx, root, expectedBranch)
	forgetGitBranch(root)
	return result, err
}

// CancelGitSync stops a push or pull in progress. Stopping a push midway is
// safe: the server either took the update or it did not.
func (a *App) CancelGitSync() {
	gitSyncMu.Lock()
	defer gitSyncMu.Unlock()
	if gitSyncCancel != nil {
		gitSyncCancel()
	}
}

// gitSyncRoot is the badge's own root check, plus the one thing the badge
// does not need: that the tab runs on this computer. The badge reads a
// server tab's configured directory on this machine, where it may happen to
// exist; pushing from that would publish a different checkout from the one
// the tab works in.
func (a *App) gitSyncRoot(sessionID string, windowIdx int, expectedRoot string) (string, error) {
	inst, err := a.browseInstance(sessionID, windowIdx)
	if err != nil {
		return "", err
	}
	if inst.ServerForWindow(windowIdx) != "" {
		return "", errors.New("error.gitSyncRemoteTab")
	}
	return validateRootSnapshot(inst, expectedRoot, "the tab working directory changed; open the panel again")
}

// tabOnServer reports whether a tab's commands run on a server rather than
// here. A session that cannot be read counts as on a server: the answer only
// ever hides an action.
func (a *App) tabOnServer(sessionID string, windowIdx int) bool {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return true
	}
	return inst.ServerForWindow(windowIdx) != ""
}

// gitSyncRootLocked checks the root under the project lock and releases it
// again before anything goes over the network. The lock is what says this
// instance owns the project and that the session still belongs to it — a
// pull writes into the work tree like a file save does — but it is exclusive,
// and holding it for a two-minute push would freeze every other action.
func (a *App) gitSyncRootLocked(sessionID string, windowIdx int, expectedRoot, expectedProjectID string) (string, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return "", err
	}
	defer done()
	return a.gitSyncRoot(sessionID, windowIdx, expectedRoot)
}

func (a *App) gitSyncParent() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// beginGitSync claims the single push/pull slot with a timed, cancellable
// context. The app's own context is the parent, so quitting stops it too.
func (a *App) beginGitSync() (context.Context, func(), error) {
	gitSyncMu.Lock()
	defer gitSyncMu.Unlock()
	if gitSyncCancel != nil {
		return nil, nil, errGitSyncBusy
	}
	ctx, cancel := context.WithTimeout(a.gitSyncParent(), gitSyncTimeout)
	gitSyncCancel = cancel
	return ctx, func() {
		gitSyncMu.Lock()
		gitSyncCancel = nil
		gitSyncMu.Unlock()
		cancel()
	}, nil
}

// forgetGitBranch drops the badge's cached answer for a path, so the refresh
// after a push or pull shows the new counts rather than the old ones for
// another few seconds.
func forgetGitBranch(path string) {
	gitBranchMu.Lock()
	delete(gitBranchCache, normalizedDashboardPath(path))
	gitBranchMu.Unlock()
}

// ---------------------------------------------------------------------------
// Push

// gitPushDestination is where a push of branch goes: the remote and the
// remote-tracking ref git itself resolves @{push} to, so pushRemote,
// remote.pushDefault and push.default are all honoured.
//
// ok is false when git has no destination for it. That is a branch never
// pushed, and also one tracking a differently named branch — "git switch -c
// feature origin/main" — where push.default=simple refuses on purpose:
// pushing feature onto main is exactly the accident it exists to prevent. Both
// get "push and set upstream" to a branch of their own name instead.
func gitPushDestination(ctx context.Context, path, branch string, remotes []string) (remote, remoteBranch string, ok bool) {
	// The bare name: git reads "refs/heads/x@{push}" as a branch called
	// "refs/heads/x". A branch name cannot begin with "-", so it cannot be
	// taken for an option either.
	output, err := runGitStdout(ctx, path, "rev-parse", "--symbolic-full-name", branch+"@{push}")
	if err != nil {
		return "", "", false
	}
	ref := strings.TrimPrefix(strings.TrimSpace(output), "refs/remotes/")
	if ref == strings.TrimSpace(output) {
		// A local "upstream" is not a push anywhere.
		return "", "", false
	}
	// Remote names may contain slashes, so the remote is whichever configured
	// one the ref starts with — the longest, should one name prefix another.
	for _, name := range remotes {
		if strings.HasPrefix(ref, name+"/") && len(name) > len(remote) {
			remote = name
		}
	}
	if remote == "" {
		return "", "", false
	}
	return remote, strings.TrimPrefix(ref, remote+"/"), true
}

// suggestedPushRemote picks where a branch with no push destination should
// go: where the user's configuration says pushes go, else the branch's own
// remote, else "origin", else the only remote. With several remotes and none
// of those, it is the user's choice and "" is returned.
func suggestedPushRemote(ctx context.Context, path, branch string, remotes []string) string {
	known := map[string]bool{}
	for _, name := range remotes {
		known[name] = true
	}
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault", "branch." + branch + ".remote"} {
		value, err := runGitStdout(ctx, path, "config", "--get", key)
		if value = strings.TrimSpace(value); err == nil && known[value] {
			return value
		}
	}
	if known["origin"] {
		return "origin"
	}
	if len(remotes) == 1 {
		return remotes[0]
	}
	return ""
}

func gitPushPreview(ctx context.Context, path string) (GitSyncPreview, error) {
	preview := GitSyncPreview{Direction: "push", Remotes: []string{}, Commits: []GitSyncCommit{}}
	branch := localBranchName(ctx, path, "HEAD")
	if branch == "" {
		return preview, errors.New("error.gitSyncDetached")
	}
	preview.Branch = branch
	head, err := runGitStdout(ctx, path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return preview, fmt.Errorf("could not read HEAD: %w", err)
	}
	preview.Head = strings.TrimSpace(head)

	// The same rule as the badge: without a trustworthy count there is
	// nothing honest to list, and the badge offers no push either.
	if !unpushedIsKnown(ctx, path, branch) {
		return preview, errors.New("error.gitSyncUnknown")
	}
	remotes := gitRemoteNames(ctx, path)
	preview.Remotes = remotes
	if remote, remoteBranch, ok := gitPushDestination(ctx, path, branch, remotes); ok {
		preview.Remote = remote
		preview.Target = remote + "/" + remoteBranch
	} else {
		preview.SetUpstream = true
		preview.Remote = suggestedPushRemote(ctx, path, branch, remotes)
	}

	commits, err := gitSyncCommits(ctx, path, "--not", "--remotes", "--not", preview.Head)
	if err != nil {
		return preview, err
	}
	preview.Commits = commits
	preview.Total = len(commits)
	if count, ok := countUnpushedKnown(ctx, path, preview.Head); ok {
		preview.Total = count
	}
	preview.Truncated = preview.Total > len(commits)
	return preview, nil
}

func gitPushAtPath(ctx context.Context, path, expectedHead, expectedTarget, remote string) (GitSyncResult, error) {
	branch := localBranchName(ctx, path, "HEAD")
	if branch == "" {
		return GitSyncResult{}, errors.New("error.gitSyncDetached")
	}
	head, err := runGitStdout(ctx, path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return GitSyncResult{}, fmt.Errorf("could not read HEAD: %w", err)
	}
	remotes := gitRemoteNames(ctx, path)
	destRemote, destBranch, hasDestination := gitPushDestination(ctx, path, branch, remotes)
	target := ""
	if hasDestination {
		target = destRemote + "/" + destBranch
	}
	// What would be pushed, or where, is no longer what the user approved.
	if strings.TrimSpace(head) != expectedHead || target != expectedTarget {
		return GitSyncResult{Outcome: gitSyncChanged}, nil
	}

	// The refspec is spelled out in full: this branch to that one, no "+", so
	// no configuration — push.default=matching, a remote.*.push refspec,
	// --force in an alias — can widen it into a force or into every branch.
	args := []string{"push", "--porcelain"}
	if hasDestination {
		args = append(args, "--end-of-options", destRemote, "refs/heads/"+branch+":refs/heads/"+destBranch)
	} else {
		if !containsString(remotes, remote) {
			return GitSyncResult{}, errors.New("error.gitSyncUnknownRemote")
		}
		args = append(args, "--set-upstream", "--end-of-options", remote, "refs/heads/"+branch+":refs/heads/"+branch)
	}

	output, runErr := runNetworkGit(ctx, path, args...)
	if outcome, failed := gitSyncFailure(ctx, runErr); failed {
		return GitSyncResult{Outcome: outcome, Message: gitSyncMessage(output)}, nil
	}
	if runErr != nil {
		outcome := gitSyncFailed
		switch {
		// --porcelain marks a refused ref "!" with the reason git gives; a
		// non-fast-forward is the one a pull fixes, so it gets its own advice.
		// "[remote rejected]" — a protected branch, a hook — is not that.
		case strings.Contains(output, "[rejected]") &&
			(strings.Contains(output, "non-fast-forward") || strings.Contains(output, "fetch first")):
			outcome = gitSyncRejected
		case gitSyncAuthFailed(output):
			outcome = gitSyncAuth
		}
		return GitSyncResult{Outcome: outcome, Message: gitSyncMessage(output)}, nil
	}
	if strings.Contains(output, "[up to date]") {
		return GitSyncResult{OK: true, Outcome: gitSyncUpToDate, Message: gitSyncMessage(output)}, nil
	}
	return GitSyncResult{OK: true, Outcome: gitSyncPushed, Message: gitSyncMessage(output)}, nil
}

// ---------------------------------------------------------------------------
// Pull

func gitPullPreview(ctx context.Context, path string) (GitSyncPreview, error) {
	preview := GitSyncPreview{Direction: "pull", Remotes: []string{}, Commits: []GitSyncCommit{}}
	branch := localBranchName(ctx, path, "HEAD")
	if branch == "" {
		return preview, errors.New("error.gitSyncDetached")
	}
	preview.Branch = branch
	if head, err := runGitStdout(ctx, path, "rev-parse", "--verify", "HEAD"); err == nil {
		preview.Head = strings.TrimSpace(head)
	}
	upstream, err := runGitStdout(ctx, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return preview, errors.New("error.gitSyncNoUpstream")
	}
	preview.Target = strings.TrimSpace(upstream)

	// As of the last fetch. The pull fetches again and may bring more; what
	// is listed is what the badge counted.
	commits, err := gitSyncCommits(ctx, path, "HEAD..@{upstream}")
	if err != nil {
		return preview, err
	}
	preview.Commits = commits
	preview.Total = gitRevCount(ctx, path, "HEAD..@{upstream}", len(commits))
	preview.Truncated = preview.Total > len(commits)
	// Commits only here mean the two histories have forked: no fast-forward
	// is possible, and the other ways to join them are not this button's.
	preview.Diverged = gitRevCount(ctx, path, "@{upstream}..HEAD", 0) > 0
	return preview, nil
}

func gitPullAtPath(ctx context.Context, path, expectedBranch string) (GitSyncResult, error) {
	if branch := localBranchName(ctx, path, "HEAD"); branch == "" || branch != expectedBranch {
		return GitSyncResult{Outcome: gitSyncChanged}, nil
	}
	if _, err := runGitStdout(ctx, path, "rev-parse", "--verify", "--quiet", "@{upstream}"); err != nil {
		return GitSyncResult{}, errors.New("error.gitSyncNoUpstream")
	}
	before, _ := runGitStdout(ctx, path, "rev-parse", "--verify", "HEAD")

	// --ff-only and --no-rebase on the command line outrank pull.ff and
	// pull.rebase in the user's configuration; autostash is switched off so a
	// dirty work tree is refused by git rather than stashed and replayed.
	output, runErr := runNetworkGit(ctx, path,
		"-c", "merge.autoStash=false", "-c", "rebase.autoStash=false",
		"pull", "--ff-only", "--no-rebase", "--no-edit")
	if outcome, failed := gitSyncFailure(ctx, runErr); failed {
		return GitSyncResult{Outcome: outcome, Message: gitSyncMessage(output)}, nil
	}
	if runErr != nil {
		outcome := gitSyncFailed
		switch {
		case strings.Contains(output, "would be overwritten"):
			outcome = gitSyncLocalChanges
		case strings.Contains(output, "Not possible to fast-forward") ||
			strings.Contains(output, "diverg") ||
			gitRevCount(ctx, path, "@{upstream}..HEAD", 0) > 0 && gitRevCount(ctx, path, "HEAD..@{upstream}", 0) > 0:
			outcome = gitSyncDiverged
		case gitSyncAuthFailed(output):
			outcome = gitSyncAuth
		}
		return GitSyncResult{Outcome: outcome, Message: gitSyncMessage(output)}, nil
	}
	after, _ := runGitStdout(ctx, path, "rev-parse", "--verify", "HEAD")
	if strings.TrimSpace(before) == strings.TrimSpace(after) {
		return GitSyncResult{OK: true, Outcome: gitSyncUpToDate, Message: gitSyncMessage(output)}, nil
	}
	return GitSyncResult{OK: true, Outcome: gitSyncPulled, Message: gitSyncMessage(output)}, nil
}

// ---------------------------------------------------------------------------
// Shared

// gitSyncCommits lists up to gitSyncListLimit commits of a rev-list range,
// newest first. The range is built here, never from user input.
func gitSyncCommits(ctx context.Context, path string, revs ...string) ([]GitSyncCommit, error) {
	args := append([]string{"log", "--max-count=" + strconv.Itoa(gitSyncListLimit),
		"--pretty=format:%H" + gitFieldSep + "%h" + gitFieldSep + "%s" + gitRecordSep}, revs...)
	args = append(args, "--")
	output, err := runGitStdout(ctx, path, args...)
	if err != nil {
		return nil, fmt.Errorf("could not list the commits: %w", err)
	}
	commits := []GitSyncCommit{}
	for _, record := range strings.Split(output, gitRecordSep) {
		fields := strings.Split(strings.TrimLeft(record, "\r\n"), gitFieldSep)
		if len(fields) < 3 || !isFullObjectID(strings.TrimSpace(fields[0])) {
			continue
		}
		commits = append(commits, GitSyncCommit{
			Hash:      strings.TrimSpace(fields[0]),
			ShortHash: strings.TrimSpace(fields[1]),
			Subject:   fields[2],
		})
	}
	return commits, nil
}

// gitRevCount counts a range, answering fallback when git cannot.
func gitRevCount(ctx context.Context, path, revRange string, fallback int) int {
	output, err := runGitStdout(ctx, path, "rev-list", "--count", revRange, "--")
	if err != nil {
		return fallback
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return fallback
	}
	return count
}

// networkGitCommand builds a git command that talks to a remote and must
// never wait for a person. A GUI app has no terminal for git to ask on: a
// credential or passphrase prompt would sit invisibly until the timeout, with
// the panel spinning, or — on a system where the app was started from a
// terminal — would appear in that terminal, which nobody is looking at.
//
//   - GIT_TERMINAL_PROMPT=0 stops git asking on the terminal for a user name
//     or password; GIT_ASKPASS=echo answers any prompt that would go to a
//     helper instead, so a missing credential fails at once as an
//     authentication error. Stored credentials (a credential helper, an
//     agent-held key) still work: nothing here disables those.
//   - SSH gets BatchMode, which makes it fail rather than ask for a password,
//     a passphrase or a new host key. Only when the user has not chosen their
//     own ssh command, which is theirs to keep.
//   - stdin is the null device, and on Unix the process starts a session of
//     its own, so it has no controlling terminal to open behind stdin's back.
//
// The output is read in the C locale because it is parsed for the outcome;
// the explanation the user reads comes from the outcome, in their language.
func networkGitCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := session.GitCommandContext(ctx, append([]string{"-C", path}, args...)...)
	cmd.Stdin = nil
	cmd.Env = networkGitEnv(os.Environ(), gitConfiguredSSHCommand(ctx, path))
	detachFromTerminal(cmd)
	// The context kills git, but a child it started (ssh, a credential
	// helper) can hold the output pipe open; this bounds the wait for it.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// gitConfiguredSSHCommand is core.sshCommand, if the repository sets one.
func gitConfiguredSSHCommand(ctx context.Context, path string) string {
	output, err := runGitStdout(ctx, path, "config", "--get", "core.sshCommand")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

// networkGitEnv is environ with the prompts switched off. configuredSSH is
// core.sshCommand, which GIT_SSH_COMMAND would override.
func networkGitEnv(environ []string, configuredSSH string) []string {
	overridden := map[string]bool{
		"GIT_TERMINAL_PROMPT": true, "GIT_ASKPASS": true, "SSH_ASKPASS_REQUIRE": true,
		"LC_ALL": true, "LANGUAGE": true,
	}
	env := make([]string, 0, len(environ)+6)
	userSSH := configuredSSH != ""
	for _, entry := range environ {
		name, value, _ := strings.Cut(entry, "=")
		if overridden[name] {
			continue
		}
		if (name == "GIT_SSH_COMMAND" || name == "GIT_SSH") && value != "" {
			userSSH = true
		}
		env = append(env, entry)
	}
	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
		"SSH_ASKPASS_REQUIRE=never",
		"LC_ALL=C",
		"LANGUAGE=C",
	)
	if !userSSH {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return env
}

// runNetworkGit runs a network git command and returns its combined output,
// which is what explains a failure.
func runNetworkGit(ctx context.Context, path string, args ...string) (string, error) {
	cmd := networkGitCommand(ctx, path, args...)
	var output boundedProjectGitOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return string(output.data), err
}

// gitSyncFailure classifies a run cut short by its context: the timeout, or
// the user pressing cancel (or the app quitting).
func gitSyncFailure(ctx context.Context, runErr error) (string, bool) {
	if runErr == nil {
		return "", false
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return gitSyncTimedOut, true
	case errors.Is(ctx.Err(), context.Canceled):
		return gitSyncCancelled, true
	}
	return "", false
}

// gitSyncAuthFailed recognises git and ssh saying they had no credentials to
// use — which, with the prompts switched off, is how a missing password looks.
func gitSyncAuthFailed(output string) bool {
	for _, sign := range []string{
		"Authentication failed",
		"could not read Username",
		"could not read Password",
		"terminal prompts disabled",
		"Permission denied (publickey",
		"Host key verification failed",
		"Invalid username or password",
		"HTTP Basic: Access denied",
	} {
		if strings.Contains(output, sign) {
			return true
		}
	}
	return false
}

// credentialInURL matches the user:password part of a URL. A remote written as
// https://user:token@host puts the token in git's "To ..." line, and the
// message is shown on screen and may be copied into a bug report.
var credentialInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

// gitSyncMessage is git's output made fit to show: credentials removed,
// porcelain noise dropped, trimmed, and cut from the front when too long.
func gitSyncMessage(output string) string {
	output = credentialInURL.ReplaceAllString(output, "${1}***@")
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "Done" {
			continue
		}
		lines = append(lines, strings.TrimRight(line, " \t"))
	}
	message := strings.Join(lines, "\n")
	if len(message) > gitSyncMessageLimit {
		message = "…" + message[len(message)-gitSyncMessageLimit:]
	}
	return message
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
