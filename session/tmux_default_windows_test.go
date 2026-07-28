//go:build windows

package session

import "testing"

// The Windows build must look for psmux rather than tmux, which has no native
// Windows build at all. Without this an install would fail with "executable
// file not found" on every session it tried to open, with nothing to say why.
func TestWindowsDefaultsToPsmux(t *testing.T) {
	if got := TmuxBinary(); got != "psmux" {
		t.Fatalf("default multiplexer on Windows is %q, want psmux", got)
	}
}

// The default is a starting point, not a lock-in: someone running tmux through
// WSL or Cygwin has to be able to point the app at it.
func TestWindowsDefaultCanBeOverridden(t *testing.T) {
	t.Cleanup(func() { SetTmuxBinary("psmux") })

	SetTmuxBinary(`C:\tools\tmux.exe`)
	if got := TmuxBinary(); got != `C:\tools\tmux.exe` {
		t.Fatalf("override ignored, got %q", got)
	}

	// Clearing it returns to the PLATFORM default, not to "tmux" — on Windows
	// that name resolves to nothing, so a cleared setting would otherwise leave
	// the app unable to open a single session.
	SetTmuxBinary("")
	if got := TmuxBinary(); got != "psmux" {
		t.Fatalf("cleared setting resolved to %q, want the platform default", got)
	}
}
