//go:build !windows

package session

// RunningTabMoveSupported reports whether a window with a process in it can be
// moved from one session to another on this computer. tmux re-parents the
// window object with move-window, keeping its pane and process.
func RunningTabMoveSupported() bool { return true }
