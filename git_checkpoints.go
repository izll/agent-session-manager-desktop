package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"asmgr-desktop/session"
)

// Checkpoints: a snapshot of a working tree, taken before an agent is let
// loose on it, that can be put back with one click.
//
// Stored as ordinary git commits, because git already knows how to store a
// tree of files cheaply, deduplicated against everything the repository
// holds. What makes them checkpoints rather than commits is where they live
// and how they are made:
//
//   - They hang off a private ref namespace, refs/asmgr/checkpoints/…, which
//     keeps them from being garbage-collected without showing up as a branch
//     or a tag, in `git log --all`-less tooling, or in a push.
//   - They are built through a temporary index, so taking one never touches
//     the user's index, HEAD, branch or stash. The user has staged exactly
//     what they meant to stage; a snapshot that disturbed that would be a tool
//     that punishes you for using it.
//   - They go through `git add -A`, so .gitignore is respected: ignored files
//     (build output, node_modules, .env) are neither saved nor, on restore,
//     touched.
//
// The metadata (label, kind, session) lives in the commit message rather than
// in the app's storage. The checkpoint then describes itself: nothing can get
// out of step when a ref is deleted by hand, it survives the app's config
// being reset, and another copy of the app sees the same list.

const (
	checkpointRefRoot = "refs/asmgr/checkpoints/"

	// A snapshot hashes every changed and untracked file, so a large tree can
	// honestly take a while; this bounds the case where git does not return.
	checkpointTimeout = 2 * time.Minute

	// The id is the creation time, so the ref names sort by age and the list
	// needs no second lookup to be ordered. Fixed width, so string order is
	// time order.
	checkpointIDLayout = "20060102-150405.000000000"

	checkpointKindManual        = "manual"
	checkpointKindBeforeRestore = "beforeRestore"

	checkpointTrailerMarker   = "Asmgr-Checkpoint"
	checkpointTrailerKind     = "Asmgr-Checkpoint-Kind"
	checkpointTrailerLabel    = "Asmgr-Checkpoint-Label"
	checkpointTrailerSession  = "Asmgr-Checkpoint-Session"
	checkpointTrailerRestored = "Asmgr-Checkpoint-Restored-From"

	// Long enough for a sentence about what is about to be tried.
	maxCheckpointLabel = 120

	// A gitlink: a submodule, or a nested repository `add -A` picked up.
	gitModeSubmodule = "160000"
)

var checkpointIDPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}\.[0-9]{9}(-[0-9]+)?$`)

// checkpointMu serialises checkpoint work. Two restores racing in one tree
// would interleave their deletes and writes; one lock for all repositories is
// plenty for an action taken by hand.
var checkpointMu sync.Mutex

// Checkpoint is one saved state of a working tree.
type Checkpoint struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	ShortHash string `json:"shortHash"`
	// Created is ISO-8601; the interface formats it in the user's locale.
	Created string `json:"created"`
	Label   string `json:"label"`
	// Kind is "manual", or "beforeRestore" for the one a restore takes of the
	// state it is about to replace.
	Kind string `json:"kind"`
	// RestoredFrom is, on a "before restore" checkpoint, the id of the one
	// whose restore made it.
	RestoredFrom string `json:"restoredFrom,omitempty"`
	Session      string `json:"session,omitempty"`
	// How far the checkpoint is from the working tree as it is now. Meaningful
	// only when StatsKnown is set.
	Files      int  `json:"files"`
	Insertions int  `json:"insertions"`
	Deletions  int  `json:"deletions"`
	StatsKnown bool `json:"statsKnown"`
}

// CheckpointList is the dialog's view of one repository.
type CheckpointList struct {
	// Root is the top of the work tree the checkpoints cover — possibly above
	// the tab's own directory, since a checkpoint is of the whole repository.
	Root        string       `json:"root"`
	Checkpoints []Checkpoint `json:"checkpoints"`
}

// CheckpointRestoreResult says what a restore did.
type CheckpointRestoreResult struct {
	// Before is the checkpoint taken of the state the restore replaced, so the
	// restore can itself be undone.
	Before  Checkpoint `json:"before"`
	Written int        `json:"written"`
	Removed int        `json:"removed"`
}

// ListCheckpoints returns the checkpoints of the tab's repository, newest
// first, each with how far it is from the working tree now.
func (a *App) ListCheckpoints(sessionID string, windowIdx int, expectedRoot string) (CheckpointList, error) {
	_, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return CheckpointList{}, err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	return listCheckpoints(ctx, root)
}

// CreateCheckpoint snapshots the tab's repository.
func (a *App) CreateCheckpoint(sessionID string, windowIdx int, expectedRoot, label, expectedProjectID string) (Checkpoint, error) {
	// Writes a ref into the repository, so it is held to the project lock like
	// every other write: a second copy of the app without the lock must not
	// change a project another copy owns.
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return Checkpoint{}, err
	}
	defer done()
	inst, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	created, _, err := createCheckpoint(ctx, root, checkpointMeta{
		Kind:    checkpointKindManual,
		Label:   label,
		Session: inst.Name,
	})
	return created, err
}

// RestoreCheckpoint makes the tab's working tree match a checkpoint, after
// first taking a checkpoint of what it replaces.
func (a *App) RestoreCheckpoint(sessionID string, windowIdx int, expectedRoot, checkpointID, expectedProjectID string) (CheckpointRestoreResult, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return CheckpointRestoreResult{}, err
	}
	defer done()
	inst, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return CheckpointRestoreResult{}, err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	return restoreCheckpoint(ctx, root, checkpointID, inst.Name)
}

// DeleteCheckpoint removes a checkpoint. Its commit becomes unreachable and
// git collects it in due course.
func (a *App) DeleteCheckpoint(sessionID string, windowIdx int, expectedRoot, checkpointID, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	_, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	return deleteCheckpoint(ctx, root, checkpointID)
}

// checkpointRoot resolves the tab to the top of its local work tree.
//
// The expected root is mandatory, as for the history: the webview names the
// directory it showed the user, and that must still be the tab's directory,
// or a restore would rewrite a tree nobody was looking at.
func (a *App) checkpointRoot(sessionID string, windowIdx int, expectedRoot string) (*session.Instance, string, error) {
	inst, err := a.browseInstance(sessionID, windowIdx)
	if err != nil {
		return nil, "", err
	}
	// Before the root is validated: a server's path does not exist here, and
	// the validation would fail with a message about a changed directory,
	// which is not what is wrong. Worse, the same path could exist on this
	// computer and be a different tree entirely.
	if inst.ServerForWindow(windowIdx) != "" {
		return nil, "", errors.New("error.checkpointsRemote")
	}
	dir, err := validateRootSnapshot(inst, expectedRoot, "error.checkpointsRootChanged")
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitBranchTimeout)
	defer cancel()
	top, err := checkpointTopLevel(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	return inst, top, nil
}

// checkpointTopLevel is the work tree a directory belongs to. A checkpoint is
// of the whole repository, not of the subdirectory the tab happens to be in:
// restoring half a tree would leave the other half from a different moment.
func checkpointTopLevel(ctx context.Context, dir string) (string, error) {
	out, err := runGitStdout(ctx, dir, "rev-parse", "--show-toplevel")
	top := strings.TrimSpace(out)
	if err != nil || top == "" {
		return "", errors.New("error.checkpointsNotRepository")
	}
	return filepath.FromSlash(top), nil
}

type checkpointMeta struct {
	Kind         string
	Label        string
	Session      string
	RestoredFrom string
}

// checkpointGit runs git in the work tree with extra environment and optional
// input, returning standard output. Standard error goes into the error, since
// these are writes and a failure has to say why.
func checkpointGit(ctx context.Context, top string, env []string, stdin []byte, args ...string) (string, error) {
	cmd := session.GitCommandContext(ctx, append([]string{"-C", top}, args...)...)
	// Appended, not replaced: git needs HOME and PATH, and os/exec keeps the
	// last value of a repeated key, so these win over anything inherited.
	cmd.Env = append(os.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], message)
	}
	return stdout.String(), nil
}

// checkpointNamespace is where this work tree's checkpoints live.
//
// Refs are shared by every worktree of a repository, and a checkpoint of one
// worktree restored into another would overwrite it with a different
// branch's files. So each linked worktree gets a namespace of its own, keyed
// by its administrative name, which — unlike its path — survives the
// worktree being moved.
func checkpointNamespace(ctx context.Context, top string) (string, error) {
	out, err := runGitStdout(ctx, top, "rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", errors.New("error.checkpointsNotRepository")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", errors.New("error.checkpointsNotRepository")
	}
	gitDir := filepath.Clean(filepath.FromSlash(strings.TrimSpace(lines[0])))
	commonDir := filepath.FromSlash(strings.TrimSpace(lines[1]))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(top, commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	if resolved, err := filepath.EvalSymlinks(commonDir); err == nil {
		commonDir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(gitDir); err == nil {
		gitDir = resolved
	}
	if session.CanonicalProjectPath(gitDir) == session.CanonicalProjectPath(commonDir) {
		return checkpointRefRoot + "main/", nil
	}
	// Hex, because a worktree's name is a directory name and may hold
	// characters a ref name may not.
	return checkpointRefRoot + "wt-" + hex.EncodeToString([]byte(filepath.Base(gitDir))) + "/", nil
}

// snapshotTree writes the work tree as it is now into the object database and
// returns its tree, without touching the user's index.
//
// The temporary index starts as a copy of the real one rather than as HEAD:
// the copy carries git's cached file stats, so `add -A` only hashes files that
// actually changed instead of the whole tree, and it keeps skip-worktree
// entries, without which a sparse checkout's absent files would be recorded as
// deleted. Should the copy not be usable (a split index, say), HEAD is the
// fallback.
func snapshotTree(ctx context.Context, top, head string) (string, error) {
	tmp, err := os.MkdirTemp("", "asmgr-checkpoint-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	tree, err := snapshotTreeWithIndex(ctx, top, head, filepath.Join(tmp, "index-copy"), true)
	if err == nil {
		return tree, nil
	}
	if ctx.Err() != nil {
		return "", err
	}
	return snapshotTreeWithIndex(ctx, top, head, filepath.Join(tmp, "index-head"), false)
}

func snapshotTreeWithIndex(ctx context.Context, top, head, indexPath string, copyIndex bool) (string, error) {
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if copyIndex {
		if err := copyUserIndex(ctx, top, indexPath); err != nil {
			return "", err
		}
	} else if head != "" {
		if _, err := checkpointGit(ctx, top, env, nil, "read-tree", head); err != nil {
			return "", err
		}
	}
	// Without a seed the index simply does not exist yet, which git reads as
	// empty — an empty FILE would be a corrupt index instead.
	if _, err := checkpointGit(ctx, top, env, nil, "add", "-A"); err != nil {
		return "", err
	}
	out, err := checkpointGit(ctx, top, env, nil, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// copyUserIndex copies the repository's index to dst. A repository that has
// never had anything staged has no index, and dst is then left absent.
func copyUserIndex(ctx context.Context, top, dst string) error {
	out, err := runGitStdout(ctx, top, "rev-parse", "--git-path", "index")
	if err != nil {
		return err
	}
	src := filepath.FromSlash(strings.TrimSpace(out))
	if !filepath.IsAbs(src) {
		src = filepath.Join(top, src)
	}
	in, err := os.Open(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out2, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out2, in); err != nil {
		out2.Close()
		return err
	}
	return out2.Close()
}

// headCommit is HEAD's commit, or "" in a repository with no commits yet.
func headCommit(ctx context.Context, top string) string {
	out, err := runGitStdout(ctx, top, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// cleanCheckpointLabel keeps a label to one short line. It becomes part of a
// commit message and a list row; a newline in it would forge a trailer.
func cleanCheckpointLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, label)
	label = strings.Join(strings.Fields(label), " ")
	if runes := []rune(label); len(runes) > maxCheckpointLabel {
		label = strings.TrimSpace(string(runes[:maxCheckpointLabel]))
	}
	return label
}

func checkpointMessage(meta checkpointMeta) string {
	subject := "asmgr checkpoint"
	if meta.Label != "" {
		subject += ": " + meta.Label
	} else if meta.Kind == checkpointKindBeforeRestore {
		subject += ": before restore"
	}
	var b strings.Builder
	b.WriteString(subject)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%s: 1\n", checkpointTrailerMarker)
	fmt.Fprintf(&b, "%s: %s\n", checkpointTrailerKind, meta.Kind)
	if meta.Label != "" {
		fmt.Fprintf(&b, "%s: %s\n", checkpointTrailerLabel, meta.Label)
	}
	if meta.Session != "" {
		fmt.Fprintf(&b, "%s: %s\n", checkpointTrailerSession, meta.Session)
	}
	if meta.RestoredFrom != "" {
		fmt.Fprintf(&b, "%s: %s\n", checkpointTrailerRestored, meta.RestoredFrom)
	}
	return b.String()
}

// parseCheckpointMessage reads back what checkpointMessage wrote.
func parseCheckpointMessage(message string) checkpointMeta {
	meta := checkpointMeta{Kind: checkpointKindManual}
	for _, line := range strings.Split(message, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case checkpointTrailerKind:
			if value == checkpointKindBeforeRestore {
				meta.Kind = value
			}
		case checkpointTrailerLabel:
			meta.Label = value
		case checkpointTrailerSession:
			meta.Session = value
		case checkpointTrailerRestored:
			meta.RestoredFrom = value
		}
	}
	return meta
}

// checkpointIdentity signs the commits as the app rather than as the user.
// commit-tree refuses to run without an identity, and a repository whose user
// never configured one must still be able to take checkpoints; nor is a
// checkpoint something the user authored.
var checkpointIdentity = []string{
	"GIT_AUTHOR_NAME=Agent Session Manager",
	"GIT_AUTHOR_EMAIL=asmgr@localhost",
	"GIT_COMMITTER_NAME=Agent Session Manager",
	"GIT_COMMITTER_EMAIL=asmgr@localhost",
}

// createCheckpoint snapshots the work tree and stores it under a new ref. It
// also returns the snapshot's tree, which a restore needs next.
func createCheckpoint(ctx context.Context, top string, meta checkpointMeta) (Checkpoint, string, error) {
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return Checkpoint{}, "", err
	}
	meta.Label = cleanCheckpointLabel(meta.Label)
	meta.Session = cleanCheckpointLabel(meta.Session)

	head := headCommit(ctx, top)
	tree, err := snapshotTree(ctx, top, head)
	if err != nil {
		return Checkpoint{}, "", err
	}

	// --no-gpg-sign: a user with commit.gpgSign set would otherwise be asked
	// for a passphrase by a process with no terminal, and wait for ever.
	args := []string{"commit-tree", "--no-gpg-sign", tree}
	if head != "" {
		args = append(args, "-p", head)
	}
	out, err := checkpointGit(ctx, top, checkpointIdentity, []byte(checkpointMessage(meta)), args...)
	if err != nil {
		return Checkpoint{}, "", err
	}
	commit := strings.TrimSpace(out)

	now := time.Now().UTC()
	base := now.Format(checkpointIDLayout)
	id := base
	// The empty old value makes update-ref refuse a ref that already exists,
	// so two checkpoints in the same nanosecond cannot overwrite each other.
	for attempt := 1; ; attempt++ {
		_, err = checkpointGit(ctx, top, nil, nil, "update-ref", "-m", "asmgr checkpoint", namespace+id, commit, "")
		if err == nil {
			break
		}
		if attempt >= 5 || ctx.Err() != nil {
			return Checkpoint{}, "", err
		}
		id = base + "-" + strconv.Itoa(attempt)
	}

	created := Checkpoint{
		ID:           id,
		Hash:         commit,
		ShortHash:    shortHash(commit),
		Created:      now.Format(time.RFC3339),
		Label:        meta.Label,
		Kind:         meta.Kind,
		RestoredFrom: meta.RestoredFrom,
		Session:      meta.Session,
		StatsKnown:   true,
	}
	return created, tree, nil
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func validateCheckpointID(id string) error {
	if !checkpointIDPattern.MatchString(id) {
		return errors.New("error.checkpointNotFound")
	}
	return nil
}

// resolveCheckpoint returns the commit and tree a checkpoint id names.
func resolveCheckpoint(ctx context.Context, top, namespace, id string) (commit, tree string, err error) {
	if err := validateCheckpointID(id); err != nil {
		return "", "", err
	}
	// The full ref name, so a branch or tag that happens to share the id's
	// spelling can never be what is restored.
	out, err := runGitStdout(ctx, top, "rev-parse", "--verify", "--quiet", "--end-of-options", namespace+id+"^{commit}")
	commit = strings.TrimSpace(out)
	if err != nil || commit == "" {
		return "", "", errors.New("error.checkpointNotFound")
	}
	out, err = runGitStdout(ctx, top, "rev-parse", "--verify", "--quiet", "--end-of-options", commit+"^{tree}")
	tree = strings.TrimSpace(out)
	if err != nil || tree == "" {
		return "", "", errors.New("error.checkpointNotFound")
	}
	return commit, tree, nil
}

func listCheckpoints(ctx context.Context, top string) (CheckpointList, error) {
	list := CheckpointList{Root: top, Checkpoints: []Checkpoint{}}
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return list, err
	}
	// Newest first by name: the ids are fixed-width timestamps, finer than the
	// committer date, which has only whole seconds.
	out, err := runGitStdout(ctx, top, "for-each-ref", "--sort=-refname",
		"--format=%(refname)%1f%(objectname)%1f%(committerdate:iso-strict)%1f%(tree)%1f%(contents)%1e",
		namespace)
	if err != nil {
		return list, fmt.Errorf("could not list checkpoints: %w", err)
	}

	type entry struct {
		checkpoint Checkpoint
		tree       string
	}
	var entries []entry
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimLeft(record, "\r\n")
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 5)
		if len(fields) < 5 {
			continue
		}
		id := strings.TrimPrefix(fields[0], namespace)
		// Anything else under the namespace was not made here; restoring it by
		// an id the validator would reject is impossible anyway.
		if validateCheckpointID(id) != nil {
			continue
		}
		meta := parseCheckpointMessage(fields[4])
		entries = append(entries, entry{
			checkpoint: Checkpoint{
				ID:           id,
				Hash:         fields[1],
				ShortHash:    shortHash(fields[1]),
				Created:      fields[2],
				Label:        meta.Label,
				Kind:         meta.Kind,
				RestoredFrom: meta.RestoredFrom,
				Session:      meta.Session,
			},
			tree: fields[3],
		})
	}
	if len(entries) == 0 {
		return list, nil
	}

	// How far each is from now needs a snapshot of now. Without one, the
	// numbers would only cover tracked files, and "no changes" beside a
	// checkpoint missing a new file would be a lie on the one row that matters.
	current, err := snapshotTree(ctx, top, headCommit(ctx, top))
	for _, e := range entries {
		if err == nil {
			if stat, statErr := runGitStdout(ctx, top, "diff-tree", "-r", "--no-renames", "--shortstat", e.tree, current); statErr == nil {
				e.checkpoint.Files, e.checkpoint.Insertions, e.checkpoint.Deletions = parseShortStat(stat)
				e.checkpoint.StatsKnown = true
			}
		}
		list.Checkpoints = append(list.Checkpoints, e.checkpoint)
	}
	return list, nil
}

var shortStatNumber = regexp.MustCompile(`(\d+) (file|insertion|deletion)`)

// parseShortStat reads " 3 files changed, 10 insertions(+), 2 deletions(-)".
// Empty output means the two trees are the same.
func parseShortStat(output string) (files, insertions, deletions int) {
	for _, match := range shortStatNumber.FindAllStringSubmatch(output, -1) {
		n, _ := strconv.Atoi(match[1])
		switch match[2] {
		case "file":
			files = n
		case "insertion":
			insertions = n
		case "deletion":
			deletions = n
		}
	}
	return files, insertions, deletions
}

func deleteCheckpoint(ctx context.Context, top, id string) error {
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return err
	}
	commit, _, err := resolveCheckpoint(ctx, top, namespace, id)
	if err != nil {
		return err
	}
	// The old value guards against deleting a ref that was replaced since it
	// was listed.
	_, err = checkpointGit(ctx, top, nil, nil, "update-ref", "-d", namespace+id, commit)
	return err
}

// checkpointChange is one path that differs between the tree now and the
// checkpoint's.
type checkpointChange struct {
	path string
	// write: the checkpoint has this file (added, changed, or of another
	// type), so it is written from there. Otherwise it is only in the tree
	// now and is removed.
	write bool
	// gitlink entries are submodules or nested repositories; their contents
	// are not in the checkpoint, and removing them could destroy a whole
	// repository, so they are left alone.
	gitlink bool
}

// parseRawDiffZ reads `git diff-tree -r -z --no-renames` output from `now` to
// `checkpoint`: ":<mode now> <mode checkpoint> <oid> <oid> <status>\0<path>\0".
func parseRawDiffZ(output string) ([]checkpointChange, error) {
	var changes []checkpointChange
	parts := strings.Split(output, "\x00")
	for at := 0; at+1 < len(parts); at += 2 {
		header := parts[at]
		name := parts[at+1]
		if header == "" && name == "" {
			break
		}
		fields := strings.Fields(strings.TrimPrefix(header, ":"))
		if len(fields) < 5 {
			return nil, fmt.Errorf("unexpected diff output %q", header)
		}
		srcMode, dstMode, status := fields[0], fields[1], fields[4]
		change := checkpointChange{path: name}
		switch status[0] {
		case 'D':
			change.gitlink = srcMode == gitModeSubmodule
		case 'A', 'M', 'T':
			change.write = true
			change.gitlink = dstMode == gitModeSubmodule
			// A file replaced by a submodule, or the other way round: the old
			// entry has to go before the new one can be written.
			if status[0] == 'T' && (srcMode == gitModeSubmodule) != (dstMode == gitModeSubmodule) {
				change.gitlink = true
			}
		default:
			return nil, fmt.Errorf("unexpected diff status %q", status)
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// checkpointPath turns a git path into one under top, refusing anything that
// would leave it. Git does not produce such paths; this is so a corrupt or
// hostile tree cannot make a restore write outside the repository.
func checkpointPath(top, gitPath string) (string, error) {
	clean := path.Clean(gitPath)
	if gitPath == "" || clean != gitPath || clean == "." || path.IsAbs(clean) ||
		clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(gitPath, "\\") {
		return "", fmt.Errorf("refusing unsafe path %q", gitPath)
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.EqualFold(part, ".git") {
			return "", fmt.Errorf("refusing unsafe path %q", gitPath)
		}
	}
	return filepath.Join(top, filepath.FromSlash(clean)), nil
}

// restoreCheckpoint makes the work tree match a checkpoint.
//
// Only the paths that differ are touched. Checking out the whole tree would
// rewrite every file, and every build tool watching the tree would then
// rebuild everything for a restore that changed three files.
//
// Never `reset --hard`, `clean`, `checkout <branch>` or `stash`: those move
// HEAD, the index or the stash, or delete ignored files, and the user asked
// for none of that. After a restore `git status` shows the restored state as
// changes against the same HEAD as before.
func restoreCheckpoint(ctx context.Context, top, id, sessionName string) (CheckpointRestoreResult, error) {
	var result CheckpointRestoreResult
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return result, err
	}
	_, targetTree, err := resolveCheckpoint(ctx, top, namespace, id)
	if err != nil {
		return result, err
	}

	// First a checkpoint of what is about to be replaced, so that a restore to
	// the wrong checkpoint is one more restore away from undone. Its tree is
	// also exactly "now" for the comparison below.
	before, currentTree, err := createCheckpoint(ctx, top, checkpointMeta{
		Kind:         checkpointKindBeforeRestore,
		Session:      sessionName,
		RestoredFrom: id,
	})
	if err != nil {
		return result, err
	}
	result.Before = before

	out, err := runGitStdout(ctx, top, "diff-tree", "-r", "-z", "--no-renames", currentTree, targetTree)
	if err != nil {
		return result, fmt.Errorf("could not compare with the checkpoint: %w", err)
	}
	changes, err := parseRawDiffZ(out)
	if err != nil {
		return result, err
	}

	var removals, writes []string
	removing := map[string]bool{}
	for _, change := range changes {
		if change.gitlink {
			continue
		}
		full, err := checkpointPath(top, change.path)
		if err != nil {
			return result, err
		}
		if change.write {
			writes = append(writes, change.path)
		} else {
			removals = append(removals, full)
			removing[full] = true
		}
	}

	// A directory where the checkpoint has a file: checkout-index would delete
	// the whole directory to make room, ignored files and all. Refuse instead,
	// before anything is changed, unless everything in it is being removed
	// anyway.
	for _, gitPath := range writes {
		full, _ := checkpointPath(top, gitPath)
		if info, err := os.Lstat(full); err == nil && info.IsDir() {
			if keeps := directoryKeepsSomething(full, removing); keeps {
				return result, fmt.Errorf("error.checkpointDirectoryInTheWay|%s", gitPath)
			}
		}
	}

	for _, full := range removals {
		if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("could not remove %s: %w", full, err)
		}
		result.Removed++
		removeEmptyParents(top, filepath.Dir(full))
	}

	if len(writes) > 0 {
		tmp, err := os.MkdirTemp("", "asmgr-restore-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(tmp)
		env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
		if _, err := checkpointGit(ctx, top, env, nil, "read-tree", targetTree); err != nil {
			return result, err
		}
		// Paths on stdin, NUL-separated: no quoting to get wrong, and no limit
		// on how many there are. -f overwrites what is there; modes and
		// symlinks come from the index, as in any checkout.
		input := []byte(strings.Join(writes, "\x00") + "\x00")
		if _, err := checkpointGit(ctx, top, env, input, "checkout-index", "-f", "-z", "--stdin"); err != nil {
			return result, err
		}
		result.Written = len(writes)
	}
	return result, nil
}

// directoryKeepsSomething says whether dir holds anything that is not about to
// be removed — an ignored file, typically.
func directoryKeepsSomething(dir string, removing map[string]bool) bool {
	keeps := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			keeps = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		if !removing[p] {
			keeps = true
			return filepath.SkipAll
		}
		return nil
	})
	return keeps
}

// removeEmptyParents removes directories a removal left empty, as git does
// when it deletes a file, stopping at the first that still holds something —
// often an ignored file, which is then left where it was.
func removeEmptyParents(top, dir string) {
	for {
		rel, err := filepath.Rel(top, dir)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
