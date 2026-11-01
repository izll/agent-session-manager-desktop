package session

import (
	"strings"
	"testing"
)

// A shell not on the list might not read the line at all, and then the command would
// not run. It gets the command as typed, and its tab stays.
func TestAnUnknownShellGetsTheCommandAsItIs(t *testing.T) {
	for _, shell := range []string{"", "nu", "pwsh", "cmd.exe", "vim"} {
		if line, leaves := commandThatLeavesItsShell("make all", shell); leaves || line != "make all" {
			t.Errorf("%q: %q, %v", shell, line, leaves)
		}
	}
	for shell, inner := range map[string]string{"bash": "bash -c", "-zsh": "zsh -c", "dash": "sh -c", "fish": "sh -c"} {
		line, leaves := commandThatLeavesItsShell("make all", shell)
		if !leaves || !strings.HasPrefix(line, inner+" ") || !strings.HasSuffix(line, "' && exit") {
			t.Errorf("%q: %q, %v", shell, line, leaves)
		}
	}
}
