package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// indexPaths pulls the paths out of an index for comparison.
func indexPaths(index *FileIndex) []string {
	out := make([]string, len(index.Files))
	for i, f := range index.Files {
		out[i] = f.Path
	}
	return out
}

func TestBuildFileIndexWalksTheWholeTree(t *testing.T) {
	root := newBrowseRoot(t)
	inst := &Instance{Path: root}

	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	want := []string{"README.md", "src/lib/util.go", "src/main.go"}
	if got := strings.Join(indexPaths(index), ","); got != strings.Join(want, ",") {
		t.Errorf("index = %v, want %v", indexPaths(index), want)
	}
	if index.Truncated {
		t.Error("a three-file tree must not be reported as truncated")
	}
	for _, f := range index.Files {
		if f.Name == "" || strings.Contains(f.Name, "/") {
			t.Errorf("Name = %q, want the basename alone", f.Name)
		}
		if f.Size <= 0 {
			t.Errorf("%s has size %d, want the real file size", f.Path, f.Size)
		}
	}
}

// The walk visits paths nobody vetted by clicking on them, so every entry it
// meets has to pass the same containment rule the rest of the browser uses.
// This is the security boundary of the index.
func TestBuildFileIndexContainment(t *testing.T) {
	root := newBrowseRoot(t)
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "do not read\n")
	if err := os.MkdirAll(filepath.Join(outside, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "deep"), "deeper-secret.txt", "nor this\n")

	// A symlinked FILE out of the tree, at the root...
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link")); err != nil {
		t.Fatal(err)
	}
	// ...a symlinked DIRECTORY out of the tree, at the root...
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	// ...and both again MID-WALK, in a subdirectory the walk reaches on its own.
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "src", "lib", "nested-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "src", "lib", "nested-escape")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}

	for _, f := range index.Files {
		if strings.Contains(f.Path, "escape") || strings.Contains(f.Name, "secret") ||
			strings.Contains(f.Path, "-link") {
			t.Errorf("index contains %q, which resolves outside the session tree", f.Path)
		}
		// Every indexed path must actually be openable through the browser, or
		// the picker offers rows that error when clicked.
		if _, err := inst.ReadFileForBrowse(f.Path); err != nil {
			t.Errorf("indexed %q is not readable through the browser: %v", f.Path, err)
		}
	}

	// The legitimate files must still be there, or the guard has just broken
	// the feature.
	if got := len(index.Files); got != 3 {
		t.Errorf("index has %d files, want the 3 real ones: %v", got, indexPaths(index))
	}
}

// A symlink pointing back INSIDE the tree is legitimate: the file it names is
// readable through the browser, so it belongs in the index.
func TestBuildFileIndexKeepsInternalSymlinkedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "real.txt", "hi\n")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	// A symlinked DIRECTORY inside the tree is NOT descended into: its contents
	// are already reachable by their real path and following it would list
	// every file twice.
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "sub"), "inner.txt", "x\n")
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "sub-link")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	want := []string{"alias.txt", "real.txt", "sub/inner.txt"}
	if got := strings.Join(indexPaths(index), ","); got != strings.Join(want, ",") {
		t.Errorf("index = %v, want %v", indexPaths(index), want)
	}
}

