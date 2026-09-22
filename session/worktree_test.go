package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A session's own worktree is what lets two agents work the same project at
// once without editing the same files. These cover the naming, which has to
// satisfy both git and the filesystem, and the lifecycle against a real
// repository.

// The branch keeps the name as typed; only what git actually refuses is
// replaced.
//
// git's ref rules are a blocklist of ASCII metacharacters, not an allowlist of
// letters — measured against check-ref-format, "hibajavítás" is a perfectly
// legal branch name. Reducing it to ASCII was an assumption, and it turned the
// name into something nobody would recognise.
func TestTheBranchKeepsTheNameAsTyped(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"hibajavítás", "hibajavítás"},
		{"árvíztűrő", "árvíztűrő"},
		{"Ünnepi Kiadás", "Ünnepi-Kiadás"}, // only the space had to go
		{"żółw", "żółw"},
		{"日本語", "日本語"},
		{"Fix the parser", "Fix-the-parser"},
		{"feature/login", "feature/login"}, // a slash is legal inside a ref
		{"a:b", "a-b"},
		{"a~b", "a-b"},
		{"-leading", "leading"},
		{"trailing.", "trailing"},
		{"", "session"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := worktreeBranchName(testCase.name); got != testCase.want {
				t.Errorf("worktreeBranchName(%q) = %q, want %q",
					testCase.name, got, testCase.want)
			}
		})
	}
}

// The directory is a filesystem concern where the branch is not, so it is
// reduced to ASCII — by transliterating, not by stripping. Deleting the
// accents gives "hibajavts" and replacing them gives "hibajav-t-s"; neither
// reads as the session it belongs to.
func TestTheDirectoryIsTransliteratedNotStripped(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"hibajavítás", "hibajavitas"},
		{"árvíztűrő tükörfúrógép", "arvizturo-tukorfurogep"},
		{"Ünnepi Kiadás", "unnepi-kiadas"},
		{"Straßen fix", "strassen-fix"}, // ß is one letter that becomes two
		{"Łódź", "lodz"},                // ł does not decompose; it needs a table
		{"Fix the parser", "fix-the-parser"},
		{"feature/login", "feature-login"},
		{"日本語", "session"}, // nothing to transliterate; the branch still has it
		{"", "session"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := worktreeDirName(testCase.name); got != testCase.want {
				t.Errorf("worktreeDirName(%q) = %q, want %q",
					testCase.name, got, testCase.want)
			}
		})
	}
}

// git rejects a ref component that begins with a dash or contains a range of
// punctuation, and a branch that cannot be created makes the whole feature
// fail at the last step.
func TestTheBranchNameIsAcceptableToGit(t *testing.T) {
	repo := newTestRepo(t)

	for _, name := range []string{
		"hibajavítás", "árvíztűrő", "feature/login", "-dash", "a...b",
		"Ünnepi Kiadás", "日本語", "a:b", "trailing.", "",
	} {
		plan := PlanWorktree(repo, name)
		// check-ref-format is git's own answer to "is this a legal branch
		// name", which beats a rule copied out of the documentation.
		cmd := exec.Command("git", "check-ref-format", "--branch", plan.Branch)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("session %q produced branch %q, which git rejects: %s",
				name, plan.Branch, strings.TrimSpace(string(out)))
		}
	}
}

// The worktree goes beside the repository, not inside it. Nested, it shows up
// as untracked content in the parent, and every status there carries it.
func TestTheWorktreeIsNotInsideTheRepository(t *testing.T) {
	repo := newTestRepo(t)
	plan := PlanWorktree(repo, "work")

	if strings.HasPrefix(filepath.Clean(plan.Dir), filepath.Clean(repo)+string(filepath.Separator)) {
		t.Errorf("worktree %q is inside the repository %q; it would show as "+
			"untracked content in every status", plan.Dir, repo)
	}
}

