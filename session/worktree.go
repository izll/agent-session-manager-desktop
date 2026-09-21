package session

import (
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
	slug := worktreeSlug(sessionName)
	parent := filepath.Dir(repoRoot)
	base := filepath.Base(repoRoot)
	return WorktreePlan{
		Dir:      filepath.Join(parent, base+worktreeDirSuffix, slug),
		Branch:   WorktreeBranchPrefix + slug,
		RepoRoot: repoRoot,
	}
}

// worktreeSlug turns a session name into something usable as a directory and a
// branch component.
//
// Git refuses a range of characters in a ref name, and a directory has its own
// rules on each platform, so this keeps to letters, digits, dash and
// underscore and lets everything else become a dash. An empty result would
// produce the bare prefix, which is not a valid ref, so it falls back.
func worktreeSlug(name string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
			lastDash = false
		default:
			// Runs of punctuation collapse, and a name cannot start with a
			// dash: git rejects a ref component beginning with one.
			if !lastDash && out.Len() > 0 {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
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

// RepoRootOf reports the top of the working tree containing path, or an empty
// string when it is not in a git repository.
//
// The answer is the top level rather than the path itself: a session can be
// opened in a subdirectory, and a worktree is added to the repository as a
// whole.
func (i *Instance) RepoRootOf(path string) string {
	out, err := i.gitOutput([]string{"-C", path, "rev-parse", "--show-toplevel"}, nil)
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
	if plan.RepoRoot == "" {
		return plan, fmt.Errorf("error.worktreeNeedsRepo")
	}

	// A directory that already holds something is not reused either: git
	// would refuse, and silently working somewhere else would be worse.
	free, err := i.freeWorktreeNames(plan)
	if err != nil {
		return plan, err
	}

	if err := os.MkdirAll(filepath.Dir(free.Dir), 0o755); err != nil {
		return free, fmt.Errorf("error.worktreeDirNotCreated|%s", err)
	}

	if _, err := i.gitOutput([]string{
		"-C", free.RepoRoot, "worktree", "add", "-b", free.Branch, free.Dir,
	}, nil); err != nil {
		return free, fmt.Errorf("error.worktreeNotCreated|%s", gitMessage(err))
	}
	return free, nil
}

// freeWorktreeNames finds a directory and a branch that are both available.
//
// Both have to be free together: a branch outlives the worktree it was made
// for, so a second session with the same name would find the directory gone
// but the branch still there, and git refuses to create it again.
func (i *Instance) freeWorktreeNames(plan WorktreePlan) (WorktreePlan, error) {
	taken, err := i.existingBranches(plan.RepoRoot)
	if err != nil {
		return plan, err
	}

	candidate := plan
	for attempt := 2; attempt < 100; attempt++ {
		_, dirExists := os.Stat(candidate.Dir)
		if !taken[candidate.Branch] && os.IsNotExist(dirExists) {
			return candidate, nil
		}
		candidate.Dir = fmt.Sprintf("%s-%d", plan.Dir, attempt)
		candidate.Branch = fmt.Sprintf("%s-%d", plan.Branch, attempt)
	}
	return plan, fmt.Errorf("error.worktreeNameNotFree|%s", plan.Branch)
}

func (i *Instance) existingBranches(repoRoot string) (map[string]bool, error) {
	out, err := i.gitOutput([]string{
		"-C", repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads",
	}, nil)
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
	var state WorktreeState

	if out, err := i.gitOutput([]string{"-C", dir, "status", "--porcelain"}, nil); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				state.ChangedFiles++
			}
		}
	}

	if baseSHA != "" {
		if out, err := i.gitOutput([]string{
			"-C", dir, "rev-list", "--count", baseSHA + "..HEAD",
		}, nil); err == nil {
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
	args := []string{"-C", repoRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, dir)

	if _, err := i.gitOutput(args, nil); err != nil {
		return fmt.Errorf("error.worktreeNotRemoved|%s", gitMessage(err))
	}

	if branch != "" && force {
		// -D rather than -d: the branch is unmerged by definition here, and
		// the user has already said the work can go.
		_, _ = i.gitOutput([]string{"-C", repoRoot, "branch", "-D", branch}, nil)
	} else if branch != "" {
		// -d refuses an unmerged branch, which is the right default: the
		// worktree was clean, but the commits on it may still matter.
		_, _ = i.gitOutput([]string{"-C", repoRoot, "branch", "-d", branch}, nil)
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
