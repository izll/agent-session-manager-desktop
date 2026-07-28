//go:build windows

package session

// MirrorSupported reports whether the multiplexer can hold one window in a
// second session.
//
// psmux accepts link-window and reports success, but the window never appears
// in the target — the mirror ends up with only its own placeholder shell.
// Attaching to that showed an empty PowerShell where the agent should be.
func MirrorSupported() bool { return false }