func TestCreatingAndRemovingAWorktree(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "wt-life"}

	plan, err := inst.CreateWorktree(PlanWorktree(repo, "the work"))
	if err != nil {
		t.Fatalf("creating the worktree failed: %v", err)
	}
	if _, err := os.Stat(plan.Dir); err != nil {
		t.Fatalf("the worktree directory is not there: %v", err)
	}

	// It is a checkout of the same repository, on its own branch.
	branch := gitIn(t, plan.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != plan.Branch {
		t.Errorf("worktree is on %q, want %q", branch, plan.Branch)
	}

	// Clean, so it goes without force.
	if err := inst.RemoveWorktree(repo, plan.Dir, plan.Branch, false); err != nil {
		t.Fatalf("removing a clean worktree failed: %v", err)
	}
	if _, err := os.Stat(plan.Dir); !os.IsNotExist(err) {
		t.Errorf("the worktree directory survived removal")
	}
}

// A branch outlives the worktree it was made for. A second session of the same
// name would otherwise find the directory free but the branch taken, and git
// refuses to create it again — so both have to be free together.
func TestASecondSessionOfTheSameNameGetsItsOwnNames(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "wt-twice"}

	first, err := inst.CreateWorktree(PlanWorktree(repo, "same name"))
	if err != nil {
		t.Fatalf("first worktree: %v", err)
	}
	second, err := inst.CreateWorktree(PlanWorktree(repo, "same name"))
	if err != nil {
		t.Fatalf("second worktree with the same session name: %v", err)
	}

	if second.Dir == first.Dir {
		t.Errorf("both sessions were given the same directory %q", first.Dir)
	}
	if second.Branch == first.Branch {
		t.Errorf("both sessions were given the same branch %q", first.Branch)
	}
}

// Removing a worktree must not quietly throw away work. git refuses without
// --force, and that refusal is the safety net.
func TestUnsavedWorkIsNotRemovedWithoutForce(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "wt-dirty"}

	plan, err := inst.CreateWorktree(PlanWorktree(repo, "dirty"))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plan.Dir, "new.txt"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := inst.RemoveWorktree(repo, plan.Dir, plan.Branch, false); err == nil {
		t.Error("a worktree holding unsaved work was removed without being forced")
	}
	if _, err := os.Stat(plan.Dir); err != nil {
		t.Errorf("the worktree was removed anyway: %v", err)
	}

	// And with the user's consent it goes.
	if err := inst.RemoveWorktree(repo, plan.Dir, plan.Branch, true); err != nil {
		t.Fatalf("forced removal failed: %v", err)
	}
	if _, err := os.Stat(plan.Dir); !os.IsNotExist(err) {
		t.Error("forced removal left the directory behind")
	}
}

// What the user is asked before losing a worktree has to be true: the counts
// are what the confirmation shows.
func TestInspectReportsWhatWouldBeLost(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "wt-inspect"}

	plan, err := inst.CreateWorktree(PlanWorktree(repo, "inspect"))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	base := gitIn(t, plan.Dir, "rev-parse", "HEAD")

	if state := inst.InspectWorktree(plan.Dir, base); state.HasWork() {
		t.Errorf("a fresh worktree reports work to lose: %+v", state)
	}

	// An uncommitted file.
	if err := os.WriteFile(filepath.Join(plan.Dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := inst.InspectWorktree(plan.Dir, base)
	if state.ChangedFiles != 1 {
		t.Errorf("changed files = %d, want 1", state.ChangedFiles)
	}

	// A commit that exists nowhere else. Counted against the commit the
	// session started from — a worktree made here has no upstream, and asking
	// git for one is an error rather than an empty answer.
	gitIn(t, plan.Dir, "add", "-A")
	gitIn(t, plan.Dir, "commit", "-m", "local work")
	state = inst.InspectWorktree(plan.Dir, base)
	if state.UnmergedCommits != 1 {
		t.Errorf("unmerged commits = %d, want 1", state.UnmergedCommits)
	}
	if !state.HasWork() {
		t.Error("a worktree with a commit of its own reports nothing to lose")
	}
}

// A directory outside a repository cannot have a worktree, and must say so
// rather than failing somewhere later.
func TestADirectoryWithNoRepositoryIsRefused(t *testing.T) {
	inst := &Instance{ID: "wt-norepo"}
	if root := inst.RepoRootOf(t.TempDir()); root != "" {
		t.Errorf("a plain directory was reported as a repository: %q", root)
	}
	if _, err := inst.CreateWorktree(WorktreePlan{}); err == nil {
		t.Error("a worktree was created with no repository")
	}
}

// A session opened in a subdirectory belongs to the repository as a whole.
func TestTheRepositoryRootIsFoundFromASubdirectory(t *testing.T) {
	repo := newTestRepo(t)
	sub := filepath.Join(repo, "deep", "inside")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{ID: "wt-sub"}
	got := inst.RepoRootOf(sub)
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}

	// Compared through filepath.Clean, because git answers with forward
	// slashes on Windows — "D:/a/tmp/repo" where filepath builds
	// "D:\a\tmp\repo". The two name the same directory and the Windows
	// filepath functions accept either, which this test asserted away by
	// comparing the strings: it failed there and only there.
	if filepath.Clean(got) != filepath.Clean(repo) {
		t.Errorf("repository root from a subdirectory = %q, want %q", got, repo)
	}
}

