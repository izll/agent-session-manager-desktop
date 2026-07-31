//go:build darwin

package session

import (
	"os"
	"path/filepath"
	"strings"
)

// EnsureToolPath adds the usual macOS package-manager locations to PATH.
//
// A GUI app launched from Finder inherits almost nothing: launchctl reports an
// empty PATH, so the process falls back to a bare default that does not include
// /opt/homebrew/bin. Everything this app shells out to lives there on Apple
// Silicon — tmux above all, but also the agents themselves — so without this a
// Finder launch finds none of them while the same binary works perfectly from a
// terminal.
//
// Only missing entries are appended, and the inherited PATH keeps priority: a
// user who has deliberately put a different tmux earlier in their PATH still
// gets theirs.
func EnsureToolPath() {
	candidates := []string{
		"/opt/homebrew/bin", // Homebrew, Apple Silicon
		"/usr/local/bin",    // Homebrew, Intel; MacPorts installs here too
		"/opt/local/bin",    // MacPorts
	}
	// Where the agents themselves tend to land. Claude Code installs to
	// ~/.local/bin, and without it a Finder launch reports the agent as not
	// installed while `which claude` finds it in a terminal.
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
			filepath.Join(home, ".bun", "bin"),
		)
	}

	current := os.Getenv("PATH")
	existing := make(map[string]struct{})
	for _, dir := range filepath.SplitList(current) {
		existing[dir] = struct{}{}
	}

	added := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		if _, seen := existing[dir]; seen {
			continue
		}
		// Skip what is not there: a PATH full of non-existent directories costs
		// a stat on every lookup.
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		added = append(added, dir)
	}
	if len(added) == 0 {
		return
	}

	if current == "" {
		os.Setenv("PATH", strings.Join(added, string(os.PathListSeparator)))
		return
	}
	os.Setenv("PATH", current+string(os.PathListSeparator)+strings.Join(added, string(os.PathListSeparator)))
}
