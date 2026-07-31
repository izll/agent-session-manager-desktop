//go:build windows

package session

// PerWindowOptionsSupported reports whether `set-option -w` stores a value
// against one window rather than sharing it across all of them.
//
// psmux does not: setting a user option with -w on a single window makes every
// window in the session report that value. Measured — @asmgr_probe set only on
// window 1, then read back from windows 0, 1 and 2, all returning it.
//
// So a per-window marker cannot exist here. Anything that would write one has
// to fall back to identifying windows by what they are, not by a flag.
func PerWindowOptionsSupported() bool { return false }
