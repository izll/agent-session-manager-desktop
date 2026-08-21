//go:build !windows

package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBackgroundAgentTimeoutKillsDescendantProcessGroup(t *testing.T) {
	oldTimeout := claudeBackgroundCommandTimeout
	oldCommand := claudeBackgroundCommand
	t.Cleanup(func() {
		claudeBackgroundCommandTimeout = oldTimeout
		claudeBackgroundCommand = oldCommand
	})
	claudeBackgroundCommandTimeout = 100 * time.Millisecond
	pidFile := t.TempDir() + "/child.pid"
	claudeBackgroundCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return CommandContext(ctx, "sh", "-c", `sleep 30 & child=$!; printf '%s' "$child" > "$PID_FILE"; wait`)
	}

	cancel, cmd := timedClaudeBackgroundCommand("agents", "--json")
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	_ = runTimedClaudeBackgroundCommand(cmd, cancel)
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("probe did not publish descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived timed Claude probe: %v", pid, err)
}

func TestBackgroundAgentWaitDelayKillsDetachedDescendant(t *testing.T) {
	oldTimeout := claudeBackgroundCommandTimeout
	oldCommand := claudeBackgroundCommand
	t.Cleanup(func() {
		claudeBackgroundCommandTimeout = oldTimeout
		claudeBackgroundCommand = oldCommand
	})
	claudeBackgroundCommandTimeout = 5 * time.Second
	pidFile := t.TempDir() + "/child.pid"
	claudeBackgroundCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return CommandContext(ctx, "sh", "-c", `sleep 30 & child=$!; printf '%s' "$child" > "$PID_FILE"; exit 0`)
	}

	cancel, cmd := timedClaudeBackgroundCommand("agents", "--json")
	cmd.WaitDelay = 50 * time.Millisecond
	cmd.Stdout = &bytes.Buffer{}
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	err := runTimedClaudeBackgroundCommand(cmd, cancel)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("detached pipe wait returned %v, want ErrWaitDelay", err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("detached descendant process %d survived WaitDelay cleanup: %v", pid, err)
}
