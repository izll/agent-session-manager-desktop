package session

import (
	"os"
	"path/filepath"
	"testing"
)

// The path is checked before a file manager is launched, because handing one a
// path that does not exist is not a visible failure: depending on which manager
// the desktop has, it either opens the user's home directory or does nothing at
// all. Both look like the button is broken rather than like the directory is
// missing.
func TestOpenInFileManagerRejectsBadPaths(t *testing.T) {
	cases := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "empty path",
			path: func(t *testing.T) string { return "" },
		},
		{
			name: "directory that does not exist",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "no-such-directory")
			},
		},
		{
			name: "a file rather than a directory",
			path: func(t *testing.T) string {
				f := filepath.Join(t.TempDir(), "a-file")
				if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
					t.Fatalf("writing the test file: %v", err)
				}
				return f
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No file manager is launched for any of these, so this stays
			// headless — the point is that the check happens first.
			if err := OpenInFileManager(tc.path(t)); err == nil {
				t.Error("expected an error, got nil; the file manager would have been " +
					"launched with a path it cannot show, which looks like nothing happened")
			}
		})
	}
}

// Errors are translation keys, not prose: the frontend looks them up, and a
// bare English sentence would reach the user untranslated.
func TestOpenInFileManagerReturnsTranslationKeys(t *testing.T) {
	err := OpenInFileManager("")
	if err == nil {
		t.Fatal("expected an error for an empty path")
	}
	const want = "error.noDirectory"
	if err.Error() != want {
		t.Errorf("error is %q, want the translation key %q — the frontend translates "+
			"this string, so anything else is shown to the user as-is", err.Error(), want)
	}
}
