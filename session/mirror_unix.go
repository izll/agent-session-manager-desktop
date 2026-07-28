//go:build !windows

package session

// MirrorSupported reports whether the multiplexer can hold one window in a
// second session. tmux links windows as the same object, which is what makes
// per-tab isolation possible.
func MirrorSupported() bool { return true }
