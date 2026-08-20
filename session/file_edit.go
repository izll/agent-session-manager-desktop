package session

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Editing files inside a session's working directory.
//
// The hard requirement here is that opening a file and saving it back
// unmodified produces a BYTE-IDENTICAL file. A text editor that quietly
// rewrites line endings, drops a BOM or adds a final newline turns a one-line
// fix into a whole-file diff, and in a directory an AI agent is also writing to
// that is not a cosmetic problem.
//
// The guarantee is structural rather than remembered: the bytes on disk are
// decomposed into (editable text, FileShape) on read, and recomposed by the
// inverse function on save. Everything the editor cannot represent — the BOM,
// the line-ending convention, the presence of a final newline — lives in the
// FileShape and never passes through the textarea, so no editing action can
// disturb it. decodeForEdit/encodeFromEdit are exact inverses, which is what
// TestEditRoundTripIsByteExact pins down.

// utf8BOM is the byte sequence some Windows tools prefix to UTF-8 files. It is
// invisible, so an editor that drops or duplicates it produces a diff the user
// cannot see the cause of.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// LineEnding is the convention a file uses.
type LineEnding string

const (
	// LineEndingLF is the Unix convention, and what an empty or single-line
	// file is assumed to use.
	LineEndingLF LineEnding = "lf"
	// LineEndingCRLF is the Windows convention.
	LineEndingCRLF LineEnding = "crlf"
	// LineEndingMixed marks a file that uses both. Such a file is never
	// normalised — see FileShape.Mixed.
	LineEndingMixed LineEnding = "mixed"
)

// FileShape records everything about a file's byte layout that the editor's
// text cannot express. It is produced by the read, echoed back by the frontend
// untouched, and consumed by the save.
type FileShape struct {
	// BOM reports a leading UTF-8 byte order mark, stripped from the editable
	// text so the user never sees or deletes it by accident.
	BOM bool `json:"bom"`
	// LineEnding is the file's convention. When Mixed, the editable text keeps
	// its CRs verbatim and no conversion happens in either direction.
	LineEnding LineEnding `json:"lineEnding"`
	// Mixed is LineEnding == LineEndingMixed, exposed as a bool because the UI
	// only ever asks the yes/no question.
	//
	// A mixed file is passed through RAW: converting it either way would
	// rewrite lines the user never touched, and there is no single convention
	// to preserve. The cost is that a newly typed line in such a file gets an
	// LF, matching what the textarea produces — which is the same thing every
	// other editor does with a mixed file.
	Mixed bool `json:"mixed"`
	// TrailingNewline reports whether the file's last line is terminated.
	// "foo\n" and "foo" are different files; git shows the difference as a
	// whole-line change, so it is carried explicitly rather than inferred from
	// the text (a textarea cannot reliably represent a trailing newline).
	TrailingNewline bool `json:"trailingNewline"`
	// Empty marks a zero-byte file, which has neither lines nor a trailing
	// newline and must stay zero bytes when saved unchanged.
	Empty bool `json:"empty"`
}

// EditableFile is a file opened for editing: its text plus the shape needed to
// write it back byte-exactly, plus the state used to detect a concurrent write.
type EditableFile struct {
	Path    string `json:"path"`
	AbsPath string `json:"absPath"`
	// Root is the canonical resolved tree this relative path was read from. The
	// caller returns it on save so a tab cwd change cannot retarget the edit.
	Root string `json:"root"`
	// Text is what the textarea shows: no BOM, LF-only endings (unless the file
	// is mixed), and no synthetic trailing newline.
	Text  string    `json:"text"`
	Shape FileShape `json:"shape"`
	// Version identifies the exact bytes this text came from. It is passed back
	// on save and must still match, or the save is refused.
	Version string `json:"version"`
	// Mode is the file's permission bits, so a chmod +x script stays
	// executable through a save.
	Mode uint32 `json:"mode"`
	Size int64  `json:"size"`
	// Editable is false when the file must not be written back — see
	// NotEditableReason.
	Editable bool `json:"editable"`
	// NotEditableReason is a stable machine-readable code ("binary",
	// "truncated", "irregular") the UI turns into a translated message.
	NotEditableReason string `json:"notEditableReason,omitempty"`
}

// SaveConflictError reports that the file changed on disk between the read and
// the save. It is a distinct type because the UI must react to it differently
// from every other failure: the user's text is still good and they need to be
// offered a choice, not just told the save failed.
type SaveConflictError struct {
	// Kind is "modified" or "deleted". A file deleted elsewhere is not
	// recreated silently — the user may have removed it deliberately, and a
	// save that resurrects it without saying so is a surprise.
	Kind string
}

func (e *SaveConflictError) Error() string {
	if e.Kind == "deleted" {
		return "conflict:deleted"
	}
	return "conflict:modified"
}

