package session

import "testing"

// parseNumstatZ reads `git diff --numstat -z`, which is used so a file listing
// costs the same whatever the files contain — the whole point of listing before
// loading. The -z form matters: in the plain output git quotes and escapes any
// path with a space, quote or newline in it, and those would have to be
// unescaped correctly or the file would open the wrong thing.
func TestParseNumstatZ(t *testing.T) {
	t.Run("plain changes", func(t *testing.T) {
		out := "12\t3\tsrc/app.go\x004\t0\tREADME.md\x00"
		files := parseNumstatZ(out)

		if len(files) != 2 {
			t.Fatalf("got %d files, want 2", len(files))
		}
		if files[0].Path != "src/app.go" || files[0].Added != 12 || files[0].Removed != 3 {
			t.Errorf("first file is %+v", files[0])
		}
		if files[1].Path != "README.md" || files[1].Added != 4 || files[1].Removed != 0 {
			t.Errorf("second file is %+v", files[1])
		}
	})

	t.Run("a path with a space", func(t *testing.T) {
		// The case -z exists for: unquoted and unescaped between the NULs.
		out := "1\t1\tdocs/my notes.md\x00"
		files := parseNumstatZ(out)

		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Path != "docs/my notes.md" {
			t.Errorf("path is %q, want it left literal", files[0].Path)
		}
	})

	t.Run("a rename", func(t *testing.T) {
		// git writes an empty path field, then old and new as separate records.
		out := "0\t0\t\x00old/name.go\x00new/name.go\x00"
		files := parseNumstatZ(out)

		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Status != "renamed" {
			t.Errorf("status is %q, want \"renamed\"", files[0].Status)
		}
		if files[0].OldPath != "old/name.go" || files[0].Path != "new/name.go" {
			t.Errorf("rename is %q → %q", files[0].OldPath, files[0].Path)
		}
	})

	t.Run("a binary file", func(t *testing.T) {
		// git reports "-" for both counts; treating those as 0 would claim the
		// file is unchanged.
		out := "-\t-\tassets/logo.png\x00"
		files := parseNumstatZ(out)

		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if !files[0].Binary {
			t.Error("the file is not marked binary, so the UI would show it as 0 changed lines")
		}
	})

	t.Run("empty output", func(t *testing.T) {
		if files := parseNumstatZ(""); len(files) != 0 {
			t.Errorf("got %d files from empty output, want none", len(files))
		}
	})

	t.Run("a rename at the end with nothing following", func(t *testing.T) {
		// Truncated output must not read past the end of the fields.
		out := "0\t0\t\x00old/name.go\x00"
		_ = parseNumstatZ(out) // must not panic
	})
}

// numstat says nothing about what happened to a file — a new file and an edited
// one are both "<added> <removed> <path>" — so every file came out labelled
// "modified" in the diff list. name-status is the side that knows.
func TestApplyNameStatus(t *testing.T) {
	t.Run("added and modified are distinguished", func(t *testing.T) {
		files := []DiffFileSummary{
			{Path: "existing.txt", Status: "modified"},
			{Path: "brand-new.txt", Status: "modified"},
		}
		applyNameStatus(files, "M\x00existing.txt\x00A\x00brand-new.txt\x00")

		if files[0].Status != "modified" {
			t.Errorf("existing.txt is %q, want \"modified\"", files[0].Status)
		}
		if files[1].Status != "added" {
			t.Errorf("brand-new.txt is %q, want \"added\" — this is the bug where every "+
				"new file was listed under Modified", files[1].Status)
		}
	})

	t.Run("deletions", func(t *testing.T) {
		files := []DiffFileSummary{{Path: "gone.txt", Status: "modified"}}
		applyNameStatus(files, "D\x00gone.txt\x00")
		if files[0].Status != "deleted" {
			t.Errorf("status is %q, want \"deleted\"", files[0].Status)
		}
	})

	t.Run("renames carry a score and two paths", func(t *testing.T) {
		// "R100" plus old and new path; the new one is what the list shows.
		files := []DiffFileSummary{{Path: "new/name.go", Status: "modified"}}
		applyNameStatus(files, "R100\x00old/name.go\x00new/name.go\x00")
		if files[0].Status != "renamed" {
			t.Errorf("status is %q, want \"renamed\"", files[0].Status)
		}
	})

	t.Run("a path only name-status knows about is ignored", func(t *testing.T) {
		// Must not panic or corrupt the entries it does have.
		files := []DiffFileSummary{{Path: "known.txt", Status: "modified"}}
		applyNameStatus(files, "A\x00unknown.txt\x00M\x00known.txt\x00")
		if len(files) != 1 || files[0].Status != "modified" {
			t.Errorf("files became %+v", files)
		}
	})

	t.Run("truncated output", func(t *testing.T) {
		files := []DiffFileSummary{{Path: "a.txt", Status: "modified"}}
		applyNameStatus(files, "R100\x00only-old-path.txt\x00") // no new path
		applyNameStatus(files, "A\x00")                          // no path at all
	})
}