// A dangling link would produce a picker row that errors when clicked.
func TestBuildFileIndexDropsDanglingSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "good.txt", "ok\n")
	if err := os.Symlink(filepath.Join(root, "gone.txt"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	if got := strings.Join(indexPaths(index), ","); got != "good.txt" {
		t.Errorf("index = %v, want only good.txt", indexPaths(index))
	}
}

func TestBuildFileIndexSkipsAndReportsSkippedDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.go", "package main\n")
	for _, dir := range []string{".git", "node_modules", ".hg", ".svn"} {
		if err := os.MkdirAll(filepath.Join(root, dir, "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, dir, "inner"), "hidden.txt", "x\n")
	}
	// Nested one too: the report must name it by its path, not just its name.
	if err := os.MkdirAll(filepath.Join(root, "pkg", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "pkg", "node_modules"), "dep.js", "x\n")
	writeFile(t, filepath.Join(root, "pkg"), "own.go", "package pkg\n")

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	want := []string{"app.go", "pkg/own.go"}
	if got := strings.Join(indexPaths(index), ","); got != strings.Join(want, ",") {
		t.Errorf("index = %v, want %v", indexPaths(index), want)
	}
	// Never silently dropped: the UI has to be able to say what was skipped.
	wantSkipped := []string{".git", ".hg", ".svn", "node_modules", "pkg/node_modules"}
	if got := strings.Join(index.SkippedDirs, ","); got != strings.Join(wantSkipped, ",") {
		t.Errorf("SkippedDirs = %v, want %v", index.SkippedDirs, wantSkipped)
	}
	if index.IncludedAll {
		t.Error("IncludedAll must be false for the default walk")
	}

	// includeAll disables the skip list — but not the containment check.
	all, err := inst.BuildFileIndex(true)
	if err != nil {
		t.Fatalf("BuildFileIndex(true): %v", err)
	}
	if len(all.Files) != 7 {
		t.Errorf("includeAll index has %d files, want all 7: %v", len(all.Files), indexPaths(all))
	}
	if len(all.SkippedDirs) != 0 {
		t.Errorf("includeAll must skip nothing, got %v", all.SkippedDirs)
	}
	if !all.IncludedAll {
		t.Error("IncludedAll must be true when the skip list was disabled")
	}
}

// includeAll must not become a way around the containment check.
func TestBuildFileIndexIncludeAllStillContained(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "no\n")
	writeFile(t, root, "ok.txt", "yes\n")
	if err := os.Symlink(outside, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(true)
	if err != nil {
		t.Fatalf("BuildFileIndex(true): %v", err)
	}
	if got := strings.Join(indexPaths(index), ","); got != "ok.txt" {
		t.Errorf("index = %v, want only ok.txt — the symlinked node_modules leaves the tree", indexPaths(index))
	}
}

func TestBuildFileIndexCapsFileCount(t *testing.T) {
	root := t.TempDir()
	// Spread over subdirectories so the cap is hit mid-recursion, not just at
	// the end of one flat ReadDir.
	const perDir = 600
	dirs := (MaxIndexFiles + 2*perDir) / perDir
	for d := 0; d < dirs; d++ {
		sub := filepath.Join(root, "d"+strconv.Itoa(d))
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < perDir; n++ {
			writeFile(t, sub, strconv.Itoa(n)+".txt", "x\n")
		}
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	if len(index.Files) != MaxIndexFiles {
		t.Errorf("index holds %d files, want the cap of %d", len(index.Files), MaxIndexFiles)
	}
	if !index.Truncated {
		t.Error("a capped index must report Truncated rather than truncate silently")
	}
}

func TestBuildFileIndexCapsDepth(t *testing.T) {
	root := t.TempDir()
	deep := root
	for d := 0; d <= MaxIndexDepth+3; d++ {
		deep = filepath.Join(deep, "d")
		if err := os.Mkdir(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, deep, "f.txt", "x\n")
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatalf("BuildFileIndex: %v", err)
	}
	if !index.Truncated {
		t.Error("a tree deeper than MaxIndexDepth must report Truncated")
	}
	// The root is depth 0 and holds no file, so the nested levels 1..MaxIndexDepth
	// contribute one each and nothing below them is read.
	if len(index.Files) != MaxIndexDepth {
		t.Errorf("indexed %d levels, want the depth cap of %d", len(index.Files), MaxIndexDepth)
	}
}

func TestBuildFileIndexRejectsMissingRoot(t *testing.T) {
	inst := &Instance{Path: filepath.Join(t.TempDir(), "gone")}
	if _, err := inst.BuildFileIndex(false); err == nil {
		t.Error("indexing a missing working directory must fail")
	}
}

// --- Content search --------------------------------------------------------

func TestSearchFileContentsFindsMatches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package main\nfunc Hello() {}\n")
	writeFile(t, root, "b.go", "// hello there\nvar x = 1\n")

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}

	res, err := inst.SearchFileContents("hello", false, index.Files)
	if err != nil {
		t.Fatalf("SearchFileContents: %v", err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("matches = %+v, want one in each file", res.Matches)
	}
	if res.Matches[0].Path != "a.go" || res.Matches[0].Line != 2 {
		t.Errorf("first match = %+v, want a.go line 2", res.Matches[0])
	}
	if got := res.Matches[0].Text[res.Matches[0].Col : res.Matches[0].Col+res.Matches[0].Length]; !strings.EqualFold(got, "hello") {
		t.Errorf("Col/Length point at %q, want the match itself", got)
	}

	// Case-sensitive must not find the lowercase one in a.go.
	sensitive, err := inst.SearchFileContents("Hello", true, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(sensitive.Matches) != 1 || sensitive.Matches[0].Path != "a.go" {
		t.Errorf("case-sensitive matches = %+v, want only a.go", sensitive.Matches)
	}

	if _, err := inst.SearchFileContents("   ", false, index.Files); err == nil {
		t.Error("an empty query must fail rather than match everything")
	}
}

func TestSearchFileContentsSkipsBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte("needle\x00needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "text.txt", "needle\n")

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Path != "text.txt" {
		t.Errorf("matches = %+v, want only the text file", res.Matches)
	}
	if res.FilesSkipped != 1 {
		t.Errorf("FilesSkipped = %d, want the binary file counted", res.FilesSkipped)
	}
	if res.FilesSearched != 1 {
		t.Errorf("FilesSearched = %d, want 1", res.FilesSearched)
	}
}

func TestSearchFileContentsCapsPerFileAndTotal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "many.txt", strings.Repeat("needle\n", MaxSearchMatchesPerFile+40))

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != MaxSearchMatchesPerFile {
		t.Errorf("matches = %d, want the per-file cap of %d", len(res.Matches), MaxSearchMatchesPerFile)
	}

	// The total cap: enough files that the per-file cap alone cannot reach it.
	root2 := t.TempDir()
	files := MaxSearchMatches/MaxSearchMatchesPerFile + 5
	for n := 0; n < files; n++ {
		writeFile(t, root2, strconv.Itoa(n)+".txt", strings.Repeat("needle\n", MaxSearchMatchesPerFile))
	}
	inst2 := &Instance{Path: root2}
	index2, err := inst2.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := inst2.SearchFileContents("needle", false, index2.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Matches) != MaxSearchMatches {
		t.Errorf("matches = %d, want the total cap of %d", len(res2.Matches), MaxSearchMatches)
	}
	if !res2.Truncated {
		t.Error("a capped search must report Truncated")
	}
}

// The index can be seconds old, so a path in it that has since become a symlink
// out of the tree must be refused at read time, not trusted because it was
// vetted when the walk ran.
func TestSearchFileContentsRevalidatesPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "needle\n")
	writeFile(t, root, "swapped.txt", "needle\n")

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}

	// Replace the indexed file with a link out of the tree, exactly as an agent
	// working in the directory could between the walk and the search.
	if err := os.Remove(filepath.Join(root, "swapped.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "swapped.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("matches = %+v, want none — the path now leaves the tree", res.Matches)
	}
	if res.FilesSkipped != 1 {
		t.Errorf("FilesSkipped = %d, want the swapped path counted", res.FilesSkipped)
	}
}

