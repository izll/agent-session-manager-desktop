package session

import (
	"fmt"
	"os"
	"path/filepath"
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

	cmd := GitCommand("-C", i.Path, "apply", "--reverse", "-")
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
	cmd := GitCommand("-C", i.Path, "checkout", ref, "--", path)
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
	cmd := GitCommand("-C", i.Path, "ls-files", "--error-unmatch", "--", path)
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
