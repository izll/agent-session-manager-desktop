package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Giving a session its own git worktree.
//
// A worktree is a second checkout of the same repository, on its own branch,
// in its own directory. Two agents working the same project then edit
// different files on disk rather than the same ones, which is what makes
// running them at the same time safe.
//
// This is optional, per session. A session without one behaves exactly as
// every session did before: it works in the directory it was given.

// WorktreeBranchPrefix keeps the branches this creates recognisable, and out
// of the way of the names a person picks by hand.
const WorktreeBranchPrefix = "asmgr/"

// worktreeDirSuffix names the directory holding a project's worktrees.
//
// A sibling of the repository rather than a directory inside it: a worktree
// nested in its own repository shows up as untracked content in the parent,
// and every `git status` there would then carry it.
const worktreeDirSuffix = "-worktrees"

// WorktreePlan is where a session's worktree would go, and on what branch.
type WorktreePlan struct {
	// Dir is the checkout directory, which becomes the session's path.
	Dir string
	// Branch is the branch created there.
	Branch string
	// RepoRoot is the repository the worktree is added to.
	RepoRoot string
}

// PlanWorktree works out the directory and branch for a session, without
// creating anything.
//
// Separate from the creation so the dialog can show the user what it is about
// to do, and so the names can be tested without a repository.
func PlanWorktree(repoRoot, sessionName string) WorktreePlan {
	return PlanWorktreeNamed(repoRoot, sessionName, "")
}

// PlanWorktreeNamed is PlanWorktree with a branch the user chose.
//
// branch empty derives one from the name, which is what the dialog offers
// before anybody edits the field. A branch given here is taken as typed, only
// cleaned of what git refuses — a person who types a name has a reason for it,
// and the prefix is not forced onto it either.
func PlanWorktreeNamed(repoRoot, sessionName, branch string) WorktreePlan {
	parent := filepath.Dir(repoRoot)
	base := filepath.Base(repoRoot)

	chosen := strings.TrimSpace(branch)
	if chosen == "" {
		chosen = WorktreeBranchPrefix + worktreeBranchName(sessionName)
	} else {
		chosen = worktreeBranchName(chosen)
	}

	// The directory follows the branch when one was given, so the two read as
	// the same thing on disk and in git; from the session's name otherwise.
	dirFrom := sessionName
	if strings.TrimSpace(branch) != "" {
		dirFrom = strings.TrimPrefix(chosen, WorktreeBranchPrefix)
	}

	return WorktreePlan{
		Dir:      filepath.Join(parent, base+worktreeDirSuffix, worktreeDirName(dirFrom)),
		Branch:   chosen,
		RepoRoot: repoRoot,
	}
}

// worktreeBranchName is the session's name, kept as the user typed it.
//
// git does not need it reduced to ASCII. Its ref rules are a blocklist of
// metacharacters, not an allowlist of letters: a branch called "hibajavítás"
// is perfectly legal, and so are Polish and Japanese names. Measured against
// git's own check-ref-format, which is the authority — the earlier assumption
// that accents had to go was simply wrong, and it turned a readable name into
// "hibajav-t-s".
//
// What git does reject is replaced: spaces, the metacharacters ~^:?*[\, "..",
// a leading dash, a trailing dot. The result is checked with check-ref-format
// before it is used, so a name this does not anticipate fails where it was
// chosen rather than deep inside `git worktree add`.
func worktreeBranchName(name string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(name) {
		if refCharIsForbidden(r) {
			if !lastDash && out.Len() > 0 {
				out.WriteByte('-')
				lastDash = true
			}
			continue
		}
		out.WriteRune(r)
		lastDash = false
	}
	// A leading dash, a trailing dot and a ".lock" ending are each rejected by
	// git in their own right.
	branch := strings.Trim(out.String(), "-.")
	branch = strings.ReplaceAll(branch, "..", "-")
	branch = strings.TrimSuffix(branch, ".lock")
	if branch == "" {
		return "session"
	}
	return branch
}

// refCharIsForbidden reports the characters git will not take in a ref name.
func refCharIsForbidden(r rune) bool {
	switch r {
	case ' ', '\t', '~', '^', ':', '?', '*', '[', '\\':
		return true
	}
	// Control characters and DEL are rejected as well. Everything at or above
	// U+0080 is passed through: git treats a ref name as opaque bytes.
	return r < 0x20 || r == 0x7f
}

