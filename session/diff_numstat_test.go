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
