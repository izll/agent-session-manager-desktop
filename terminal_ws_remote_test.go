package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"asmgr-desktop/session"
)

// probeExecutor records what was asked of a server and answers success, so a
// test can tell "the question reached the server" from "the question went to
// this computer".
type probeExecutor struct {
	mu       sync.Mutex
	commands [][]string
}

func (p *probeExecutor) Run(_ context.Context, args ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, append([]string(nil), args...))
	return nil
}

func (p *probeExecutor) Output(_ context.Context, args ...string) ([]byte, error) {
	return nil, p.Run(context.Background(), args...)
}

func (*probeExecutor) Describe() string { return "test server" }

func (p *probeExecutor) seen() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]string(nil), p.commands...)
}

// The bug this covers: the "is it running?" probe asked THIS computer about a
// session living on a server. It is of course not here, so the attach was
// refused with "session not running" before the remote path was reached, and
// nothing on a server could be opened at all.
func TestTheAliveProbeAsksTheMachineTheSessionLivesOn(t *testing.T) {
	executor := &probeExecutor{}
	session.SetExecutor("remote-session", executor)
	t.Cleanup(func() { session.ClearExecutor("remote-session") })

	ts := &TerminalServer{}
	inst := &session.Instance{ID: "remote-session", ServerID: "srv1"}

	if err := ts.sessionAliveProbe(context.Background(), inst, 0, "asm_remote"); err != nil {
		t.Fatalf("the probe failed although the server answered: %v", err)
	}

	// Two questions: the session, then the window inside it. A tab whose
	// window is gone must not be attached to on the strength of the session
	// still existing — that is how "can't find window N" ended up printed into
	// the pane instead of the placeholder.
	seen := executor.seen()
	if len(seen) != 2 {
		t.Fatalf("expected the session and the window to be checked, got %v", seen)
	}
	if seen[0][0] != "has-session" || seen[0][2] != "asm_remote" {
		t.Errorf("the session was not checked: %v", seen[0])
	}
	if seen[1][0] != "has-session" || seen[1][2] != "asm_remote:0" {
		t.Errorf("the window was not checked: %v", seen[1])
	}
}

// A session with no server must keep the behaviour it always had: the probe
// runs here, against this computer's multiplexer.
func TestTheAliveProbeStaysLocalForALocalSession(t *testing.T) {
	executor := &probeExecutor{}
	// Registered under the same id on purpose. A local session must not pick
	// it up — which is what would happen if the branch keyed on the executor
	// map rather than on the session's own server.
	session.SetExecutor("local-session", executor)
	t.Cleanup(func() { session.ClearExecutor("local-session") })

	ts := &TerminalServer{}
	inst := &session.Instance{ID: "local-session"}

	// The result is ignored: with no multiplexer in the test environment this
	// fails, and that is fine. What matters is where it was asked.
	_ = ts.sessionAliveProbe(context.Background(), inst, 0, "asm_local")

	if seen := executor.seen(); len(seen) != 0 {
		t.Errorf("a local session's probe was sent to a server: %v", seen)
	}
}

// The attach-time options configure the session being attached to, so they
// have to reach the machine holding it.
func TestAttachSetupReachesTheSessionsMachine(t *testing.T) {
	executor := &probeExecutor{}
	session.SetExecutor("remote-session", executor)
	t.Cleanup(func() { session.ClearExecutor("remote-session") })

	ts := &TerminalServer{}
	inst := &session.Instance{ID: "remote-session", ServerID: "srv1"}

	ts.attachSetupRun(context.Background(), inst, 0, "set-option", "-t", "asm_remote", "status", "off")

	seen := executor.seen()
	if len(seen) != 1 || seen[0][0] != "set-option" {
		t.Fatalf("the option did not reach the server: %v", seen)
	}
}

// A remote session cannot be attached through a local mirror: the mirror is
// built with local multiplexer commands against a session that is elsewhere,
// and the attach would then aim at a mirror name that exists nowhere.
//
// Asserted against the source because the decision sits in the middle of a
// long handler that needs a live WebSocket to run.
func TestRemoteSessionsSkipTheLocalMirror(t *testing.T) {
	source, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	anchor := strings.Index(text, "attachTarget := linkedName")
	if anchor < 0 {
		t.Fatal("the attach target is no longer chosen here; this test needs rewriting")
	}
	window := text[anchor:min(anchor+600, len(text))]

	if !strings.Contains(window, `inst.ServerForWindow(winIdx) != ""`) {
		t.Error("a session on a server is no longer sent straight to its own session; " +
			"it would be attached through a mirror that does not exist there")
	}
}

// The crash this prevents: closing a tab that runs on a server took the whole
// app down with a nil pointer dereference.
//
// A remote tab is attached over an SSH channel, so session.StartTerminal is
// never called for it and cmd.Process stays nil. The detach path killed that
// process unconditionally:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	os.(*Process).signal ... terminal_ws.go:1291
//
// Every place that reaches for the process has to ask whether there is one.
func TestClosingARemoteTabDoesNotKillANilProcess(t *testing.T) {
	source, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	for _, call := range []string{"cmd.Process.Kill()", "cmd.Wait()"} {
		for offset := 0; ; {
			at := strings.Index(text[offset:], call)
			if at < 0 {
				break
			}
			at += offset
			offset = at + len(call)

			// The guard has to be close above the call, in the same block.
			start := at - 400
			if start < 0 {
				start = 0
			}
			if !strings.Contains(text[start:at], "cmd.Process != nil") {
				line := 1 + strings.Count(text[:at], "\n")
				t.Errorf("terminal_ws.go:%d calls %s with no nil check above it; "+
					"a tab on a server has no local process and this panics", line, call)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// The window is checked on this computer too, not only on a server.
//
// A local tab whose window died — an agent that is not installed dies the
// instant it opens — passed the probe on the strength of the session existing.
// The attach then printed the multiplexer's own "can't find window N" into the
// pane, and the reconnect brought it back every 750ms. Measured: `has-session
// -t <session>` succeeds for a session whose window 8 is gone, while
// `has-session -t <session>:8` refuses it — so asking the narrower question is
// what turns this into the ordinary "not running" path.
func TestTheLocalProbeAsksAboutTheWindowToo(t *testing.T) {
	source, err := os.ReadFile("terminal_ws_remote.go")
	if err != nil {
		t.Fatal(err)
	}
	probe := string(source)
	at := strings.Index(probe, "func (ts *TerminalServer) sessionAliveProbe")
	if at < 0 {
		t.Fatal("sessionAliveProbe is gone; this test needs rewriting")
	}
	body := probe[at:]
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of sessionAliveProbe")
	}
	body = body[:end]

	local := body[:strings.Index(body, "inst.ExecutorOn(serverID)")]
	if !strings.Contains(local, "%s:%d") {
		t.Error("the local branch asks only whether the session exists, so a tab " +
			"whose window is gone attaches and the multiplexer prints its own error")
	}
}
