package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Deleting a file from the file browser, in a way that can be taken back.
//
// The file is not unlinked but moved into a holding directory outside the
// tree. To everything else — the agent, git, an editor — it is gone at once,
// which is what the user asked for; it is only actually removed once the undo
// offer is over (DiscardDeletedFile). Deferring the delete itself instead would
// leave the file on disk while the interface says it is not there.

// ErrRestoreOccupied is returned when a deleted file cannot be put back
// because something with its name has appeared in its place.
var ErrRestoreOccupied = errors.New("a file with this name exists again")

// DeletedFile is a file moved out of the tree and held for a possible undo.
type DeletedFile struct {
	// Original is the absolute path the file had.
	Original string
	// Held is where it is kept, inside its own directory under the holding
	// directory, under its own name.
	Held string
}

// DeleteFileForBrowse moves one file of the browsed tree into holdDir.
//
// Only a file or a symlink is taken, never a directory. A symlink is moved as
// the link itself: its target, which may be outside the tree, is untouched.
// The containment check runs on the resolved parent, as strictly as
// resolveBrowsePath; the leaf is deliberately not resolved, or deleting a link
// would delete what it points at.
func (i *Instance) DeleteFileForBrowse(rel, holdDir string) (*DeletedFile, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("path must be relative to the session directory")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("no file given")
	}
	parentRel, leaf := filepath.Split(clean)
	if leaf == "" || leaf == "." || leaf == ".." {
		return nil, fmt.Errorf("no file given")
	}
	parentAbs, _, err := i.resolveBrowsePath(filepath.ToSlash(filepath.Clean(parentRel)))
	if err != nil {
		return nil, err
	}
	original := filepath.Join(parentAbs, leaf)
	info, err := os.Lstat(original)
	if err != nil {
		return nil, fmt.Errorf("could not delete %s: %w", displayPath(rel), err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", displayPath(rel))
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("%s is not a regular file", displayPath(rel))
	}

	if err := os.MkdirAll(holdDir, 0o700); err != nil {
		return nil, fmt.Errorf("could not prepare the deleted files folder: %w", err)
	}
	// A directory of its own per file keeps the name as it was, so two files
	// of the same name deleted in a row do not collide.
	slot, err := os.MkdirTemp(holdDir, "file-")
	if err != nil {
		return nil, fmt.Errorf("could not prepare the deleted files folder: %w", err)
	}
	held := filepath.Join(slot, leaf)
	if err := moveFile(original, held); err != nil {
		os.RemoveAll(slot)
		return nil, fmt.Errorf("could not delete %s: %w", displayPath(rel), err)
	}
	return &DeletedFile{Original: original, Held: held}, nil
}

// RestoreDeletedFile puts a deleted file back where it was.
//
// It never overwrites: a file created under the same name since is the
// user's or the agent's, and newer than the one being restored.
func RestoreDeletedFile(d *DeletedFile) error {
	if _, err := os.Lstat(d.Original); err == nil {
		return ErrRestoreOccupied
	} else if !os.IsNotExist(err) {
		return err
	}
	if info, err := os.Stat(filepath.Dir(d.Original)); err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s is gone", filepath.Dir(d.Original))
	}
	if err := moveFileNoClobber(d.Held, d.Original); err != nil {
		if os.IsExist(err) {
			return ErrRestoreOccupied
		}
		return err
	}
	os.RemoveAll(filepath.Dir(d.Held))
	return nil
}

// DiscardDeletedFile removes a held file for good.
func DiscardDeletedFile(d *DeletedFile) error {
	return os.RemoveAll(filepath.Dir(d.Held))
}

// moveFile moves src to dst. A rename where it can be; across filesystems —
// the holding directory is in the user's config, the project may be on
// another disk — a copy followed by removing the source.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFileNoClobber(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// moveFileNoClobber moves src to dst, failing with an os.IsExist error when
// dst exists. The check and the move are one step — a hard link, or a copy
// opened with O_EXCL — so a file appearing in between is not overwritten.
func moveFileNoClobber(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		if err := os.Link(src, dst); err == nil {
			return os.Remove(src)
		} else if os.IsExist(err) {
			return err
		}
		// Across filesystems, or where hard links are not supported: copy.
	}
	if err := copyFileNoClobber(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// copyFileNoClobber copies a regular file or a symlink, keeping a file's
// permissions and modification time. It fails if dst exists.
func copyFileNoClobber(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

// DeletedFilesDir is where files deleted from the file browser are held
// while their deletion can still be undone.
func (s *Storage) DeletedFilesDir() string {
	return filepath.Join(s.configDir, "deleted-files")
}
