//go:build !windows

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStopContextBoundsWedgedMultiplexerAndPreservesRunningState(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "wedged-tmux")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldBinary := TmuxBinary()
	SetTmuxBinary(command)
	t.Cleanup(func() { SetTmuxBinary(oldBinary) })

	instance := &Instance{ID: "bounded-stop", Status: StatusRunning, Agent: AgentClaude}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := instance.StopContext(ctx)
	if err == nil || !strings.Contains(err.Error(), "timed out stopping tmux session") {
		t.Fatalf("StopContext error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("StopContext took %v after cancellation", elapsed)
	}
	if instance.Status != StatusRunning {
		t.Fatalf("failed stop published status %q, want running", instance.Status)
	}
}

func TestStopWindowContextBoundsWedgedLookupAndPreservesWindowState(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "wedged-tmux")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldBinary := TmuxBinary()
	SetTmuxBinary(command)
	t.Cleanup(func() { SetTmuxBinary(oldBinary) })

	instance := &Instance{ID: "bounded-window-stop", Status: StatusRunning, Agent: AgentClaude}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := instance.StopWindowContext(ctx, 0)
	if err == nil {
		t.Fatal("StopWindowContext unexpectedly succeeded against wedged multiplexer")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("StopWindowContext took %v after cancellation", elapsed)
	}
	if instance.MainWindowStopped || instance.Status != StatusRunning {
		t.Fatalf("failed window stop changed state: mainStopped=%v status=%q", instance.MainWindowStopped, instance.Status)
	}
}
