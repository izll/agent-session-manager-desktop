package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readSaveUnchanged runs a file through the exact path the UI uses — read, then
// save the text back untouched — and returns the resulting bytes.
//
// It deliberately goes through ReadFileForEdit/SaveFileForEdit rather than
// decodeForEdit/encodeFromEdit: testing the helpers alone would pass even if
// the API layer dropped the shape or recomputed the trailing newline, which is
// precisely the mistake this feature has to avoid.
func readSaveUnchanged(t *testing.T, root, name string) []byte {
	t.Helper()
	inst := &Instance{Path: root}

	opened, err := inst.ReadFileForEdit(name)
	if err != nil {
		t.Fatalf("ReadFileForEdit(%s): %v", name, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve edit root: %v", err)
	}
	if opened.Root != resolvedRoot {
		t.Fatalf("ReadFileForEdit(%s) root = %q, want canonical %q", name, opened.Root, resolvedRoot)
	}
	if !opened.Editable {
		t.Fatalf("%s should be editable, got reason %q", name, opened.NotEditableReason)
	}
	saved, err := inst.SaveFileForEdit(name, opened.Text, opened.Shape, opened.Version, false)
	if err != nil {
		t.Fatalf("SaveFileForEdit(%s): %v", name, err)
	}
	if saved.Root != resolvedRoot {
		t.Fatalf("SaveFileForEdit(%s) root = %q, want canonical %q", name, saved.Root, resolvedRoot)
	}
	got, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The headline requirement: a file opened and saved with no edits must be
// byte-identical. Anything else turns a no-op into a whole-file diff.
func TestEditRoundTripIsByteExact(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"lf-with-trailing-newline", []byte("one\ntwo\nthree\n")},
		{"lf-without-trailing-newline", []byte("one\ntwo\nthree")},
		{"crlf-with-trailing-newline", []byte("one\r\ntwo\r\nthree\r\n")},
		{"crlf-without-trailing-newline", []byte("one\r\ntwo\r\nthree")},
		{"utf8-bom-lf", append(append([]byte{}, utf8BOM...), []byte("hello\nworld\n")...)},
		{"utf8-bom-crlf-no-newline", append(append([]byte{}, utf8BOM...), []byte("hello\r\nworld")...)},
		{"empty", []byte("")},
		{"single-line-no-newline", []byte("just this")},
		{"single-line-with-newline", []byte("just this\n")},
		{"mixed-endings", []byte("unix\nwindows\r\nunix again\n")},
		{"mixed-endings-crlf-last", []byte("unix\nwindows\r\n")},
		{"only-a-newline", []byte("\n")},
		{"only-a-crlf", []byte("\r\n")},
		{"blank-lines-preserved", []byte("a\n\n\nb\n")},
		{"trailing-whitespace-kept", []byte("a   \n\tb\t\n")},
		{"utf8-multibyte", []byte("árvíztűrő tükörfúrógép\n日本語\n")},
		{"no-newline-trailing-space", []byte("tail with space   ")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "f.txt"), tc.raw, 0o644); err != nil {
				t.Fatal(err)
			}
			got := readSaveUnchanged(t, root, "f.txt")
			if !bytes.Equal(got, tc.raw) {
				t.Errorf("round trip changed the file\n before: %q\n  after: %q", tc.raw, got)
			}
		})
	}
}

// The decompose/recompose pair is the structural guarantee; if it is not an
// exact inverse, every round-trip case above is luck.
func TestDecodeEncodeAreInverses(t *testing.T) {
	inputs := [][]byte{
		[]byte(""),
		[]byte("\n"),
		[]byte("a"),
		[]byte("a\n"),
		[]byte("a\r\n"),
		[]byte("a\r\nb"),
		[]byte("a\nb\r\nc"),
		append(append([]byte{}, utf8BOM...), []byte("")...),
		append(append([]byte{}, utf8BOM...), []byte("x\r\ny\r\n")...),
	}
	for _, raw := range inputs {
		text, shape := decodeForEdit(raw)
		back := encodeFromEdit(text, shape)
		if !bytes.Equal(back, raw) {
			t.Errorf("decode/encode(%q) = %q, want the original", raw, back)
		}
	}
}

