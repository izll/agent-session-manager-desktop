package session

import (
	"path/filepath"
	"testing"
)

// resolvedTestDir is t.TempDir() with symlinks resolved.
//
// Any test comparing a path against one the code produced needs this: macOS
// puts temp directories under /var, which is a symlink to /private/var, so the
// two forms name the same directory and differ as strings. Code that resolves
// its inputs — as most of this package does, deliberately — then returns
// something the test did not expect, on that platform only.
func resolvedTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}
