package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/remote/protocol"
)

// The helper runs commands through a login shell, because that is the only way
// an agent installed in ~/.local/bin or through nvm is on the PATH at all. The
// cost is that the shell is entitled to print things of its own — "mesg:
// ttyname failed" from a stock Ubuntu .profile, measured on a real server —
// and mixed into the output those would corrupt every answer the app parses.
func TestRunSeparatesOutputFromShellNoise(t *testing.T) {
	params, err := json.Marshal(protocol.RunParams{
		Command: "echo the-answer; echo noise >&2",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := runCommand(params)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var result protocol.RunResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(result.Output) != "the-answer" {
		t.Errorf("output = %q; anything else means stderr leaked into it", result.Output)
	}
	if !strings.Contains(result.Stderr, "noise") {
		t.Errorf("stderr = %q; error messages are worth keeping, not discarding",
			result.Stderr)
	}
}

// A command that fails is an answer, not a fault: `command -v claude` exiting
// non-zero is how the app learns the agent is not installed there.
func TestNonZeroExitIsReportedNotRaised(t *testing.T) {
	params, _ := json.Marshal(protocol.RunParams{Command: "exit 3"})

	raw, err := runCommand(params)
	if err != nil {
		t.Fatalf("a failing command was reported as a protocol error: %v", err)
	}
	var result protocol.RunResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", result.ExitCode)
	}
}

// A command with no limit can hold the connection open forever, and the app
// has no way to cancel one it cannot see.
func TestCommandsTimeOutRatherThanHang(t *testing.T) {
	params, _ := json.Marshal(protocol.RunParams{
		Command:   "sleep 30",
		TimeoutMs: 200,
	})

	raw, err := runCommand(params)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var result protocol.RunResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut {
		t.Error("a command that outlived its timeout was not reported as timed out")
	}
}

// The timeout has to reach what the shell started, not just the shell.
//
// Killing only /bin/sh leaves its children running on the server, unreachable
// and unaccounted for — a `sleep 30` behind a 200 ms timeout went on for the
// full thirty seconds, and the helper waited for it. The elapsed time is the
// assertion: it is the only thing that tells the two cases apart.
func TestTimeoutKillsWhatTheShellStarted(t *testing.T) {
	params, _ := json.Marshal(protocol.RunParams{
		Command:   "sleep 30",
		TimeoutMs: 200,
	})

	start := time.Now()
	if _, err := runCommand(params); err != nil {
		t.Fatalf("run: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("the command took %v to give up; the timeout reached the shell "+
			"but not the sleep it started", elapsed.Round(time.Millisecond))
	}
}

func TestEmptyCommandIsRefused(t *testing.T) {
	params, _ := json.Marshal(protocol.RunParams{})
	if _, err := runCommand(params); err == nil {
		t.Error("an empty command was accepted")
	}
}

// Requests are paired by id so several can be in flight at once — the sidebar
// asks about every session at the same time. A response that lost its id would
// be delivered to whoever asked last.
func TestResponsesKeepTheirRequestID(t *testing.T) {
	for _, id := range []int64{1, 42, 9007199254740991} {
		response := handle(&protocol.Request{ID: id, Method: protocol.MethodPing})
		if response.ID != id {
			t.Errorf("response id = %d, want %d", response.ID, id)
		}
		if response.Error != "" {
			t.Errorf("ping failed: %s", response.Error)
		}
	}
}

// A method the helper does not know means the two ends are different versions,
// and the message has to say so — "unknown method" alone sends the reader
// looking for a bug that is not there.
func TestUnknownMethodExplainsTheVersionMismatch(t *testing.T) {
	response := handle(&protocol.Request{ID: 1, Method: "teleport"})
	if response.Error == "" {
		t.Fatal("an unknown method was accepted")
	}
	if !strings.Contains(response.Error, "version") {
		t.Errorf("error = %q; it does not point at a version mismatch", response.Error)
	}
}

func TestVersionReportsTheProtocol(t *testing.T) {
	response := handle(&protocol.Request{ID: 1, Method: protocol.MethodVersion})
	if response.Error != "" {
		t.Fatalf("version failed: %s", response.Error)
	}

	var result protocol.VersionResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Protocol != protocol.Version {
		t.Errorf("protocol = %d, want %d", result.Protocol, protocol.Version)
	}
	if result.Arch == "" {
		t.Error("the architecture is missing; a mismatch would surface as " +
			"'exec format error' with nothing to explain it")
	}
}
