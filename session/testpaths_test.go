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

// isolateHome points os.UserHomeDir() at a throwaway directory.
//
// t.Setenv("HOME", ...) alone is not enough: os.UserHomeDir reads USERPROFILE on
// Windows, so a test setting only HOME kept using the real profile — and wrote
// the developer's actual session store while doing it. That is how a stray
// "{ this is not json" from one test made every later run fail there.
//
// Returns the directory so callers can look inside it.
func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // os.UserHomeDir reads this on Windows
	return dir
}
