//go:build !windows

package session

import "fmt"

// Installing the multiplexer for the user is Windows-only, on purpose.
//
// There, psmux is a separate download most people have not heard of, and winget
// installs it without elevation and reports what it did — so offering the
// install is both possible and useful.
//
// Here it is neither. Linux needs a system package manager, which needs a
// privilege prompt this app has no business raising, and the command differs by
// distribution. macOS needs Homebrew, which may itself not be installed, so the
// helpful action would be installing a package manager on the user's machine —
// far past what a session manager should do uninvited.
//
// Both platforms also usually have tmux already, or one familiar command away.
// Printing that command and leaving it to the user is the honest option.

// InstallMultiplexerSupported reports whether this platform can install it.
func InstallMultiplexerSupported() bool { return false }

// InstallMultiplexer is not available here.
func InstallMultiplexer() (string, error) {
	return "", fmt.Errorf("installing %s automatically is not supported on this "+
		"platform — install it with: %s", TmuxBinary(), MultiplexerInstallHint())
}