// decodeForEdit splits raw file bytes into the text the editor shows and the
// shape needed to rebuild them. Paired with encodeFromEdit, which must be its
// exact inverse for an unmodified text.
func decodeForEdit(raw []byte) (string, FileShape) {
	var shape FileShape

	if len(raw) == 0 {
		shape.Empty = true
		shape.LineEnding = LineEndingLF
		return "", shape
	}

	body := raw
	if bytes.HasPrefix(body, utf8BOM) {
		shape.BOM = true
		body = body[len(utf8BOM):]
	}

	// Count both conventions before deciding: a file with even one stray CRLF
	// among LFs must not be normalised, because doing so would rewrite the
	// other lines.
	crlf := bytes.Count(body, []byte("\r\n"))
	lf := bytes.Count(body, []byte("\n"))
	loneLF := lf - crlf

	switch {
	case crlf > 0 && loneLF > 0:
		shape.LineEnding = LineEndingMixed
		shape.Mixed = true
	case crlf > 0:
		shape.LineEnding = LineEndingCRLF
	default:
		shape.LineEnding = LineEndingLF
	}

	// The trailing newline is removed from the text and recorded, because a
	// textarea cannot hold the difference between "foo\n" and "foo" reliably:
	// browsers strip or add a final newline on their own.
	if bytes.HasSuffix(body, []byte("\n")) {
		shape.TrailingNewline = true
		body = body[:len(body)-1]
		if shape.LineEnding == LineEndingCRLF && bytes.HasSuffix(body, []byte("\r")) {
			body = body[:len(body)-1]
		}
	}

	text := string(body)
	if shape.LineEnding == LineEndingCRLF {
		// Safe because the file is known to be CRLF-only: every \n has a \r
		// before it, so nothing else can be affected.
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	// LF-only and mixed files pass through untouched; in the mixed case the CRs
	// stay in the text so the save can put them back exactly where they were.
	return text, shape
}

// encodeFromEdit rebuilds file bytes from edited text and the shape recorded
// when it was read. Inverse of decodeForEdit.
func encodeFromEdit(text string, shape FileShape) []byte {
	if shape.Empty && text == "" {
		// A file that was empty and still is stays zero bytes — not a lone
		// newline, which is what appending the trailing newline would give.
		return nil
	}

	body := text
	if shape.LineEnding == LineEndingCRLF {
		// The textarea only ever emits \n, so any \r in the text came from the
		// user typing one; splitting on \n and rejoining cannot double a \r
		// because decodeForEdit removed them all.
		body = strings.ReplaceAll(body, "\n", "\r\n")
	}
	if shape.TrailingNewline {
		if shape.LineEnding == LineEndingCRLF {
			body += "\r\n"
		} else {
			body += "\n"
		}
	}

	if shape.BOM {
		return append(append([]byte{}, utf8BOM...), body...)
	}
	return []byte(body)
}

// fileVersion identifies a file's exact contents for conflict detection.
//
// A content hash rather than mtime+size: mtime has coarse granularity on some
// filesystems (and is only whole seconds on a few), so an agent that rewrites a
// file twice within the same tick would be invisible to an mtime check, and a
// same-length edit is invisible to a size check. Hashing costs one read of a
// file that is at most MaxBrowseFileBytes and was just read anyway, which is
// nothing next to silently clobbering an agent's work. The hash also makes the
// check idempotent: rewriting identical bytes is correctly NOT a conflict.
func fileVersion(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// ReadFileForEdit opens a file for editing.
//
// It re-reads the file rather than reusing a ReadFileForBrowse result, because
// the Version must describe the bytes the returned Text was decoded from — a
// version taken from a separate read could describe a different revision.
//
// Files that must not be written back still come back (so the UI can explain
// why) but with Editable false: see NotEditableReason.
func (i *Instance) ReadFileForEdit(rel string) (*EditableFile, error) {
	if strings.TrimSpace(rel) == "" {
		return nil, fmt.Errorf("no file given")
	}
	abs, root, err := i.resolveBrowsePath(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", displayPath(rel), err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", displayPath(rel))
	}

	result := &EditableFile{
		Path:    filepath.ToSlash(filepath.Clean(rel)),
		AbsPath: abs,
		Root:    root,
		Size:    info.Size(),
		Mode:    uint32(info.Mode().Perm()),
	}

	if !info.Mode().IsRegular() {
		result.NotEditableReason = "irregular"
		return result, nil
	}

	// Read one past the cap for the same reason ReadFileForBrowse does: a file
	// exactly at the limit is complete and must stay editable, and a 5 GB log
	// must not be pulled into memory to discover it is too big.
	raw, err := readCapped(abs)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", displayPath(rel), err)
	}
	if len(raw) > MaxBrowseFileBytes {
		// Saving a buffer that only holds the head would destroy the tail.
		// Truncated files carry no Text at all, so there is nothing an editor
		// could submit — the refusal is structural, not a flag to remember.
		result.NotEditableReason = "truncated"
		return result, nil
	}
	if IsBinaryContent(raw) {
		result.NotEditableReason = "binary"
		return result, nil
	}

	text, shape := decodeForEdit(raw)
	result.Text = text
	result.Shape = shape
	result.Version = fileVersion(raw)
	result.Editable = true
	return result, nil
}

