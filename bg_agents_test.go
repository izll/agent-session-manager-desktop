package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestBoundedBackgroundAgentOutputKeepsNewestLogTail(t *testing.T) {
	output := &boundedCommandOutput{limit: 8, tail: true}
	if _, err := output.Write([]byte("old-")); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("new-tail")); err != nil {
		t.Fatal(err)
	}
	got, truncated := output.Bytes()
	if !truncated || string(got) != "new-tail" {
		t.Fatalf("tail = %q truncated=%v, want newest bounded bytes", got, truncated)
	}
}

func TestStopBackgroundAgentTimesOutHungCLI(t *testing.T) {
	oldTimeout := backgroundAgentCommandTimeout
	oldCommand := backgroundAgentCommand
	t.Cleanup(func() {
		backgroundAgentCommandTimeout = oldTimeout
		backgroundAgentCommand = oldCommand
	})
	backgroundAgentCommandTimeout = 100 * time.Millisecond
	backgroundAgentCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return session.CommandContext(ctx, "sh", "-c", "sleep 30")
	}
	app := &App{storage: guardedTestStorage(t), projectLocked: true}

	started := time.Now()
	err := app.StopBackgroundAgent("abcdef", "")
	if err == nil {
		t.Fatal("hung background-agent CLI unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("hung CLI was not cancelled promptly: %v", elapsed)
	}
}

func TestBackgroundAgentLogTimeoutReapsDescendantHoldingOutputPipe(t *testing.T) {
	oldTimeout := backgroundAgentCommandTimeout
	oldCommand := backgroundAgentCommand
	t.Cleanup(func() {
		backgroundAgentCommandTimeout = oldTimeout
		backgroundAgentCommand = oldCommand
	})
	backgroundAgentCommandTimeout = 100 * time.Millisecond
	backgroundAgentCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return session.CommandContext(ctx, "sh", "-c", "(sleep 30) & wait")
	}

	started := time.Now()
	_, err := (&App{}).GetBackgroundAgentLogs("abcdef")
	if err == nil {
		t.Fatal("hung background-agent log command unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("descendant kept the output pipe alive after cancellation: %v", elapsed)
	}
}

func TestBackgroundAgentMutationsUsePersistenceRollback(t *testing.T) {
	text := readTextFile(t, "bg_agents.go")
	if strings.Count(text, "persistOrRollbackExternalMutation(") < 2 {
		t.Fatal("background-agent session and tab attach paths do not both compensate persistence failures")
	}
}