// worktreeDirName is the session's name reduced to ASCII, for the directory.
//
// The directory is a real filesystem concern where the branch is not: macOS
// stores names decomposed and Linux composed, so the same accented name can
// compare unequal between them, and Windows has its own limits. Transliterated
// rather than stripped — "hibajavítás" becomes "hibajavitas", where deleting
// the accents gave "hibajavts" and replacing them gave "hibajav-t-s", neither
// of which reads as the session it belongs to.
func worktreeDirName(name string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if folded, ok := asciiFolding[r]; ok {
			out.WriteString(folded)
			lastDash = false
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
			lastDash = false
		default:
			// Runs of punctuation collapse, and a name cannot start with a
			// dash.
			if !lastDash && out.Len() > 0 {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		// A name with nothing transliterable in it — Japanese, say. The
		// branch still carries the real name; this only has to be a legal
		// directory.
		return "session"
	}
	// Long enough to stay readable, short enough for a path on Windows, where
	// the whole path is what is limited rather than each component.
	const maxSlug = 40
	if len(slug) > maxSlug {
		slug = strings.Trim(slug[:maxSlug], "-")
	}
	return slug
}

// asciiFolding maps accented letters to the plain letters they are built on.
//
// A table rather than Unicode decomposition, because the two cases that matter
// most here are not decomposable: German ß is one letter that becomes "ss",
// and Polish ł is a letter in its own right rather than an l with a mark.
// Lower case only — the name is lowered before this is consulted.
var asciiFolding = map[rune]string{
	// Hungarian, and the Latin-1 letters it shares with its neighbours.
	'á': "a", 'é': "e", 'í': "i", 'ó': "o", 'ö': "o", 'ő': "o",
	'ú': "u", 'ü': "u", 'ű': "u",
	// The rest of Western Europe.
	'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'æ': "ae",
	'ç': "c", 'è': "e", 'ê': "e", 'ë': "e",
	'ì': "i", 'î': "i", 'ï': "i",
	'ñ': "n", 'ò': "o", 'ô': "o", 'õ': "o", 'ø': "o",
	'ù': "u", 'û': "u", 'ý': "y", 'ÿ': "y", 'ß': "ss",
	// Central and Eastern Europe.
	'ā': "a", 'ă': "a", 'ą': "a", 'ć': "c", 'č': "c", 'ď': "d", 'đ': "d",
	'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e", 'ğ': "g", 'ī': "i", 'į': "i",
	'ł': "l", 'ń': "n", 'ň': "n", 'ō': "o", 'ř': "r", 'ś': "s", 'š': "s",
	'ť': "t", 'ū': "u", 'ů': "u", 'ų': "u", 'ź': "z", 'ż': "z", 'ž': "z",
	'ı': "i", 'ş': "s",
}

// RepoRootOf reports the top of the working tree containing path, or an empty
// string when it is not in a git repository.
//
// The answer is the top level rather than the path itself: a session can be
// opened in a subdirectory, and a worktree is added to the repository as a
// whole.
func (i *Instance) RepoRootOf(path string) string {
	return i.RepoRootOn("", path)
}

// RepoRootOn is RepoRootOf on a named machine, for a tab placed on a server.
func (i *Instance) RepoRootOn(serverID, path string) string {
	out, err := i.gitOutputOn(serverID, []string{"-C", path, "rev-parse", "--show-toplevel"})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CreateWorktree adds a worktree for this session and returns where it landed.
//
// The branch is created from the current HEAD of the repository. A name
// already in use is not reused — that would put the session on somebody
// else's branch — so a free one is found by adding a number.
func (i *Instance) CreateWorktree(plan WorktreePlan) (WorktreePlan, error) {
	return i.CreateWorktreeOn("", plan)
}

// CreateWorktreeOn creates the worktree on a named machine.
func (i *Instance) CreateWorktreeOn(serverID string, plan WorktreePlan) (WorktreePlan, error) {
	if plan.RepoRoot == "" {
		return plan, fmt.Errorf("error.worktreeNeedsRepo")
	}

	// A directory that already holds something is not reused either: git
	// would refuse, and silently working somewhere else would be worse.
	free, err := i.freeWorktreeNames(serverID, plan)
	if err != nil {
		return plan, err
	}

	// The parent directory is made where the worktree will be. On a server
	// that is not this filesystem, so os.MkdirAll would make it in the wrong
	// place — and git will not create a worktree under a directory that is not
	// there.
	if serverID == "" {
		if err := os.MkdirAll(filepath.Dir(free.Dir), 0o755); err != nil {
			return free, fmt.Errorf("error.worktreeDirNotCreated|%s", err)
		}
	} else if _, err := i.gitOutputOn(serverID, []string{
		"--exec-path",
	}); err == nil {
		// git is reachable there; make the parent with the shell the tab uses.
		if shell := shellExecutorOn(i.ID, serverID); shell != nil {
			ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
			defer cancel()
			_, stderr, exitCode, runErr := shell.RunShell(ctx, "",
				"mkdir", "-p", "--", pathDir(free.Dir))
			if runErr != nil || exitCode != 0 {
				return free, fmt.Errorf("error.worktreeDirNotCreated|%s",
					strings.TrimSpace(string(stderr)))
			}
		}
	}

	// Checked with git's own validator before it is used.
	//
	// The branch keeps the name as typed, so it can hold anything a person
	// writes. check-ref-format is the authority on what git will take, and
	// asking it here means a name it refuses fails where it was chosen rather
	// than deep inside `worktree add`, which reports it as an opaque failure.
	if _, err := i.gitOutputOn(serverID, []string{
		"check-ref-format", "--branch", free.Branch,
	}); err != nil {
		return free, fmt.Errorf("error.worktreeBranchRefused|%s", free.Branch)
	}

	if _, err := i.gitOutputOn(serverID, []string{
		"-C", free.RepoRoot, "worktree", "add", "-b", free.Branch, free.Dir,
	}); err != nil {
		return free, fmt.Errorf("error.worktreeNotCreated|%s", gitMessage(err))
	}
	return free, nil
}

// freeWorktreeNames finds a directory and a branch that are both available.
//
// Both have to be free together: a branch outlives the worktree it was made
// for, so a second session with the same name would find the directory gone
// but the branch still there, and git refuses to create it again.
func (i *Instance) freeWorktreeNames(serverID string, plan WorktreePlan) (WorktreePlan, error) {
	taken, err := i.existingBranches(serverID, plan.RepoRoot)
	if err != nil {
		return plan, err
	}

	candidate := plan
	for attempt := 2; attempt < 100; attempt++ {
		if !taken[candidate.Branch] && !i.pathExistsOn(serverID, candidate.Dir) {
			return candidate, nil
		}
		candidate.Dir = fmt.Sprintf("%s-%d", plan.Dir, attempt)
		candidate.Branch = fmt.Sprintf("%s-%d", plan.Branch, attempt)
	}
	return plan, fmt.Errorf("error.worktreeNameNotFree|%s", plan.Branch)
}

func (i *Instance) existingBranches(serverID, repoRoot string) (map[string]bool, error) {
	out, err := i.gitOutputOn(serverID, []string{
		"-C", repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads",
	})
	if err != nil {
		return nil, fmt.Errorf("error.worktreeBranchesNotRead|%s", gitMessage(err))
	}
	taken := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			taken[name] = true
		}
	}
	return taken, nil
}

