package main

import (
	"testing"

	"asmgr-desktop/session"
)

// What the tab bar shows for a runner tab, and — the point of the three-way
// answer — that a failed build is not reported as simply finished.
func TestRunStateForDistinguishesFailureFromFinishing(t *testing.T) {
	live := session.WindowInfo{Index: 3, Name: "server", Dead: false, ExitStatus: -1}
	okExit := session.WindowInfo{Index: 3, Name: "server", Dead: true, ExitStatus: 0}
	badExit := session.WindowInfo{Index: 3, Name: "server", Dead: true, ExitStatus: 1}
	signalled := session.WindowInfo{Index: 3, Name: "server", Dead: true, ExitStatus: -1}
	var absent session.WindowInfo

	cases := []struct {
		name    string
		command string
		stopped bool
		pane    session.WindowInfo
		want    string
	}{
		// A tab with no command is not a runner and gets no control at all.
		{"not a runner", "", false, live, ""},
		{"not a runner even when stopped", "", true, absent, ""},

		{"running", "./gradlew bootRun", false, live, "running"},
		{"finished cleanly", "make build", false, okExit, "stopped"},
		{"exited with an error", "make build", false, badExit, "failed"},

		// ■ kills the pane, which leaves no exit status. The user asked for
		// that, so it is stopped rather than a failure.
		{"stopped by the user", "./gradlew bootRun", false, signalled, "stopped"},

		// The stored flag wins: a parked tab is stopped whatever a stale
		// listing says.
		{"parked tab", "./gradlew bootRun", true, live, "stopped"},

		// No listing for the index: the window is gone, which is the same
		// position as never started.
		{"window gone", "npm run dev", false, absent, "stopped"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runStateFor(c.command, c.stopped, c.pane); got != c.want {
				t.Errorf("runStateFor(%q, %v, %+v) = %q, want %q",
					c.command, c.stopped, c.pane, got, c.want)
			}
		})
	}
}
