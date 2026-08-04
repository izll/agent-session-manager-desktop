package main

import (
	"os"
	"strings"
	"testing"
)

// The pane's own process has to be inspected, not only its children.
//
// A tab runs its agent directly, so tmux reports the AGENT's pid as the pane
// pid, and the children are the MCP servers the agent spawned. Walking only
// children therefore looked at every MCP server and never at the agent, and
// detection came back empty for every tab.
//
// That stayed hidden for as long as detection only ever had to confirm an id we
// had put on the command line ourselves at launch: the value was already known,
// so nothing looked broken. What it could not do is notice the user moving the
// tab to a different conversation — which is the whole reason detection exists.
func TestClaudeDetectionInspectsThePaneProcess(t *testing.T) {
	b, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("reading app.go: %v", err)
	}
	src := string(b)

	start := strings.Index(src, "func getClaudeSessionIDFromTmuxWindow(")
	if start < 0 {
		t.Fatal("getClaudeSessionIDFromTmuxWindow not found; if it was renamed, update this test")
	}
	body := src[start : start+strings.Index(src[start:], "\n}\n")]

	// The pane pid must reach the candidate list, not just seed a pgrep.
	if !strings.Contains(body, "[]string{panePID}") {
		t.Error("the pane's own pid is not among the candidates. A tab runs its " +
			"agent directly, so that pid IS the agent; searching only its " +
			"children finds the MCP servers and never the agent itself")
	}
}
