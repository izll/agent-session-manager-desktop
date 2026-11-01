//go:build !windows

// The helper runs on the servers a session can be placed on, which are Linux
// and macOS — it is never built for or shipped to Windows. Its tests drive a
// real shell, so on Windows they fail for want of /bin/sh rather than for
// anything about the code.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/remote/protocol"
	"asmgr-desktop/tokenusage"
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

// A server answers with numbers, from logs under its own home.
func TestUsageMethodsReadTheServersLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	rel := "projects/-work/aaaaaaaa-0000-0000-0000-000000000001.jsonl"
	full := filepath.Join(home, ".claude", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","requestId":"r","timestamp":"2026-10-20T10:00:00Z","cwd":"/work",` +
		`"message":{"id":"m","model":"claude-opus-5-5","content":"secret words","usage":{"input_tokens":3,"output_tokens":4}}}` + "\n"
	if err := os.WriteFile(full, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	listed := handle(&protocol.Request{ID: 1, Method: protocol.MethodUsageFiles})
	if listed.Error != "" {
		t.Fatal(listed.Error)
	}
	var files protocol.UsageFilesResult
	if err := json.Unmarshal(listed.Result, &files); err != nil {
		t.Fatal(err)
	}
	if len(files.Files) != 1 {
		t.Fatalf("files = %+v", files.Files)
	}

	params, _ := json.Marshal(protocol.UsageReadParams{Requests: []tokenusage.ReadRequest{{Path: files.Files[0].Path}}})
	read := handle(&protocol.Request{ID: 2, Method: protocol.MethodUsageRead, Params: params})
	if read.Error != "" {
		t.Fatal(read.Error)
	}
	if strings.Contains(string(read.Result), "secret") {
		t.Fatalf("conversation text crossed the wire: %s", read.Result)
	}
	var result protocol.UsageReadResult
	if err := json.Unmarshal(read.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || len(result.Results[0].Rows) != 1 || result.Results[0].Rows[0].Output != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageReadRejectsBadParameters(t *testing.T) {
	response := handle(&protocol.Request{ID: 1, Method: protocol.MethodUsageRead, Params: json.RawMessage(`{"requests":"no"}`)})
	if response.Error == "" {
		t.Fatal("bad parameters were accepted")
	}
}
