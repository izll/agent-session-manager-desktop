package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// syncPair is a bare server and two clones of it: "mine", where the push or
// pull happens, and "theirs", which moves the server under it.
func syncPair(t *testing.T) (mine string, runMine func(...string) string, theirs string, runTheirs func(...string) string) {
	t.Helper()
	server := unpushedServer(t)
	mine, runMine = cloneOf(t, server)
	theirs, runTheirs = cloneOf(t, server)
	return mine, runMine, theirs, runTheirs
}

func syncCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func headOf(run func(...string) string, rev string) string {
	return strings.TrimSpace(run("rev-parse", rev))
}

// A push sends the unpushed commits to where the branch pushes, and the badge
// count drops to zero straight after — the remote-tracking ref moves with it.
func TestPushSendsTheUnpushedCommits(t *testing.T) {
	repo, run := unpushedRepo(t)
	commit(t, run, repo, "local-1")
	commit(t, run, repo, "local-2")
	ctx := syncCtx(t)

	preview, err := gitPushPreview(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(run("branch", "--show-current"))
	if preview.SetUpstream || preview.Target != "origin/"+branch {
		t.Fatalf("preview target = %q (set upstream %v), want origin/%s", preview.Target, preview.SetUpstream, branch)
	}
	if preview.Total != 2 || len(preview.Commits) != 2 || preview.Commits[0].Subject != "local-2" {
		t.Fatalf("preview lists %d of %d: %+v; want local-2, local-1", len(preview.Commits), preview.Total, preview.Commits)
	}

	result, err := gitPushAtPath(ctx, repo, preview.Head, preview.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Outcome != gitSyncPushed {
		t.Fatalf("push = %+v, want pushed", result)
	}
	if info := branchInfo(t, repo); !info.UnpushedKnown || info.Unpushed != 0 {
		t.Errorf("after the push the badge says %d unpushed (known %v)", info.Unpushed, info.UnpushedKnown)
	}
	if got := headOf(run, "origin/"+branch); got != preview.Head {
		t.Errorf("the server's branch is at %s, want %s", got, preview.Head)
	}
}

// A branch never pushed has nowhere to go: the push creates the remote branch
// under its own name and makes it the upstream.
func TestPushWithoutUpstreamSetsIt(t *testing.T) {
	repo, run := unpushedRepo(t)
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")
	ctx := syncCtx(t)

	preview, err := gitPushPreview(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SetUpstream || preview.Target != "" || preview.Remote != "origin" || preview.Total != 1 {
		t.Fatalf("preview = %+v; want set upstream to origin with one commit", preview)
	}

	result, err := gitPushAtPath(ctx, repo, preview.Head, preview.Target, preview.Remote)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("push = %+v", result)
	}
	if upstream := strings.TrimSpace(run("rev-parse", "--abbrev-ref", "feature@{upstream}")); upstream != "origin/feature" {
		t.Errorf("upstream = %q, want origin/feature", upstream)
	}
	if info := branchInfo(t, repo); info.Unpushed != 0 {
		t.Errorf("after the push the badge says %d unpushed", info.Unpushed)
	}
}

// A branch tracking a differently named one must not be pushed onto it:
// "git switch -c feature origin/main" then push would rewrite main.
func TestPushNeverTargetsADifferentlyNamedUpstream(t *testing.T) {
	repo, run := unpushedRepo(t)
	main := strings.TrimSpace(run("branch", "--show-current"))
	run("switch", "-q", "-c", "feature", "--track", "origin/"+main)
	commit(t, run, repo, "feature-1")

	preview, err := gitPushPreview(syncCtx(t), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SetUpstream || preview.Target != "" {
		t.Fatalf("preview = %+v; want a new upstream rather than origin/%s", preview, main)
	}
}

// Several remotes and no hint which one: the user picks, and a name that is
// not a remote is refused rather than handed to git.
func TestPushWithAmbiguousRemotesAsks(t *testing.T) {
	repo, run := unpushedRepo(t)
	run("remote", "rename", "origin", "alpha")
	run("remote", "add", "beta", unpushedServer(t))
	run("fetch", "-q", "beta")
	run("switch", "-q", "-c", "feature")
	commit(t, run, repo, "feature-1")
	ctx := syncCtx(t)

	preview, err := gitPushPreview(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SetUpstream || preview.Remote != "" || len(preview.Remotes) != 2 {
		t.Fatalf("preview = %+v; want the choice left to the user", preview)
	}
	if _, err := gitPushAtPath(ctx, repo, preview.Head, "", "--upload-pack=evil"); err == nil {
		t.Error("an unknown remote was accepted")
	}
	if result, err := gitPushAtPath(ctx, repo, preview.Head, "", "beta"); err != nil || !result.OK {
		t.Fatalf("push to the chosen remote = %+v, %v", result, err)
	}
}

// Somebody else pushed first: git refuses, the panel says so, and nothing is
// forced over their work.
func TestARejectedPushIsReportedAndNotForced(t *testing.T) {
	mine, runMine, theirs, runTheirs := syncPair(t)
	branch := strings.TrimSpace(runMine("branch", "--show-current"))
	theirCommit := commit(t, runTheirs, theirs, "theirs")
	runTheirs("push", "-q")
	commit(t, runMine, mine, "mine")
	ctx := syncCtx(t)

	preview, err := gitPushPreview(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	result, err := gitPushAtPath(ctx, mine, preview.Head, preview.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Outcome != gitSyncRejected {
		t.Fatalf("push = %+v, want rejected", result)
	}
	if !strings.Contains(result.Message, "rejected") {
		t.Errorf("git's reason is not passed on: %q", result.Message)
	}
	runTheirs("fetch", "-q")
	if got := headOf(runTheirs, "origin/"+branch); got != theirCommit {
		t.Errorf("the server's branch moved to %s; their commit %s was overwritten", got, theirCommit)
	}
}

// A commit made after the preview is not published unseen.
func TestPushRefusesWhenHeadMovedSinceThePreview(t *testing.T) {
	repo, run := unpushedRepo(t)
	commit(t, run, repo, "seen")
	ctx := syncCtx(t)
	preview, err := gitPushPreview(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	commit(t, run, repo, "unseen")

	result, err := gitPushAtPath(ctx, repo, preview.Head, preview.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Outcome != gitSyncChanged {
		t.Fatalf("push = %+v, want refused as changed", result)
	}
	if info := branchInfo(t, repo); info.Unpushed != 2 {
		t.Errorf("%d unpushed after a refused push, want 2", info.Unpushed)
	}
}

// Behind and not ahead: the pull fast-forwards.
func TestPullFastForwards(t *testing.T) {
	mine, runMine, theirs, runTheirs := syncPair(t)
	theirCommit := commit(t, runTheirs, theirs, "theirs")
	runTheirs("push", "-q")
	runMine("fetch", "-q")
	branch := strings.TrimSpace(runMine("branch", "--show-current"))
	ctx := syncCtx(t)

	preview, err := gitPullPreview(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Diverged || preview.Total != 1 || len(preview.Commits) != 1 || preview.Commits[0].Hash != theirCommit {
		t.Fatalf("preview = %+v; want their one commit", preview)
	}

	result, err := gitPullAtPath(ctx, mine, branch)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Outcome != gitSyncPulled {
		t.Fatalf("pull = %+v, want pulled", result)
	}
	if got := headOf(runMine, "HEAD"); got != theirCommit {
		t.Errorf("HEAD = %s, want %s", got, theirCommit)
	}
	if info := branchInfo(t, mine); info.Behind != 0 {
		t.Errorf("still %d behind after the pull", info.Behind)
	}
}

// Both sides have commits: no fast-forward exists, and the pull neither
// merges nor rebases — even when the user's configuration says to rebase.
func TestPullRefusesDivergedHistory(t *testing.T) {
	mine, runMine, theirs, runTheirs := syncPair(t)
	commit(t, runTheirs, theirs, "theirs")
	runTheirs("push", "-q")
	myCommit := commit(t, runMine, mine, "mine")
	runMine("fetch", "-q")
	runMine("config", "pull.rebase", "true")
	branch := strings.TrimSpace(runMine("branch", "--show-current"))
	ctx := syncCtx(t)

	preview, err := gitPullPreview(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Diverged {
		t.Errorf("preview does not see the divergence: %+v", preview)
	}

	result, err := gitPullAtPath(ctx, mine, branch)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Outcome != gitSyncDiverged {
		t.Fatalf("pull = %+v, want refused as diverged", result)
	}
	if got := headOf(runMine, "HEAD"); got != myCommit {
		t.Errorf("HEAD moved to %s; the pull merged or rebased", got)
	}
}

// A local edit the pull would overwrite: git refuses, and says which file.
func TestPullReportsLocalChangesInTheWay(t *testing.T) {
	mine, runMine, theirs, runTheirs := syncPair(t)
	commit(t, runTheirs, theirs, "shared")
	runTheirs("push", "-q")
	if err := os.WriteFile(filepath.Join(mine, "shared.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(runMine("branch", "--show-current"))

	result, err := gitPullAtPath(syncCtx(t), mine, branch)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Outcome != gitSyncLocalChanges || !strings.Contains(result.Message, "shared.txt") {
		t.Fatalf("pull = %+v, want refused over shared.txt", result)
	}
	if data, _ := os.ReadFile(filepath.Join(mine, "shared.txt")); string(data) != "mine\n" {
		t.Errorf("the local edit was lost: %q", data)
	}
}

// A server that wants a password must not get a prompt nobody can answer.
// The environment the app was started from may carry an askpass helper — a
// desktop sets one — that would wait for a person; the push must fail as an
// authentication error instead, well within the time the user would wait.
func TestPushNeverWaitsForCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the waiting helper is a shell script")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	helper := filepath.Join(t.TempDir(), "askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nsleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", helper)
	t.Setenv("SSH_ASKPASS", helper)
	// The user's own credential helpers are not part of this test.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo, run := unpushedRepo(t)
	run("remote", "set-url", "--push", "origin", server.URL+"/repo.git")
	commit(t, run, repo, "local")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	preview, err := gitPushPreview(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	result, err := gitPushAtPath(ctx, repo, preview.Head, preview.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the push took %v; it waited on a prompt", elapsed)
	}
	if result.OK || result.Outcome != gitSyncAuth {
		t.Fatalf("push = %+v, want an authentication failure", result)
	}
}

// A server that accepts the connection and never answers is cut off by the
// timeout, git and everything it started with it.
func TestAStalledPushIsCutOff(t *testing.T) {
	stall := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stall
	}))
	t.Cleanup(func() { close(stall); server.Close() })

	repo, run := unpushedRepo(t)
	run("remote", "set-url", "--push", "origin", server.URL+"/repo.git")
	commit(t, run, repo, "local")
	preview, err := gitPushPreview(syncCtx(t), repo)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	result, err := gitPushAtPath(ctx, repo, preview.Head, preview.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("the push ran %v past a one-second limit", elapsed)
	}
	if result.OK || result.Outcome != gitSyncTimedOut {
		t.Fatalf("push = %+v, want timed out", result)
	}
}

// The prompts are switched off, but a user's own ssh command is kept.
func TestNetworkGitEnvironment(t *testing.T) {
	has := func(env []string, entry string) bool {
		for _, e := range env {
			if e == entry {
				return true
			}
		}
		return false
	}
	env := networkGitEnv([]string{"PATH=/bin", "GIT_ASKPASS=/usr/bin/ksshaskpass", "GIT_TERMINAL_PROMPT=1"}, "")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "PATH=/bin"} {
		if !has(env, want) {
			t.Errorf("%s missing from %v", want, env)
		}
	}
	if has(env, "GIT_ASKPASS=/usr/bin/ksshaskpass") || has(env, "GIT_TERMINAL_PROMPT=1") {
		t.Errorf("the inherited prompt settings survived: %v", env)
	}

	for _, env := range [][]string{
		networkGitEnv([]string{"GIT_SSH_COMMAND=ssh -i key"}, ""),
		networkGitEnv(nil, "ssh -i key"),
	} {
		for _, e := range env {
			if e == "GIT_SSH_COMMAND=ssh -o BatchMode=yes" {
				t.Errorf("the user's ssh command was overridden: %v", env)
			}
		}
	}
}

// A token in a remote URL appears in git's "To ..." line; it is not shown.
func TestGitSyncMessageHidesCredentials(t *testing.T) {
	message := gitSyncMessage("To https://user:secret-token@example.com/repo.git\n!\trefs/heads/main:refs/heads/main\t[rejected] (fetch first)\nDone\n")
	if strings.Contains(message, "secret-token") {
		t.Errorf("the token is shown: %q", message)
	}
	if !strings.Contains(message, "https://***@example.com") || strings.Contains(message, "Done") {
		t.Errorf("message = %q", message)
	}
}

// The badge reads a server tab's configured directory on this computer, where
// a checkout of that path may happen to exist. Pushing from it would publish
// something other than what the tab works on.
func TestPushAndPullRefuseATabOnAServer(t *testing.T) {
	source, err := os.ReadFile("git_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	body := functionBody(t, text, "func (a *App) gitSyncRoot(")
	guard := strings.Index(body, `inst.ServerForWindow(windowIdx) != ""`)
	validate := strings.Index(body, "validateRootSnapshot(")
	if guard < 0 || validate < 0 || guard > validate {
		t.Error("a server tab is not refused before its root is used")
	}
	for _, signature := range []string{"func (a *App) GetGitSyncPreview(", "func (a *App) GitPush(", "func (a *App) GitPull("} {
		if !strings.Contains(functionBody(t, text, signature), "a.gitSyncRoot") {
			t.Errorf("%s does not go through gitSyncRoot", signature)
		}
	}

	branch, err := os.ReadFile("git_branch.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(functionBody(t, string(branch), "func (a *App) GetGitBranch("), "info.OnServer = a.tabOnServer(") {
		t.Error("the badge no longer says the tab is on a server, so it would offer push and pull there")
	}
}

// The project lock is exclusive; held across a two-minute push it would
// freeze every other action. It is taken to check the root and released
// before anything goes over the network.
func TestTheProjectLockIsNotHeldDuringTheNetworkCall(t *testing.T) {
	source, err := os.ReadFile("git_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	for _, signature := range []string{"func (a *App) GitPush(", "func (a *App) GitPull("} {
		body := functionBody(t, text, signature)
		if strings.Contains(body, "beginExpectedProjectMutation") {
			t.Errorf("%s holds the project lock for the whole push/pull", signature)
		}
		if !strings.Contains(body, "a.gitSyncRootLocked(") {
			t.Errorf("%s no longer checks the project under its lock", signature)
		}
	}
	locked := functionBody(t, text, "func (a *App) gitSyncRootLocked(")
	if !strings.Contains(locked, "defer done()") || strings.Contains(locked, "runNetworkGit") {
		t.Error("gitSyncRootLocked must release the lock on return and run nothing over the network")
	}
}
