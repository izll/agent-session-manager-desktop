package session

import (
	"os"
	"path/filepath"
	"testing"
)

// CanonicalProjectPath must give one answer for a directory whether or not it
// exists yet. EvalSymlinks fails on a missing path, and keeping the unresolved
// one made the answer depend on when it was asked: two processes then took
// different lock files and different tasks.json for the same project, each
// overwriting the other. macOS reaches this every time — its temporary and
// per-user directories live under a symlinked /var.
func TestCanonicalProjectPathIgnoresWhetherThePathExists(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	project := filepath.Join(link, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	whilePresent := CanonicalProjectPath(project)
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	whileAbsent := CanonicalProjectPath(project)

	if whilePresent != whileAbsent {
		t.Fatalf("canonical path changed with the directory's existence:\n  present = %q\n  absent  = %q",
			whilePresent, whileAbsent)
	}
	// And it really is resolved, not merely consistent.
	if resolvedReal, err := filepath.EvalSymlinks(real); err == nil {
		want := filepath.Join(resolvedReal, "project")
		want = CanonicalProjectPath(want)
		if whileAbsent != want {
			t.Fatalf("canonical path = %q, want the symlink resolved to %q", whileAbsent, want)
		}
	}
}

// A path with no existing ancestor at all must still come back cleaned rather
// than empty.
func TestCanonicalProjectPathHandlesAFullyMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "place")
	got := CanonicalProjectPath(missing)
	if got == "" {
		t.Fatal("canonical path of a missing directory came back empty")
	}
	if filepath.Base(got) != "place" {
		t.Fatalf("canonical path %q lost the trailing components", got)
	}
}
