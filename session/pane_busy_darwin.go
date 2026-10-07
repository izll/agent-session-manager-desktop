//go:build darwin

package session

import (
	"os/exec"
	"strconv"
	"strings"
)

// paneHasChildProcess reports whether the pane's shell has started anything.
//
// See the Linux file for why this question rather than pane_dead or
// pane_current_command. Darwin has no /proc, so this asks pgrep, which costs
// a process per runner tab per second — acceptable because only a tab that
// is actually a runner is ever asked.
func paneHasChildProcess(panePID int) bool {
	if panePID <= 0 {
		return false
	}
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(panePID)).Output()
	if err != nil {
		// pgrep exits non-zero when it matches nothing, which is the
		// common case of an idle prompt.
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// paneChildrenKnown says whether paneHasChildProcess can answer at all.
const paneChildrenKnown = true
