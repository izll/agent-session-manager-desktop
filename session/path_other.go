//go:build !darwin

package session

// EnsureToolPath is a no-op away from macOS: Linux and Windows GUI launches
// inherit a usable PATH, so there is nothing to repair.
func EnsureToolPath() {}
