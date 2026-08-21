package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"asmgr-desktop/session"
)

// Reading a repository's commit history, for browsing it in a dialog.
//
// Paged rather than read whole: a repository of any age holds tens of
// thousands of commits, and loading them to show the twenty on screen would
// make opening the dialog a visible pause and hold the lot in memory for a
// glance at the top of the list.

const (
	// A click opens this, so it may not keep the interface waiting. Long
	// enough for a cold repository on a slow disk, short enough that a wedged
	// git does not become an unresponsive dialog.
	gitHistoryTimeout = 10 * time.Second

	// One screenful several times over: enough that scrolling does not fetch
	// constantly, small enough that the first page arrives promptly.
	gitHistoryPageSize = 100

	// The separator between fields of one commit. Chosen because it cannot
	// occur in any of them — a subject line can contain anything printable,
	// and splitting on a character an author put in their commit message is a
	// bug that only shows up on somebody else's history.
	gitFieldSep  = "\x1f"
	gitRecordSep = "\x1e"
)

// GitCommit is one entry in the history list.
type GitCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"shortHash"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	Email     string `json:"email"`
	// Committed is ISO-8601, formatted where it is displayed rather than here:
	// the interface knows the user's locale and this does not.
	Committed string `json:"committed"`
	// Refs are the branch and tag names pointing at this commit, already
	// stripped of git's decoration syntax.
	Refs []string `json:"refs,omitempty"`
	// Parents distinguishes a merge from an ordinary commit, and lets the
	// frontend draw the shape of the history.
	Parents []string `json:"parents,omitempty"`
}

// GitHistoryPage is one page of commits, plus what the caller needs to ask for
// the next.
type GitHistoryPage struct {
	Path       string      `json:"path"`
	Repository bool        `json:"repository"`
	Branch     string      `json:"branch"`
	Commits    []GitCommit `json:"commits"`
	// HasMore says another page exists, so the list can keep loading on scroll
	// rather than guessing from a short page.
	HasMore bool `json:"hasMore"`
	// Skip is what to pass as offset for the next page.
	Skip int `json:"skip"`
}

// GetGitHistory returns a page of commits for a working directory.
//
// branch is empty for the current checkout, or a branch name to read another
// one — browsing history is not the same as switching to it, and a user
// looking at what happened on a branch should not have to move the working
// tree to do it.
func (a *App) GetGitHistory(sessionID, branch string, skip, windowIdx int, expectedRoot string) (GitHistoryPage, error) {
	path, err := a.gitRootSnapshot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return GitHistoryPage{}, err
	}
	return getGitHistoryAtPath(path, branch, skip)
}

func getGitHistoryAtPath(path, branch string, skip int) (GitHistoryPage, error) {
	page := GitHistoryPage{Path: path, Skip: skip}
	if strings.TrimSpace(path) == "" {
		return page, fmt.Errorf("no directory to read history from")
	}
	if err := validateGitRefArgument(branch); err != nil {
		return page, err
	}
	if skip < 0 {
		skip = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitHistoryTimeout)
	defer cancel()

	inside, err := runDashboardGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return page, nil
	}
	page.Repository = true
	if head, headErr := runDashboardGit(ctx, path, "rev-parse", "--abbrev-ref", "HEAD"); headErr == nil {
		page.Branch = strings.TrimSpace(head)
	}

	// One more than a page, to learn whether another page exists without a
	// second count of the whole history — which on a large repository costs
	// more than the page itself.
	args := []string{
		"log",
		"--skip=" + strconv.Itoa(skip),
		"--max-count=" + strconv.Itoa(gitHistoryPageSize+1),
		"--date=iso-strict",
		"--pretty=format:%H" + gitFieldSep + "%h" + gitFieldSep + "%s" + gitFieldSep +
			"%an" + gitFieldSep + "%ae" + gitFieldSep + "%cd" + gitFieldSep +
			"%D" + gitFieldSep + "%P" + gitFieldSep + "%b" + gitRecordSep,
	}
	if strings.TrimSpace(branch) != "" {
		// --end-of-options is required before an untrusted revision. A plain
		// trailing -- only separates revisions from paths; it does not stop a
		// branch such as "--output=/tmp/file" being parsed as a Git option.
		args = append(args, "--end-of-options", branch, "--")
	}

	output, err := runDashboardGit(ctx, path, args...)
	if ctx.Err() != nil {
		return page, fmt.Errorf("reading the history took too long")
	}
	if err != nil {
		// An unknown branch is the common case here — the list is stale, or the
		// branch was deleted while the dialog was open.
		return page, fmt.Errorf("could not read the history: %w", err)
	}

	commits := parseGitLog(output)
	if len(commits) > gitHistoryPageSize {
		page.HasMore = true
		commits = commits[:gitHistoryPageSize]
	}
	page.Commits = commits
	page.Skip = skip + len(commits)
	return page, nil
}

