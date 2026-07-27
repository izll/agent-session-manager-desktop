package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// newBrowseRoot builds a plain (non-git) directory tree to browse.
func newBrowseRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "README.md", "hello\n")
	writeFile(t, filepath.Join(root, "src"), "main.go", "package main\n")
	writeFile(t, filepath.Join(root, "src", "lib"), "util.go", "package lib\n")
	return root
}

// The path arrives from the frontend, so nothing may reach outside the
// session's own directory. This is the security boundary of the whole feature.
func TestBrowsePathContainment(t *testing.T) {
	root := newBrowseRoot(t)
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "do not read\n")

	// A symlink INSIDE the tree pointing OUT of it: the case a purely lexical
	// path check cannot see.
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}

	rejected := []string{
		"..",
		"../",
		"../secret.txt",
		"src/../../secret.txt",
		"../../etc/passwd",
		outside,                              // absolute path elsewhere
		filepath.Join(outside, "secret.txt"), // absolute file elsewhere
		"/etc/passwd",
		"escape",            // symlinked directory out of the tree
		"escape/secret.txt", // through that symlink
		"secret-link",       // symlinked file out of the tree
	}
	for _, bad := range rejected {
		if _, err := inst.ListDirectory(bad); err == nil {
			t.Errorf("ListDirectory(%q) was allowed; it must be rejected", bad)
		}
		if _, err := inst.ReadFileForBrowse(bad); err == nil {
			t.Errorf("ReadFileForBrowse(%q) was allowed; it must be rejected", bad)
		}
	}

	// A legitimate nested path must still work, or the guard is useless.
	listing, err := inst.ListDirectory("src/lib")
	if err != nil {
		t.Fatalf("ListDirectory(src/lib): %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "util.go" {
		t.Errorf("src/lib listing = %+v, want just util.go", listing.Entries)
	}
	file, err := inst.ReadFileForBrowse("src/lib/util.go")
	if err != nil {
		t.Fatalf("ReadFileForBrowse(src/lib/util.go): %v", err)
	}
	if file.Content != "package lib\n" {
		t.Errorf("content = %q, want the file's text", file.Content)
	}

	// "" and "." both mean the root itself.
	for _, rootRel := range []string{"", ".", "./"} {
		if _, err := inst.ListDirectory(rootRel); err != nil {
			t.Errorf("ListDirectory(%q) should list the root: %v", rootRel, err)
		}
	}
}

// A symlink pointing back INSIDE the tree is legitimate and must keep working —
// resolving before the containment check must not become a blanket ban.
func TestBrowseAllowsInternalSymlink(t *testing.T) {
	root := newBrowseRoot(t)
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "src-link")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("src-link")
	if err != nil {
		t.Fatalf("an in-tree symlink must be followable: %v", err)
	}
	if len(listing.Entries) == 0 {
		t.Error("in-tree symlinked directory listed as empty")
	}
}

// Listing must not follow a symlink out of the tree either: statting the
// target would leak the size and modification time of a file the user is not
// allowed to open.
func TestListDirectoryDoesNotFollowSymlinksOutOfTree(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", strings.Repeat("x", 4242))
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak-dir")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}

	for _, e := range listing.Entries {
		if e.Name != "leak" && e.Name != "leak-dir" {
			continue
		}
		if !e.Unreadable {
			t.Errorf("%s points outside the tree and must be marked unreadable: %+v", e.Name, e)
		}
		if e.Size != 0 || e.ModTime != "" {
			t.Errorf("%s leaked metadata from outside the tree: size=%d modTime=%q",
				e.Name, e.Size, e.ModTime)
		}
	}
}

func TestListDirectorySortsDirsFirstThenName(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"zeta", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, "b.txt", "b\n")
	writeFile(t, root, "A.txt", "a\n")

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}

	var got []string
	for _, e := range listing.Entries {
		got = append(got, e.Name)
	}
	want := []string{"alpha", "zeta", "A.txt", "b.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestListDirectorySkipsGit(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "a.txt", "a\n")

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	for _, e := range listing.Entries {
		if e.Name == ".git" {
			t.Error(".git must not be listed")
		}
	}
}