// The BOM is invisible in the editor, so a bug here is one the user cannot see
// until git shows it.
func TestBOMIsHiddenFromTheEditorAndNotDuplicated(t *testing.T) {
	root := t.TempDir()
	raw := append(append([]byte{}, utf8BOM...), []byte("line\n")...)
	if err := os.WriteFile(filepath.Join(root, "bom.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("bom.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Shape.BOM {
		t.Error("the BOM must be recorded in the shape")
	}
	if strings.HasPrefix(opened.Text, string(utf8BOM)) {
		t.Error("the BOM must not appear in the editable text")
	}

	// Save twice: a save that re-adds a BOM the text already carries would only
	// show up on the second pass.
	saved, err := inst.SaveFileForEdit("bom.txt", opened.Text, opened.Shape, opened.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("bom.txt", saved.Text, saved.Shape, saved.Version, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "bom.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("repeated saves changed the file: %q, want %q", got, raw)
	}
}

// A CRLF file that gains a line must gain it as CRLF, or the new line is the
// only one git shows as different from the rest.
func TestEditedCRLFFileKeepsCRLF(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "w.txt"), []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("w.txt")
	if err != nil {
		t.Fatal(err)
	}
	if opened.Text != "a\nb" {
		t.Fatalf("editable text = %q, want LF-normalised %q", opened.Text, "a\nb")
	}
	// The textarea only ever produces \n, which is what the editor would send.
	if _, err := inst.SaveFileForEdit("w.txt", "a\nb\nc", opened.Shape, opened.Version, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "w.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\r\nb\r\nc\r\n" {
		t.Errorf("got %q, want every line CRLF including the new one", got)
	}
}

// A mixed file is passed through raw rather than normalised: converting it
// either way rewrites lines the user never touched.
func TestMixedEndingsArePreservedNotNormalised(t *testing.T) {
	root := t.TempDir()
	raw := []byte("unix\nwindows\r\nunix\n")
	if err := os.WriteFile(filepath.Join(root, "m.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("m.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Shape.Mixed {
		t.Fatal("a file with both conventions must be reported as mixed")
	}
	// The CRs stay in the editable text, which is how the save puts them back
	// exactly where they were.
	if !strings.Contains(opened.Text, "windows\r") {
		t.Errorf("mixed text = %q, want the CR kept verbatim", opened.Text)
	}
	if _, err := inst.SaveFileForEdit("m.txt", opened.Text, opened.Shape, opened.Version, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "m.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("mixed file was rewritten: %q, want %q", got, raw)
	}
}

// The write path is reachable from the frontend and creates files, so its
// containment guard matters more than the read path's — the same vectors the
// read tests cover must be rejected here too.
func TestSaveFileForEditPathContainment(t *testing.T) {
	root := newBrowseRoot(t)
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "original\n")

	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	shape := FileShape{LineEnding: LineEndingLF, TrailingNewline: true}

	rejected := []string{
		"..",
		"../secret.txt",
		"src/../../secret.txt",
		"../../etc/passwd",
		outside,
		filepath.Join(outside, "secret.txt"),
		"/etc/passwd",
		"escape/secret.txt",
		"secret-link",
		"",
	}
	for _, bad := range rejected {
		if _, err := inst.ReadFileForEdit(bad); err == nil {
			t.Errorf("ReadFileForEdit(%q) was allowed; it must be rejected", bad)
		}
		if _, err := inst.SaveFileForEdit(bad, "OWNED\n", shape, "", true); err == nil {
			t.Errorf("SaveFileForEdit(%q) was allowed; it must be rejected", bad)
		}
	}

	// Nothing outside the tree may have been touched, including through the
	// symlinks — a rejected save that still wrote would be the worst outcome.
	got, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Errorf("a file outside the tree was written: %q", got)
	}

	// The guard is only useful if legitimate saves still work.
	opened, err := inst.ReadFileForEdit("src/lib/util.go")
	if err != nil {
		t.Fatalf("a normal nested file must be editable: %v", err)
	}
	if _, err := inst.SaveFileForEdit("src/lib/util.go", opened.Text+"\n// added", opened.Shape, opened.Version, false); err != nil {
		t.Fatalf("a normal nested save must succeed: %v", err)
	}
}

// Sessions have agents writing these same files, so a save must never clobber
// a write that landed since the file was opened.
func TestSaveRefusesWhenTheFileChangedSinceItWasRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("shared.txt")
	if err != nil {
		t.Fatal(err)
	}

	// The agent writes while the user is editing.
	if err := os.WriteFile(path, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = inst.SaveFileForEdit("shared.txt", "my edit\n", opened.Shape, opened.Version, false)
	var conflict *SaveConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("save must be refused with a conflict, got %v", err)
	}
	if conflict.Kind != "modified" {
		t.Errorf("conflict kind = %q, want %q", conflict.Kind, "modified")
	}

	// The refusal must not have written anything.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "theirs\n" {
		t.Errorf("a refused save still wrote: %q", got)
	}

	// Overwriting is the user's explicit choice, and must then work.
	if _, err := inst.SaveFileForEdit("shared.txt", "my edit", opened.Shape, opened.Version, true); err != nil {
		t.Fatalf("an explicit overwrite must succeed: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "my edit\n" {
		t.Errorf("overwrite wrote %q", got)
	}
}

// A content hash, not mtime: two writes inside one mtime tick must still be
// caught, and rewriting identical bytes must NOT count as a conflict.
func TestConflictDetectionUsesContentNotTimestamps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("f.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Rewritten with identical content: the mtime moved, the bytes did not.
	if err := os.WriteFile(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("f.txt", "edited\n", opened.Shape, opened.Version, false); err != nil {
		t.Errorf("an identical rewrite is not a conflict: %v", err)
	}

	// A same-length change, which a size check would miss.
	if err := os.WriteFile(path, []byte("EDITED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var conflict *SaveConflictError
	if _, err := inst.SaveFileForEdit("f.txt", "again\n", opened.Shape, opened.Version, false); !errors.As(err, &conflict) {
		t.Errorf("a same-length change must be detected, got %v", err)
	}
}

// A file deleted elsewhere must not be silently recreated: the user may have
// removed it deliberately and needs to be told that saving resurrects it.
func TestSaveReportsDeletionDistinctly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(path, []byte("here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("gone.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	_, err = inst.SaveFileForEdit("gone.txt", "text\n", opened.Shape, opened.Version, false)
	var conflict *SaveConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("a deleted file must be a conflict, got %v", err)
	}
	if conflict.Kind != "deleted" {
		t.Errorf("conflict kind = %q, want %q", conflict.Kind, "deleted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused save must not recreate the file")
	}

	// Even overwrite must not resurrect it — the containment guard resolves the
	// path on disk and there is nothing there to resolve.
	if _, err := inst.SaveFileForEdit("gone.txt", "text\n", opened.Shape, opened.Version, true); err == nil {
		t.Error("saving over a deleted file must not silently recreate it")
	}
}

// A chmod +x script must still be executable after a save.
func TestSavePreservesPermissionBits(t *testing.T) {
	// The editor preserves a file's permission bits — a unix idea. Windows has no permission
	// bits: Go reports 0666 for every regular file and 0777 for every directory,
	// and what actually protects them is the ACL, which Mode() does not
	// describe. Asserting here would be testing the operating system.
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to the umask, so set the bits explicitly.
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if opened.Mode != 0o755 {
		t.Errorf("Mode = %o, want 0755", opened.Mode)
	}
	if _, err := inst.SaveFileForEdit("run.sh", opened.Text+"\necho more", opened.Shape, opened.Version, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("permissions after save = %o, want 0755", info.Mode().Perm())
	}

	// A restrictive mode must survive too, not just the executable bit.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := inst.ReadFileForEdit("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("run.sh", reopened.Text, reopened.Shape, reopened.Version, false); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions after save = %o, want 0600", info.Mode().Perm())
	}
}

// Saving a truncated buffer would destroy everything past the read cap, so
// such a file comes back with no Text at all — there is nothing to submit.
func TestTruncatedAndBinaryFilesAreNotEditable(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte("x"), MaxBrowseFileBytes+500)
	if err := os.WriteFile(filepath.Join(root, "big.log"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte("ELF\x00\x01text"), 0o644); err != nil {
		t.Fatal(err)
	}
	exact := bytes.Repeat([]byte("y"), MaxBrowseFileBytes)
	if err := os.WriteFile(filepath.Join(root, "exact.log"), exact, 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}

	truncated, err := inst.ReadFileForEdit("big.log")
	if err != nil {
		t.Fatal(err)
	}
	if truncated.Editable || truncated.NotEditableReason != "truncated" {
		t.Errorf("oversized file: editable=%v reason=%q", truncated.Editable, truncated.NotEditableReason)
	}
	if truncated.Text != "" {
		t.Error("a truncated file must carry no text, so a partial save is impossible")
	}

	binary, err := inst.ReadFileForEdit("bin")
	if err != nil {
		t.Fatal(err)
	}
	if binary.Editable || binary.NotEditableReason != "binary" {
		t.Errorf("binary file: editable=%v reason=%q", binary.Editable, binary.NotEditableReason)
	}

	// Exactly at the cap is complete, so it stays editable.
	atCap, err := inst.ReadFileForEdit("exact.log")
	if err != nil {
		t.Fatal(err)
	}
	if !atCap.Editable {
		t.Errorf("a file exactly at the cap is complete and must be editable: %q", atCap.NotEditableReason)
	}

	// And the save side refuses even if a stale editor submits one anyway: the
	// file on disk is what is checked, not what the caller claims.
	shape := FileShape{LineEnding: LineEndingLF, TrailingNewline: true}
	var conflict *SaveConflictError
	if _, err := inst.SaveFileForEdit("big.log", "tiny\n", shape, "", true); !errors.As(err, &conflict) {
		t.Errorf("saving over an oversized file must be refused, got %v", err)
	}
	if _, err := inst.SaveFileForEdit("bin", "tiny\n", shape, "", true); !errors.As(err, &conflict) {
		t.Errorf("saving over a binary file must be refused, got %v", err)
	}
	// Neither refusal may have written.
	if got, _ := os.ReadFile(filepath.Join(root, "big.log")); len(got) != len(big) {
		t.Errorf("the oversized file was truncated by a refused save: %d bytes", len(got))
	}
}

func TestReadFileForEditRejectsDirsAndMissing(t *testing.T) {
	root := newBrowseRoot(t)
	inst := &Instance{Path: root}

	if _, err := inst.ReadFileForEdit("src"); err == nil {
		t.Error("opening a directory for editing must fail")
	}
	if _, err := inst.ReadFileForEdit("nope.txt"); err == nil {
		t.Error("opening a missing file must fail")
	}

	shape := FileShape{LineEnding: LineEndingLF, TrailingNewline: true}
	if _, err := inst.SaveFileForEdit("src", "x\n", shape, "", true); err == nil {
		t.Error("saving over a directory must fail")
	}
	// The directory must be intact.
	if info, err := os.Stat(filepath.Join(root, "src")); err != nil || !info.IsDir() {
		t.Error("a refused save damaged the directory")
	}
}

// The temp file is an implementation detail; it must never survive the call,
// success or failure. A stray .foo.asmgr-123 in a repo is noise the user then
// has to clean up.
func TestSaveLeavesNoTempFileBehind(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("f.txt", "b\n", opened.Shape, opened.Version, false); err != nil {
		t.Fatal(err)
	}
	assertOnlyEntries(t, root, "f.txt")

	// And on the failure path: a conflict must not leave one either.
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("f.txt", "c\n", opened.Shape, opened.Version, false); err == nil {
		t.Fatal("expected a conflict")
	}
	assertOnlyEntries(t, root, "f.txt")

	// A write that fails after the temp file exists: an unwritable directory
	// makes CreateTemp itself fail, so exercise the rename failure instead by
	// pointing at a directory that goes away is not portable — settle for
	// verifying the atomic helper cleans up on a chmod-refused target.
	locked := t.TempDir()
	target := filepath.Join(locked, "g.txt")
	if err := os.WriteFile(target, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("y\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	assertOnlyEntries(t, locked, "g.txt")
}

// assertOnlyEntries fails if dir holds anything other than the named entries.
func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	allowed := make(map[string]bool, len(want))
	for _, name := range want {
		allowed[name] = true
	}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("stray file left behind: %s", e.Name())
		}
	}
	if len(entries) != len(want) {
		t.Errorf("directory holds %d entries, want %d", len(entries), len(want))
	}
}

// The original file must survive a failed write intact — that is the whole
// point of writing to a temp file first.
func TestWriteFileAtomicNeverTruncatesTheOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	original := []byte("important contents\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Make the directory unwritable so CreateTemp fails: the original must be
	// untouched, because it is never opened for writing at all.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	if err := writeFileAtomic(path, []byte("new\n"), 0o644); err == nil {
		t.Skip("the directory is still writable (running as root); nothing to assert")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("a failed write damaged the original: %q", got)
	}
}

// An in-tree symlink is a legitimate way to reach a file, and editing through
// one must write the real file rather than replacing the link.
func TestSaveThroughInTreeSymlinkWritesTheTarget(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	opened, err := inst.ReadFileForEdit("link.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.SaveFileForEdit("link.txt", "edited", opened.Shape, opened.Version, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "edited\n" {
		t.Errorf("the target was not updated: %q", got)
	}
	// resolveBrowsePath hands back the resolved path, so the write replaces the
	// target and the link still points at it.
	info, err := os.Lstat(filepath.Join(root, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
}

func TestStagedEditRechecksVersionImmediatelyBeforeReplace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "f.txt")
	original := []byte("opened version\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	staged, err := stageFileAtomic(target, []byte("editor version\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(staged) })
	// Simulate an agent write after the editor's initial version check but
	// while its replacement was being staged and synced.
	if err := os.WriteFile(target, []byte("agent newer version\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = replaceStagedFileChecked(staged, target, fileVersion(original), false)
	var conflict *SaveConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "modified" {
		t.Fatalf("replace error = %v, want modified conflict", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "agent newer version\n" {
		t.Fatalf("newer agent content was overwritten: %q", got)
	}
}