// WorktreeState describes what would be lost by removing a worktree.
type WorktreeState struct {
	// ChangedFiles is the number of modified, added or untracked files.
	ChangedFiles int
	// UnmergedCommits is the number of commits made here that the branch it
	// started from does not have.
	UnmergedCommits int
}

// HasWork reports whether removing this worktree would throw anything away.
func (s WorktreeState) HasWork() bool {
	return s.ChangedFiles > 0 || s.UnmergedCommits > 0
}

// InspectWorktree reports what is in a worktree that has not been saved
// elsewhere.
//
// Commits are counted against the commit the session started from, not
// against an upstream branch: a worktree made here has no upstream until
// somebody pushes it, and asking git for one is an error rather than an empty
// answer. baseSHA empty skips that half rather than guessing.
func (i *Instance) InspectWorktree(dir, baseSHA string) WorktreeState {
	return i.InspectWorktreeOn("", dir, baseSHA)
}

// InspectWorktreeOn is InspectWorktree on a named machine.
func (i *Instance) InspectWorktreeOn(serverID, dir, baseSHA string) WorktreeState {
	var state WorktreeState

	if out, err := i.gitOutputOn(serverID, []string{"-C", dir, "status", "--porcelain"}); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				state.ChangedFiles++
			}
		}
	}

	if baseSHA != "" {
		if out, err := i.gitOutputOn(serverID, []string{
			"-C", dir, "rev-list", "--count", baseSHA + "..HEAD",
		}); err == nil {
			count := strings.TrimSpace(string(out))
			if n := atoiSafe(count); n > 0 {
				state.UnmergedCommits = n
			}
		}
	}
	return state
}

