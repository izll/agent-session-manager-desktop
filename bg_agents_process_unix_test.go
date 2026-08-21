//go:build !windows

package main

import (
	"bytes"
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

	"asmgr-desktop/session"
)

func TestBackgroundAgentWaitDelayKillsDetachedCLIHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := session.CommandContext(ctx, "sh", "-c",
		`sleep 30 & child=$!; printf '%s' "$child" > "$PID_FILE"; exit 0`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	cmd.Stdout = &bytes.Buffer{}
	configureBackgroundAgentCommand(cmd)
	cmd.WaitDelay = 50 * time.Millisecond
	err := runBackgroundAgentCommand(cmd)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("detached output-pipe wait returned %v, want ErrWaitDelay", err)
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
	t.Fatalf("detached Claude helper %d survived WaitDelay cleanup: %v", pid, err)
}
