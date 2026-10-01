//go:build windows

package session

// RunningTabMoveSupported reports whether a window with a process in it can be
// moved from one session to another on this computer.
//
// Not with psmux: it runs a server per session, and a window cannot cross from
// one server to another — the same reason link-window reports success and
// delivers nothing (see MirrorSupported). A running tab on this computer is
// refused rather than restarted behind the user's back; a stopped one moves as
// its record, and a tab on a server moves there, where the multiplexer is tmux.
func RunningTabMoveSupported() bool { return false }