// RemoveWorktree deletes a worktree and the branch it was on.
//
// force is what the user's answer to "there is unsaved work here" becomes.
// Without it git refuses to remove a worktree holding changes, which is the
// behaviour wanted: the refusal is the safety net, not an obstacle.
//
// The branch is removed after the worktree, and its failure is not fatal. A
// branch whose commits went nowhere is worth keeping when the user said to
// keep the work, and a leftover branch costs nothing but a name.
func (i *Instance) RemoveWorktree(repoRoot, dir, branch string, force bool) error {
	return i.RemoveWorktreeOn("", repoRoot, dir, branch, force)
}

// RemoveWorktreeOn removes a worktree on a named machine.
func (i *Instance) RemoveWorktreeOn(serverID, repoRoot, dir, branch string, force bool) error {
	args := []string{"-C", repoRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, dir)

	if _, err := i.gitOutputOn(serverID, args); err != nil {
		return fmt.Errorf("error.worktreeNotRemoved|%s", gitMessage(err))
	}

	if branch != "" && force {
		// -D rather than -d: the branch is unmerged by definition here, and
		// the user has already said the work can go.
		_, _ = i.gitOutputOn(serverID, []string{"-C", repoRoot, "branch", "-D", branch})
	} else if branch != "" {
		// -d refuses an unmerged branch, which is the right default: the
		// worktree was clean, but the commits on it may still matter.
		_, _ = i.gitOutputOn(serverID, []string{"-C", repoRoot, "branch", "-d", branch})
	}
	return nil
}

// gitMessage trims a git failure to its last line, which is the part that says
// what happened; the rest is the command that produced it.
func gitMessage(err error) string {
	text := strings.TrimSpace(err.Error())
	if cut := strings.LastIndex(text, ": "); cut >= 0 {
		text = text[cut+2:]
	}
	return text
}

func atoiSafe(value string) int {
	total := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		total = total*10 + int(r-'0')
	}
	return total
}

// shellExecutorOn returns the shell for a tab's machine, or nil when there is
// no live connection to it.
func shellExecutorOn(sessionID, serverID string) ShellExecutor {
	found, ok := executors.Load(tabExecutorKey(sessionID, serverID))
	if !ok {
		return nil
	}
	shell, isShell := found.(ShellExecutor)
	if !isShell {
		return nil
	}
	return shell
}

// pathDir is filepath.Dir for a path on the machine it names.
//
// A server is Unix whatever this computer is, so filepath.Dir would cut a
// Windows desktop's path at the wrong separator and hand git a directory that
// does not exist there.
func pathDir(value string) string {
	if cut := strings.LastIndex(value, "/"); cut > 0 {
		return value[:cut]
	}
	return filepath.Dir(value)
}

// pathExistsOn reports whether a path is already taken on a machine.
func (i *Instance) pathExistsOn(serverID, path string) bool {
	if serverID == "" {
		_, err := os.Stat(path)
		return err == nil
	}
	shell := shellExecutorOn(i.ID, serverID)
	if shell == nil {
		// Unknown rather than free: claiming a directory on a machine that
		// cannot be asked risks git finding something there.
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	_, _, exitCode, err := shell.RunShell(ctx, "", "test", "-e", path)
	return err == nil && exitCode == 0
}

// discardUnusedWorktree removes a checkout made for something that then failed
// to be created.
//
// Forced, because the only thing in it is what git put there a moment ago:
// there is no work to protect, and refusing would leave the directory behind
// for a tab that does not exist.
func (i *Instance) discardUnusedWorktree(serverID string, plan WorktreePlan) {
	if plan.Dir == "" {
		return
	}
	_ = i.RemoveWorktreeOn(serverID, plan.RepoRoot, plan.Dir, plan.Branch, true)
}
