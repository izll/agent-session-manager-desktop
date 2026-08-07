package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Splitting a unified diff into per-file entries and per-file hunks, so the UI
// can show a file tree and revert a single file or a single hunk instead of
// the whole working tree.

// DiffHunk is one "@@ ... @@" block within a file's diff.
type DiffHunk struct {
	// Header is the literal "@@ -a,b +c,d @@ ..." line.
	Header string `json:"header"`
	// Body holds the hunk's context/added/removed lines, without the header.
	Body string `json:"body"`
	// Index is the hunk's position within its file, starting at 0.
	Index   int `json:"index"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	// Patch is this hunk alone as a standalone patch, ready to be handed back
	// for reverting. Sending it round-trip means we undo exactly what was on
	// screen, even if the file changed in between.
	Patch string `json:"patch"`
}

// DiffFile is one file's worth of a unified diff.
type DiffFile struct {
	// Path is the file's current path, relative to the repository root.
	Path string `json:"path"`
	// OldPath differs from Path only for renames.
	OldPath string `json:"oldPath"`
	// Status is one of "modified", "added", "deleted", "renamed".
	Status string `json:"status"`
	// Header is everything before the first hunk ("diff --git", index, ---,
	// +++ lines). Reverting re-assembles a patch from this plus hunks.
	Header  string     `json:"header"`
	Hunks   []DiffHunk `json:"hunks"`
	Added   int        `json:"added"`
	Removed int        `json:"removed"`
	// Binary marks files git reported without textual hunks; they can be
	// reverted whole but not per hunk.
	Binary bool `json:"binary"`
}

// ParseDiffFiles splits a unified diff into per-file entries.
//
// It deliberately keeps the original header text verbatim: reverting works by
// feeding a reconstructed patch back to `git apply -R`, which needs git's own
// header lines to identify the target.
func ParseDiffFiles(content string) []DiffFile {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	var files []DiffFile
	var cur *DiffFile
	var hunk *DiffHunk
	inHeader := false

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			old, new := parseGitHeaderPaths(line)
			cur = &DiffFile{Path: new, OldPath: old, Status: "modified"}
			cur.Header = line + "\n"
			inHeader = true

		case cur == nil:
			// Preamble before the first file (or malformed input): ignore.

		case strings.HasPrefix(line, "@@"):
			flushHunk()
			inHeader = false
			hunk = &DiffHunk{Header: line, Index: len(cur.Hunks)}

		case inHeader:
			cur.Header += line + "\n"
			switch {
			case strings.HasPrefix(line, "new file mode"):
				cur.Status = "added"
			case strings.HasPrefix(line, "deleted file mode"):
				cur.Status = "deleted"
			case strings.HasPrefix(line, "rename from "):
				cur.Status = "renamed"
				cur.OldPath = strings.TrimPrefix(line, "rename from ")
			case strings.HasPrefix(line, "rename to "):
				cur.Status = "renamed"
				cur.Path = strings.TrimPrefix(line, "rename to ")
			case strings.HasPrefix(line, "Binary files "),
				strings.HasPrefix(line, "GIT binary patch"):
				cur.Binary = true
			}

		case hunk != nil:
			hunk.Body += line + "\n"
			if len(line) > 0 {
				switch line[0] {
				case '+':
					hunk.Added++
					cur.Added++
				case '-':
					hunk.Removed++
					cur.Removed++
				}
			}
		}
	}
	flushFile()

	// Fill in each hunk's standalone patch now that its file header is known.
	for fi := range files {
		for hi := range files[fi].Hunks {
			if patch, err := BuildHunkPatch(files[fi], hi); err == nil {
				files[fi].Hunks[hi].Patch = patch
			}
		}
	}
	return files
}

// parseGitHeaderPaths pulls the a/… and b/… paths out of a "diff --git" line.
// Paths containing spaces are ambiguous in this format; git quotes those, and
// the quoted form is handled by falling back to the +++ / --- lines.
func parseGitHeaderPaths(line string) (old, new string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	// Common case: "a/path b/path" with no spaces in either path.
	if i := strings.Index(rest, " b/"); i >= 0 {
		old = strings.TrimPrefix(rest[:i], "a/")
		new = strings.TrimPrefix(rest[i+1:], "b/")
		return strings.Trim(old, `"`), strings.Trim(new, `"`)
	}
	fields := strings.Fields(rest)
	if len(fields) == 2 {
		return strings.TrimPrefix(fields[0], "a/"), strings.TrimPrefix(fields[1], "b/")
	}
	return "", strings.TrimPrefix(rest, "a/")
}

// BuildHunkPatch reassembles a patch containing exactly one hunk of one file,
// suitable for `git apply -R`.
func BuildHunkPatch(file DiffFile, hunkIndex int) (string, error) {
	if hunkIndex < 0 || hunkIndex >= len(file.Hunks) {
		return "", fmt.Errorf("hunk %d out of range for %s", hunkIndex, file.Path)
	}
	h := file.Hunks[hunkIndex]
	body := h.Body
	// A patch must end with a newline or git rejects it.
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return file.Header + h.Header + "\n" + body, nil
}

// RevertHunk undoes a single hunk in the working tree by reverse-applying it.
//
// The caller passes the patch text it displayed, rather than a hunk index we
// would re-derive here: if the file changed since the diff was shown, the
// hunk at a given index is no longer the change the user asked to undo.
// Git then verifies the context still matches and refuses rather than
// corrupting a file that moved on.
func (i *Instance) RevertHunk(patch string) error {
	if !i.isGitRepo() {
		return fmt.Errorf("not a git repository")
	}
	if strings.TrimSpace(patch) == "" {
		return fmt.Errorf("no change given to revert")
	}
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}

	cmd, cancel := GitCommandTimed("-C", i.Path, "apply", "--reverse", "-")
	defer cancel()
	cmd.Stdin = strings.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("could not revert that change — the file may have "+
			"changed since: %s", msg)
	}
	return nil
}

// RevertFile discards all pending changes to one file.
//
// Untracked files are deleted, because there is no committed version to
// restore; everything else is restored from baseRef (or HEAD).
func (i *Instance) RevertFile(path string, baseRef string) error {
	if !i.isGitRepo() {
		return fmt.Errorf("not a git repository")
	}
	if err := i.validateRepoPath(path); err != nil {
		return err
	}

	if i.isUntracked(path) {
		full := filepath.Join(i.Path, filepath.FromSlash(path))
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not delete %s: %w", path, err)
		}
		return nil
	}

	ref := baseRef
	if ref == "" {
		ref = "HEAD"
	}
	cmd, cancel := GitCommandTimed("-C", i.Path, "checkout", ref, "--", path)
	defer cancel()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("could not restore %s: %s", path, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateRepoPath rejects paths that would escape the repository. The path
// comes from the frontend, so it is not trusted.
func (i *Instance) validateRepoPath(path string) error {
	if path == "" {
		return fmt.Errorf("no file given")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be relative to the repository")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes the repository")
	}
	return nil
}

// isUntracked reports whether git has no record of this path.
func (i *Instance) isUntracked(path string) bool {
	cmd, cancel := GitCommandTimed("-C", i.Path, "ls-files", "--error-unmatch", "--", path)
	defer cancel()
	return cmd.Run() != nil
}

// diffFiles returns the current diff split per file.
func (i *Instance) diffFiles(baseRef string) ([]DiffFile, error) {
	stats := i.getDiff(baseRef)
	if stats.Error != nil {
		return nil, stats.Error
	}
	return ParseDiffFiles(stats.Content), nil
}

// IsGitRepo reports whether this session's directory is inside a git
// repository. The UI uses it to decide whether a diff view makes any sense.
func (i *Instance) IsGitRepo() bool {
	return i.isGitRepo()
}

// GetSessionDiffFiles returns the per-file diff since the session started.
func (i *Instance) GetSessionDiffFiles() ([]DiffFile, error) {
	if i.BaseCommitSHA == "" {
		return nil, fmt.Errorf("no base commit (not a git repo or session started before tracking)")
	}
	return i.diffFiles(i.BaseCommitSHA)
}

// GetFullDiffFiles returns the per-file uncommitted diff.
func (i *Instance) GetFullDiffFiles() ([]DiffFile, error) {
	return i.diffFiles("")
}

// DiffFileSummary is one changed file without its contents.
//
// The diff view lists files first and loads a file's hunks when it is opened.
// Listing used to mean running the whole diff and parsing it, which is fine
// until something writes a lot of files — a build dropping its output into the
// tree produced a diff large enough that the view never finished loading.
// numstat costs the same whether a file changed by one line or fifty thousand.
type DiffFileSummary struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	// Binary marks files git reports as binary (numstat gives "-" for those).
	Binary bool `json:"binary"`
}

// diffFileSummaries lists the changed files and their line counts, without
// reading any file's contents.
func (i *Instance) diffFileSummaries(baseRef string) ([]DiffFileSummary, error) {
	gitEnv, cleanup, err := i.diffIndexEnv()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := []string{"-C", i.Path, "--no-pager", "diff", "--numstat", "-z", "--find-renames"}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	cmd, cancel := GitCommandTimed(args...)
	defer cancel()
	cmd.Env = gitEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat failed: %w", err)
	}
	files := parseNumstatZ(string(out))

	// numstat gives line counts and paths but says nothing about what happened
	// to a file: a new file and an edited one are both just "<added> <removed>
	// <path>", so everything came out labelled "modified". name-status is the
	// side that knows — A, M, D, R — so the two are read together.
	statusArgs := []string{"-C", i.Path, "--no-pager", "diff", "--name-status", "-z", "--find-renames"}
	if baseRef != "" {
		statusArgs = append(statusArgs, baseRef)
	}
	statusCmd, cancelStatus := GitCommandTimed(statusArgs...)
	defer cancelStatus()
	statusCmd.Env = gitEnv
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil {
		// The counts are still useful without it; better a list labelled
		// "modified" than no list at all.
		return files, nil
	}
	applyNameStatus(files, string(statusOut))
	return files, nil
}

// applyNameStatus fills in each file's status from `git diff --name-status -z`.
//
// Records are "<letter>\x00<path>", except renames and copies, which carry a
// similarity score on the letter (R100) and two paths. Matched by path rather
// than by position: the two commands agree on order in practice, but a mismatch
// would silently label the wrong files.
func applyNameStatus(files []DiffFileSummary, out string) {
	byPath := make(map[string]*DiffFileSummary, len(files))
	for idx := range files {
		byPath[files[idx].Path] = &files[idx]
	}

	fields := strings.Split(out, "\x00")
	for idx := 0; idx < len(fields); idx++ {
		letter := fields[idx]
		if letter == "" {
			continue
		}
		var path string
		switch letter[0] {
		case 'R', 'C':
			// Old path, then new path; the new one is what the list shows.
			if idx+2 >= len(fields) {
				return
			}
			path = fields[idx+2]
			idx += 2
		default:
			if idx+1 >= len(fields) {
				return
			}
			path = fields[idx+1]
			idx++
		}
		f, ok := byPath[path]
		if !ok {
			continue
		}
		switch letter[0] {
		case 'A':
			f.Status = "added"
		case 'D':
			f.Status = "deleted"
		case 'R':
			f.Status = "renamed"
		case 'C':
			f.Status = "added" // a copy is a new file as far as reviewing goes
		default:
			f.Status = "modified"
		}
	}
}

// parseNumstatZ reads `git diff --numstat -z` output.
//
// The -z form is used because a path with a space, a quote or a newline in it
// is not distinguishable in the plain output — git quotes those, and the quoted
// form then has to be unescaped. With -z the fields are NUL-separated and the
// paths are literal.
// ParseNumstatZ is parseNumstatZ for callers outside the package: reading a
// commit's file list needs the same parsing as reading a working tree's.
func ParseNumstatZ(out string) []DiffFileSummary { return parseNumstatZ(out) }

func parseNumstatZ(out string) []DiffFileSummary {
	fields := strings.Split(out, "\x00")
	var files []DiffFileSummary

	for idx := 0; idx < len(fields); idx++ {
		record := fields[idx]
		if record == "" {
			continue
		}
		// "<added>\t<removed>\t<path>", or for a rename "<added>\t<removed>\t"
		// followed by two more NUL-separated fields: old path, then new path.
		parts := strings.Split(record, "\t")
		if len(parts) < 3 {
			continue
		}
		f := DiffFileSummary{Status: "modified"}
		// git writes "-" for both counts on a binary file.
		if parts[0] == "-" || parts[1] == "-" {
			f.Binary = true
		} else {
			f.Added, _ = strconv.Atoi(parts[0])
			f.Removed, _ = strconv.Atoi(parts[1])
		}

		if parts[2] == "" && idx+2 < len(fields) {
			f.OldPath = fields[idx+1]
			f.Path = fields[idx+2]
			f.Status = "renamed"
			idx += 2
		} else {
			f.Path = parts[2]
		}
		files = append(files, f)
	}
	return files
}

// diffForFile returns the diff of a single file, parsed into hunks.
//
// Scoped to one path so opening a file costs what that file costs, rather than
// what the whole tree costs.
// wholeFileContext is the -U value that makes git emit the entire file around
// the changes.
//
// git has no "all of it" switch, so this is a number chosen to exceed any file
// worth reading in a diff view. Larger than the biggest source file anyone
// reviews line by line, and small enough that a pathological input — a
// generated bundle, a vendored blob — is still bounded rather than unrolled in
// full.
const wholeFileContext = 100000

func (i *Instance) diffForFile(baseRef, path string, contextLines int) (*DiffFile, error) {
	if path == "" {
		return nil, fmt.Errorf("no file given")
	}
	gitEnv, cleanup, err := i.diffIndexEnv()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := []string{"-C", i.Path, "--no-pager", "diff", "--find-renames"}
	if contextLines > 0 {
		// Whole-file view: the unchanged lines around each change come from git
		// rather than from reading the file separately, so both sides stay
		// consistent with what the diff describes — a file edited between the
		// two reads would otherwise show changes against content it never had.
		args = append(args, fmt.Sprintf("-U%d", contextLines))
	}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	// "--" keeps a path that looks like a revision from being read as one.
	args = append(args, "--", path)

	cmd, cancel := GitCommandTimed(args...)
	defer cancel()
	cmd.Env = gitEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed for %s: %w", path, err)
	}

	parsed := ParseDiffFiles(string(out))
	if len(parsed) == 0 {
		// The file was listed but has no diff now: it changed back, or was
		// reverted between listing and opening. An empty result is honest.
		return nil, nil
	}
	return &parsed[0], nil
}

// GetSessionDiffFileList lists files changed since the session started.
func (i *Instance) GetSessionDiffFileList() ([]DiffFileSummary, error) {
	if i.BaseCommitSHA == "" {
		return nil, fmt.Errorf("no base commit (not a git repo or session started before tracking)")
	}
	return i.diffFileSummaries(i.BaseCommitSHA)
}

// GetFullDiffFileList lists files with uncommitted changes.
func (i *Instance) GetFullDiffFileList() ([]DiffFileSummary, error) {
	return i.diffFileSummaries("")
}

// GetSessionDiffForFile returns one file's diff since the session started.
//
// wholeFile asks for the entire file around the changes, rather than the few
// lines of context a diff normally carries.
func (i *Instance) GetSessionDiffForFile(path string, wholeFile bool) (*DiffFile, error) {
	if i.BaseCommitSHA == "" {
		return nil, fmt.Errorf("no base commit (not a git repo or session started before tracking)")
	}
	return i.diffForFile(i.BaseCommitSHA, path, contextFor(wholeFile))
}

// GetFullDiffForFile returns one file's uncommitted diff.
func (i *Instance) GetFullDiffForFile(path string, wholeFile bool) (*DiffFile, error) {
	return i.diffForFile("", path, contextFor(wholeFile))
}

// contextFor turns the caller's intent into a -U value; zero leaves git on its
// own default of three lines.
func contextFor(wholeFile bool) int {
	if wholeFile {
		return wholeFileContext
	}
	return 0
}
