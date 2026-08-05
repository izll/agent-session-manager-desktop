package session

import (
	"strings"
	"sync"
)

// Which shell a plain terminal tab starts.
//
// Restarting a stopped pane has to name a command: respawn-pane without one
// re-runs whatever the pane was started with, which for a stopped tab is
// "exit 0" — it would exit again immediately. Creating a tab has no such
// problem, because creation passes no command and lets the multiplexer start
// the shell itself.
//
// The configured value wins, and the per-platform default fills in when there
// is none. It matters most on Windows, where "the shell" is genuinely
// ambiguous: cmd.exe and PowerShell are both reasonable and the environment
// does not say which the user wants.

var (
	shellMu         sync.RWMutex
	configuredShell string
)

// SetTerminalShell records the shell a terminal tab should start. An empty
// value restores the platform default.
func SetTerminalShell(command string) {
	shellMu.Lock()
	defer shellMu.Unlock()
	configuredShell = strings.TrimSpace(command)
}

// TerminalShell reports the configured shell, empty when the platform default
// is in use.
func TerminalShell() string {
	shellMu.RLock()
	defer shellMu.RUnlock()
	return configuredShell
}

// defaultShell returns the command that starts an interactive shell.
func defaultShell() string {
	if configured := TerminalShell(); configured != "" {
		return configured
	}
	return platformDefaultShell()
}
