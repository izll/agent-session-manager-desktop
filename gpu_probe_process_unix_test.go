//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGPUProbeTimeoutKillsRendererDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "renderer.pid")
	cmd := exec.CommandContext(ctx, "sh", "-c",
		`sleep 30 & child=$!; printf '%s' "$child" > "$PID_FILE"; wait`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	configureGPUProbeCommand(cmd)
	_ = runGPUProbeCommand(cmd)

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("probe did not publish renderer pid: %v", err)
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
	t.Fatalf("renderer descendant %d survived GPU probe timeout: %v", pid, err)
}

func TestGPUProbeCrashKillsDetachedRenderer(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "renderer.pid")
	cmd := exec.CommandContext(context.Background(), "sh", "-c",
		`sleep 30 & child=$!; printf '%s' "$child" > "$PID_FILE"; sleep 0.05; exit 1`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	configureGPUProbeCommand(cmd)
	runErr := runGPUProbeCommand(cmd)
	if runErr == nil {
		t.Fatal("crashing probe unexpectedly succeeded")
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("crashing probe did not start normally: %v", runErr)
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
	t.Fatalf("renderer descendant %d survived direct GPU probe crash: %v", pid, err)
}
