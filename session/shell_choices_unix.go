//go:build !windows

package session

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ShellChoice is one option offered for what a terminal tab runs.
type ShellChoice struct {
	// Command is stored in settings; empty means the system default.
	Command string `json:"command"`
	// Label names it in the interface. Empty means the frontend supplies a
	// translated "system default" label.
	Label string `json:"label"`
}

// Shells that live in /etc/shells but are not what anyone means by "my shell".
// tmux and screen are there because chsh accepts them, and starting a
// multiplexer inside a multiplexer pane is not a useful tab; the restricted
// variants exist for locked-down accounts rather than for choosing.
var nonInteractiveShells = map[string]bool{
	"tmux":    true,
	"screen":  true,
	"rbash":   true,
	"rzsh":    true,
	"nologin": true,
	"false":   true,
	"sync":    true,
}

// ShellChoices lists the shells worth offering on this platform.
//
// It matters here for a subtler reason than on Windows. Creating a tab passes
// no command and lets tmux start its default-shell, which comes from the
// account's passwd entry; restarting one has to name a command, and used
// $SHELL. Those are not the same value — $SHELL can be overridden by a profile
// or a terminal emulator, and default-shell can be set in tmux.conf — so a tab
// could come back running a different shell from the one it was created with.
// Naming the shell explicitly makes both paths agree.
//
// Read from /etc/shells, the system's own list of login shells, which exists
// on both Linux and macOS.
func ShellChoices() []ShellChoice {
	choices := []ShellChoice{{Command: "", Label: ""}}

	for _, path := range installedShells() {
		choices = append(choices, ShellChoice{
			Command: path,
			Label:   filepath.Base(path),
		})
	}
	return choices
}

// installedShells returns the interactive shells this machine has, one entry
// per shell name — /etc/shells lists /bin/bash and /usr/bin/bash separately,
// and offering both as though they were a choice is noise.
func installedShells() []string {
	file, err := os.Open("/etc/shells")
	if err != nil {
		return nil
	}
	defer file.Close()

	byName := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !filepath.IsAbs(line) {
			continue
		}

		name := filepath.Base(line)
		if nonInteractiveShells[name] {
			continue
		}
		// Listed but not installed: /etc/shells is a static file, and a package
		// can be removed without it being rewritten.
		if info, err := os.Stat(line); err != nil || info.IsDir() {
			continue
		}
		// Prefer the shortest path for a given shell, which is the canonical
		// one on both Linux (/bin/bash) and macOS (/bin/zsh).
		if existing, seen := byName[name]; !seen || len(line) < len(existing) {
			byName[name] = line
		}
	}

	paths := make([]string, 0, len(byName))
	for _, path := range byName {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(a, b int) bool {
		return filepath.Base(paths[a]) < filepath.Base(paths[b])
	})
	return paths
}