func TestSearchFileContentsSkipsOversizedFiles(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("a", MaxSearchFileBytes+10) + "needle\n"
	if err := os.WriteFile(filepath.Join(root, "big.log"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("matches = %+v, want none — the file is over the size cap", res.Matches)
	}
	if res.FilesSkipped != 1 {
		t.Errorf("FilesSkipped = %d, want the oversized file counted", res.FilesSkipped)
	}
}

func TestSearchFileContentsRechecksSizeAfterIndexSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "growing.log")
	if err := os.WriteFile(path, []byte("small\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 1 || index.Files[0].Size > MaxSearchFileBytes {
		t.Fatalf("unexpected initial index: %+v", index.Files)
	}
	large := strings.Repeat("x", MaxSearchFileBytes+1) + "needle\n"
	if err := os.WriteFile(path, []byte(large), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 0 || result.FilesSkipped != 1 {
		t.Fatalf("post-index oversized replacement was searched: %+v", result)
	}
}

// A minified bundle is one enormous line; previewing all of it would ship
// hundreds of kilobytes to the UI to render eighty columns.
func TestSearchFileContentsTrimsLongLines(t *testing.T) {
	root := t.TempDir()
	line := strings.Repeat("x", 5000) + "needle" + strings.Repeat("y", 5000)
	writeFile(t, root, "min.js", line+"\n")

	inst := &Instance{Path: root}
	index, err := inst.BuildFileIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.SearchFileContents("needle", false, index.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("matches = %+v, want one", res.Matches)
	}
	m := res.Matches[0]
	if len(m.Text) > maxSearchLineRunes+16 {
		t.Errorf("preview is %d bytes, want it trimmed to about %d", len(m.Text), maxSearchLineRunes)
	}
	// The reported column must still point at the match inside the TRIMMED text.
	if m.Col < 0 || m.Col+m.Length > len(m.Text) || m.Text[m.Col:m.Col+m.Length] != "needle" {
		t.Errorf("Col=%d Length=%d does not point at the match in %q", m.Col, m.Length, m.Text)
	}
}

// Trimming must never cut a rune in half — the mojibake would land right next
// to the match, which is where it would be noticed.
func TestTrimMatchLineKeepsValidUTF8(t *testing.T) {
	prefix := strings.Repeat("á", 2000) // two bytes each
	line := prefix + "needle" + prefix
	col := strings.Index(line, "needle")
	text, newCol := trimMatchLine(line, col, len("needle"))
	if !strings.Contains(text, "needle") {
		t.Fatalf("trimmed preview lost the match: %q", text)
	}
	if text[newCol:newCol+len("needle")] != "needle" {
		t.Errorf("adjusted column %d does not point at the match", newCol)
	}
	for _, r := range text {
		if r == '�' {
			t.Error("trimming split a multi-byte rune")
			break
		}
	}
}