func TestListDirectoryCapsEntryCount(t *testing.T) {
	root := t.TempDir()
	total := MaxBrowseEntries + 25
	for n := 0; n < total; n++ {
		writeFile(t, root, strconv.Itoa(n)+".txt", "x\n")
	}

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(listing.Entries) != MaxBrowseEntries {
		t.Errorf("returned %d entries, want the cap of %d", len(listing.Entries), MaxBrowseEntries)
	}
	if !listing.Truncated {
		t.Error("a capped listing must report Truncated")
	}
	if listing.TotalEntries != total {
		t.Errorf("TotalEntries = %d, want the real count %d", listing.TotalEntries, total)
	}
}

// A listing must survive one unreadable entry rather than failing wholesale.
func TestListDirectoryToleratesBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "good.txt", "ok\n")
	if err := os.Symlink(filepath.Join(root, "gone.txt"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	listing, err := inst.ListDirectory("")
	if err != nil {
		t.Fatalf("one bad entry must not fail the listing: %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("entries = %+v, want both the file and the dangling link", listing.Entries)
	}
	var dangling *BrowseEntry
	for idx := range listing.Entries {
		if listing.Entries[idx].Name == "dangling" {
			dangling = &listing.Entries[idx]
		}
	}
	if dangling == nil || !dangling.Unreadable {
		t.Errorf("the dangling link should be marked unreadable: %+v", listing.Entries)
	}
}

func TestReadFileForBrowseDetectsBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte("ELF\x00\x01\x02text"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "text.txt", "plain\n")

	inst := &Instance{Path: root}

	bin, err := inst.ReadFileForBrowse("bin")
	if err != nil {
		t.Fatalf("ReadFileForBrowse(bin): %v", err)
	}
	if !bin.Binary {
		t.Error("a file with a NUL byte must be reported as binary")
	}
	if bin.Content != "" {
		t.Error("a binary file must not return content for the UI to render")
	}

	txt, err := inst.ReadFileForBrowse("text.txt")
	if err != nil {
		t.Fatalf("ReadFileForBrowse(text.txt): %v", err)
	}
	if txt.Binary {
		t.Error("a plain text file was misdetected as binary")
	}
}

// A NUL beyond the sniff window must not flip the verdict — otherwise a large
// text file with one stray byte at the end would be unreadable.
func TestIsBinaryContentOnlyLooksAtTheHead(t *testing.T) {
	deep := append(bytes.Repeat([]byte{'a'}, binarySniffBytes+10), 0)
	if IsBinaryContent(deep) {
		t.Error("a NUL past the sniff window must not mark the file binary")
	}
	if !IsBinaryContent([]byte("ab\x00cd")) {
		t.Error("a NUL in the head must mark the file binary")
	}
	if IsBinaryContent(nil) {
		t.Error("an empty file is not binary")
	}
}

func TestReadFileForBrowseTruncatesLargeFiles(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte{'x'}, MaxBrowseFileBytes+500)
	if err := os.WriteFile(filepath.Join(root, "big.log"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	// Exactly at the cap: displayable in full, and NOT reported as truncated.
	exact := bytes.Repeat([]byte{'y'}, MaxBrowseFileBytes)
	if err := os.WriteFile(filepath.Join(root, "exact.log"), exact, 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}

	got, err := inst.ReadFileForBrowse("big.log")
	if err != nil {
		t.Fatalf("ReadFileForBrowse(big.log): %v", err)
	}
	if !got.Truncated {
		t.Error("an oversized file must report Truncated rather than truncate silently")
	}
	if len(got.Content) != MaxBrowseFileBytes {
		t.Errorf("content = %d bytes, want the cap of %d", len(got.Content), MaxBrowseFileBytes)
	}
	if got.Size != int64(len(big)) {
		t.Errorf("Size = %d, want the real file size %d", got.Size, len(big))
	}

	atCap, err := inst.ReadFileForBrowse("exact.log")
	if err != nil {
		t.Fatalf("ReadFileForBrowse(exact.log): %v", err)
	}
	if atCap.Truncated {
		t.Error("a file exactly at the cap is complete and must not be flagged")
	}
}

func TestReadFileForBrowseRejectsDirsAndMissing(t *testing.T) {
	root := newBrowseRoot(t)
	inst := &Instance{Path: root}

	if _, err := inst.ReadFileForBrowse("src"); err == nil {
		t.Error("reading a directory must fail")
	}
	if _, err := inst.ReadFileForBrowse("nope.txt"); err == nil {
		t.Error("reading a missing file must fail")
	}
	if _, err := inst.ReadFileForBrowse(""); err == nil {
		t.Error("an empty path must fail")
	}
	if _, err := inst.ListDirectory("README.md"); err == nil {
		t.Error("listing a file as a directory must fail")
	}
}
