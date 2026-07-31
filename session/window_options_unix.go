//go:build !windows

package session

// PerWindowOptionsSupported reports whether `set-option -w` stores a value
// against one window rather than sharing it across all of them. tmux scopes
// window options per window, which is what makes a per-window marker possible.
func PerWindowOptionsSupported() bool { return true }
