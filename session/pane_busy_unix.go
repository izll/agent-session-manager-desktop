//go:build linux

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// paneHasChildProcess reports whether the pane's shell has started anything.
//
// This is how a runner tab knows its command is still going. The obvious
// answers do not work: pane_dead stays 0 because the shell outlives the
// command, and pane_current_command reports "sh" both for an idle prompt and
// for a running shell script — which is what a Gradle or Maven wrapper is.
// A shell at its prompt has no children; one running anything has at least
// one.
//
// Read from /proc rather than by running pgrep: this is asked once per
// runner tab per second, and spawning a process for it would cost more than
// the answer. Darwin has no such file and uses pgrep instead.
func paneHasChildProcess(panePID int) bool {
	if panePID <= 0 {
		return false
	}
	// The thread group's children file lists the direct children of every
	// thread in the process, which for a shell is the one thread.
	entries, err := filepath.Glob("/proc/" + strconv.Itoa(panePID) + "/task/*/children")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) != "" {
			return true
		}
	}
	return false
}

// paneChildrenKnown says whether paneHasChildProcess can answer at all.
const paneChildrenKnown = true