// parseGitLog turns the record-separated log output into commits.
//
// Split out so it can be tested against real git output without a repository,
// which is what makes the field handling checkable at all: a subject line can
// contain anything, including the characters a naive parser splits on.
func parseGitLog(output string) []GitCommit {
	var commits []GitCommit

	for _, record := range strings.Split(output, gitRecordSep) {
		record = strings.TrimLeft(record, "\r\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 8 {
			continue
		}

		commit := GitCommit{
			Hash:      strings.TrimSpace(fields[0]),
			ShortHash: strings.TrimSpace(fields[1]),
			Subject:   fields[2],
			Author:    fields[3],
			Email:     fields[4],
			Committed: strings.TrimSpace(fields[5]),
			Refs:      parseGitRefs(fields[6]),
		}
		if parents := strings.Fields(fields[7]); len(parents) > 0 {
			commit.Parents = parents
		}
		if len(fields) > 8 {
			commit.Body = strings.TrimRight(fields[8], "\r\n")
		}
		if commit.Hash == "" {
			continue
		}
		commits = append(commits, commit)
	}
	return commits
}

// parseGitRefs turns "HEAD -> main, origin/main, tag: v1.0" into names.
func parseGitRefs(decoration string) []string {
	decoration = strings.TrimSpace(decoration)
	if decoration == "" {
		return nil
	}

	var refs []string
	for _, ref := range strings.Split(decoration, ",") {
		ref = strings.TrimSpace(ref)
		// "HEAD -> main" names the branch HEAD is on; the arrow is git's
		// notation, not part of the name.
		if at := strings.Index(ref, "->"); at >= 0 {
			ref = strings.TrimSpace(ref[at+2:])
		}
		ref = strings.TrimPrefix(ref, "tag: ")
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// GetGitCommitFiles lists what one commit changed, for the file tree.
func (a *App) GetGitCommitFiles(sessionID, hash string, windowIdx int, expectedRoot string) ([]session.DiffFileSummary, error) {
	path, err := a.gitRootSnapshot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return nil, err
	}
	return getGitCommitFilesAtPath(path, hash)
}

func getGitCommitFilesAtPath(path, hash string) ([]session.DiffFileSummary, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("no commit to read")
	}
	if err := validateGitCommitHash(hash); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitHistoryTimeout)
	defer cancel()

	// A merge shows nothing without -m: git has no single "the diff" for two
	// parents, and left alone it prints an empty file list, which reads as "an
	// empty commit" rather than "a merge".
	// -z keeps paths intact: a filename can contain anything, and git quotes
	// the awkward ones unless the output is NUL-separated.
	output, err := runDashboardGit(ctx, path,
		"show", "--numstat", "-z", "--format=", "-m", "--first-parent",
		"--end-of-options", hash, "--")
	if ctx.Err() != nil {
		return nil, fmt.Errorf("reading the commit took too long")
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the commit: %w", err)
	}

	files := session.ParseNumstatZ(output)
	if files == nil {
		files = []session.DiffFileSummary{}
	}
	return files, nil
}

// GetGitCommitDiff returns one file's diff within a commit.
//
// wholeFile asks for the file around the change rather than the few lines
// either side of it: seeing three lines of context tells you what changed but
// not what it changed within, which for a review of somebody else's commit is
// most of the question.
func (a *App) GetGitCommitDiff(sessionID, hash, file string, wholeFile bool, windowIdx int, expectedRoot string) (*session.DiffFile, error) {
	path, err := a.gitRootSnapshot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return nil, err
	}
	return getGitCommitDiffAtPath(path, hash, file, wholeFile)
}

func getGitCommitDiffAtPath(path, hash, file string, wholeFile bool) (*session.DiffFile, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("no commit to read")
	}
	if err := validateGitCommitHash(hash); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitHistoryTimeout)
	defer cancel()

	args := []string{"show", "--format=", "-m", "--first-parent"}
	if wholeFile {
		// A context larger than any file, which is how git is asked for all of
		// it — there is no "whole file" flag.
		args = append(args, "-U100000")
	}
	args = append(args, "--end-of-options", hash)
	if strings.TrimSpace(file) != "" {
		args = append(args, "--", file)
	}

	output, err := runDashboardGit(ctx, path, args...)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("reading the diff took too long")
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the diff: %w", err)
	}

	files := session.ParseDiffFiles(output)
	if len(files) == 0 {
		return nil, nil
	}
	return &files[0], nil
}

func validateGitRefArgument(ref string) error {
	if ref == "" {
		return nil
	}
	if ref != strings.TrimSpace(ref) || strings.HasPrefix(ref, "-") || strings.IndexFunc(ref, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0 {
		return fmt.Errorf("invalid Git branch")
	}
	return nil
}

func validateGitCommitHash(hash string) error {
	if len(hash) < 4 || len(hash) > 64 {
		return fmt.Errorf("invalid Git commit hash")
	}
	for _, r := range hash {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fmt.Errorf("invalid Git commit hash")
		}
	}
	return nil
}