// SaveFileForEdit writes edited text back to a file.
//
// text is what the editor holds; shape and version come from the ReadFileForEdit
// that produced it. When overwrite is false and the file no longer matches
// version, nothing is written and a *SaveConflictError is returned.
//
// The write is atomic: a temp file in the SAME directory (so the rename cannot
// cross a filesystem boundary and degrade into a copy) is renamed over the
// target, which means a crash mid-write leaves the original intact.
func (i *Instance) SaveFileForEdit(rel, text string, shape FileShape, version string, overwrite bool) (*EditableFile, error) {
	if strings.TrimSpace(rel) == "" {
		return nil, fmt.Errorf("no file given")
	}
	// The containment guard matters more here than on the read path: this call
	// creates and renames files. resolveBrowsePath resolves symlinks before
	// checking, so a link planted inside the tree cannot redirect the write.
	abs, root, err := i.resolveBrowsePath(rel)
	if err != nil {
		// resolveBrowsePath cannot resolve a path whose leaf no longer exists,
		// so a file deleted since it was opened arrives here as a generic
		// failure. Distinguishing it needs a containment check that survives
		// the missing leaf — done on the parent, which still exists — and it
		// must never turn into "create the file": the user may have removed it
		// deliberately and has to be told that saving resurrects it.
		if i.missingInsideTree(rel) {
			return nil, &SaveConflictError{Kind: "deleted"}
		}
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &SaveConflictError{Kind: "deleted"}
		}
		return nil, fmt.Errorf("could not open %s: %w", displayPath(rel), err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", displayPath(rel))
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", displayPath(rel))
	}

	current, err := readCapped(abs)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", displayPath(rel), err)
	}
	// A file that grew past the cap or turned binary since it was opened is no
	// longer the file the editor is holding, whatever the version says.
	if len(current) > MaxBrowseFileBytes || IsBinaryContent(current) {
		return nil, &SaveConflictError{Kind: "modified"}
	}
	if !overwrite && fileVersion(current) != version {
		return nil, &SaveConflictError{Kind: "modified"}
	}

	data := encodeFromEdit(text, shape)

	// Permissions come from the file as it is NOW, not from the read: a chmod
	// between open and save is a change the user made deliberately.
	if err := writeFileAtomic(abs, data, info.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("could not save %s: %w", displayPath(rel), err)
	}

	saved := &EditableFile{
		Path:     filepath.ToSlash(filepath.Clean(rel)),
		AbsPath:  abs,
		Root:     root,
		Text:     text,
		Shape:    shape,
		Version:  fileVersion(data),
		Mode:     uint32(info.Mode().Perm()),
		Size:     int64(len(data)),
		Editable: true,
	}
	return saved, nil
}

// missingInsideTree reports whether rel names a leaf that is simply absent from
// a directory that IS inside the session tree — i.e. a file deleted since it
// was opened, rather than an escape attempt.
//
// The containment check runs on the resolved PARENT, so it is exactly as strict
// as resolveBrowsePath: a path whose parent is outside the tree, or whose
// parent does not resolve at all, is not "missing", it is rejected. Nothing
// here writes; the caller turns a true into a refusal, never into a create.
func (i *Instance) missingInsideTree(rel string) bool {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	parentRel, leaf := filepath.Split(clean)
	if leaf == "" {
		return false
	}
	parentAbs, _, err := i.resolveBrowsePath(filepath.ToSlash(filepath.Clean(parentRel)))
	if err != nil {
		return false
	}
	_, err = os.Lstat(filepath.Join(parentAbs, leaf))
	return os.IsNotExist(err)
}

// readCapped reads at most MaxBrowseFileBytes+1 bytes. The extra byte is what
// distinguishes "exactly at the cap, therefore complete" from "over the cap,
// therefore truncated" without trusting the stat'd size, which can be stale for
// a file an agent is currently writing.
func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readAtMost(f, make([]byte, 0, MaxBrowseFileBytes+1), MaxBrowseFileBytes+1)
}

// writeFileAtomic replaces path's contents without ever leaving it truncated.
//
// The temp file is created in the target's own directory so os.Rename is a
// same-filesystem operation — across devices it fails, and a fallback copy
// would reintroduce exactly the partial-write window this avoids. It is removed
// on every failure path, so no stray file is left behind.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".asmgr-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := func(cause error) error {
		tmp.Close()
		os.Remove(tmpPath)
		return cause
	}

	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}
	// Flush to disk before the rename: without it a crash right after the
	// rename can leave the new name pointing at zero bytes on some filesystems.
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// CreateTemp always makes 0600; restore the original bits before the file
	// becomes visible under its real name, so the window where a script is
	// unexecutable never exists.
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