// --- helpers ---

func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "first")

	// Resolved, because git reports the resolved path and macOS puts temporary
	// directories behind a symlink — comparing the two otherwise fails there
	// and nowhere else.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// --- a tab's own worktree ---

// Tabs default to the session's directory, so two agents in one session edit
// the same files. That is the case a worktree exists for, and the commoner
// one: several tabs in a session is what tabs are for.
func TestATabCanHaveAWorktreeOfItsOwn(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "tab-wt", Name: "session", Path: repo}

	plan, err := inst.CreateWorktreeOn("", PlanWorktreeNamed(repo, "db work", ""))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if plan.Dir == repo {
		t.Error("the tab was given the session's own directory")
	}
	if branch := gitIn(t, plan.Dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != plan.Branch {
		t.Errorf("worktree is on %q, want %q", branch, plan.Branch)
	}
}

// A branch the user typed is used as they typed it. They had a reason for the
// name, and the prefix is not forced onto it either.
func TestAChosenBranchNameIsKept(t *testing.T) {
	repo := newTestRepo(t)

	plan := PlanWorktreeNamed(repo, "the tab name", "feature/login")
	if plan.Branch != "feature/login" {
		t.Errorf("branch = %q, want the name as typed", plan.Branch)
	}
	// The directory follows the branch, so the two read as the same thing.
	if !strings.HasSuffix(plan.Dir, "feature-login") {
		t.Errorf("directory %q does not follow the chosen branch", plan.Dir)
	}

	// Empty still derives one from the name, which is what the dialog offers
	// before anybody edits the field.
	derived := PlanWorktreeNamed(repo, "the tab name", "")
	if derived.Branch != WorktreeBranchPrefix+"the-tab-name" {
		t.Errorf("derived branch = %q", derived.Branch)
	}
}

// What git refuses is still cleaned out of a name the user typed, or the
// worktree fails at the last step with an opaque error.
func TestAChosenBranchIsStillMadeLegal(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "tab-legal"}

	plan, err := inst.CreateWorktreeOn("", PlanWorktreeNamed(repo, "tab", "my branch"))
	if err != nil {
		t.Fatalf("a branch with a space in it was refused outright: %v", err)
	}
	if strings.Contains(plan.Branch, " ") {
		t.Errorf("branch %q kept a space, which git does not allow", plan.Branch)
	}
}

// A worktree made for a tab that then fails to be created must not be left on
// the user's disk.
func TestAWorktreeIsNotLeftBehindWhenTheTabFails(t *testing.T) {
	repo := newTestRepo(t)
	inst := &Instance{ID: "tab-fail"}

	plan, err := inst.CreateWorktreeOn("", PlanWorktreeNamed(repo, "doomed", ""))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	inst.discardUnusedWorktree("", plan)

	if _, err := os.Stat(plan.Dir); !os.IsNotExist(err) {
		t.Error("the checkout outlived the tab it was made for")
	}
	branches := gitIn(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.Contains(branches, plan.Branch) {
		t.Errorf("the branch outlived the tab: %s", branches)
	}
}
