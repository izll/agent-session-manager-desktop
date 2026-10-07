//go:build windows

package session

// paneHasChildProcess has no cheap answer on Windows: the multiplexer there
// is psmux, whose pane process model does not expose a child list the way
// /proc or pgrep do.
//
// Reporting false would make a running command look finished, which is the
// dangerous direction: ▶ would start a second one on top of the first. So
// the caller falls back to what the pane's current command says, which is
// right whenever the command is a real program and wrong only for a script
// wrapper — a worse answer than on Unix, but never one that double-starts.
func paneHasChildProcess(panePID int) bool {
	return false
}

// paneChildrenKnown says whether paneHasChildProcess can answer at all, so
// the caller knows to fall back rather than trust a false.
const paneChildrenKnown = false
