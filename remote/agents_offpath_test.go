package remote

import (
	"os"
	"strings"
	"testing"
)

// The case that sent the user here: Claude is installed on the server and runs
// perfectly, but lives in ~/.local/bin, which a non-interactive SSH shell does
// not have on its PATH. The test reported "no agents found", and the session
// dialog then refused to start a session that would have worked.
//
// Finding it there turns that into a line naming the directory to put in the
// extra PATH setting.
func TestAnAgentInstalledOffThePathIsStillFound(t *testing.T) {
	dir, names := firstDirectoryWithAgents("/root/.local/bin claude\n")

	if dir != "/root/.local/bin" {
		t.Errorf("directory = %q", dir)
	}
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("names = %v", names)
	}
}

// Several agents in one place are reported together, so the user learns what
// that one PATH entry would bring in.
func TestAgentsInOneDirectoryAreReportedTogether(t *testing.T) {
	_, names := firstDirectoryWithAgents(
		"/root/.local/bin claude\n/root/.local/bin codex\n")

	if len(names) != 2 || names[0] != "claude" || names[1] != "codex" {
		t.Errorf("names = %v", names)
	}
}

// With agents in more than one place, the first is recommended — the probe
// asks in order of likelihood, so that is the one worth naming.
func TestTheFirstDirectoryWins(t *testing.T) {
	dir, names := firstDirectoryWithAgents(
		"/root/.local/bin claude\n/usr/local/bin gemini\n")

	if dir != "/root/.local/bin" {
		t.Errorf("directory = %q, want the first one probed", dir)
	}
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("names = %v, want only the first directory's", names)
	}
}

// Nothing found stays nothing found: the caller falls back to saying the
// server is bare, which is the honest answer for a machine with no agent.
func TestNothingFoundReportsNothing(t *testing.T) {
	if dir, names := firstDirectoryWithAgents("   \n\n"); dir != "" || names != nil {
		t.Errorf("got %q %v from empty output", dir, names)
	}
}

// A line the probe did not produce — a shell notice, a stray warning — must
// not be read as a directory.
func TestNoiseIsIgnored(t *testing.T) {
	dir, _ := firstDirectoryWithAgents(
		"mesg: ttyname failed: Inappropriate ioctl for device\n/root/bin claude\n")

	if dir != "/root/bin" {
		t.Errorf("directory = %q; a noise line was taken for output", dir)
	}
}

// The probe must not report failure just because it found nothing.
//
// Each agent is tested with `command -v X && echo X`, and the exit status of
// the whole line is the last test's — so a server with no agents on its PATH
// answered with status 1. That was read as "the server could not be asked",
// the step reported itself skipped, and the search for agents installed off
// the PATH never ran: exactly the case it exists for. A trailing `true` makes
// "found none" a successful answer.
func TestTheAgentProbeSucceedsWhenNothingIsFound(t *testing.T) {
	source, err := os.ReadFile("check.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	for _, function := range []string{
		"func agentStatus(",
		"func agentsOffThePath(",
	} {
		at := strings.Index(text, function)
		if at < 0 {
			t.Fatalf("%s is gone; this test needs rewriting", function)
		}
		end := strings.Index(text[at:], "\n}\n")
		body := text[at : at+end]

		if !strings.Contains(body, `builder.WriteString("true")`) {
			t.Errorf("%s can exit non-zero when it simply found nothing, "+
				"which reads as a server that could not be asked", function)
		}
	}
}
