package session

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestFindBackgroundAgentDoesNotHangOnCLI(t *testing.T) {
	oldTimeout := claudeBackgroundCommandTimeout
	oldCommand := claudeBackgroundCommand
	t.Cleanup(func() {
		claudeBackgroundCommandTimeout = oldTimeout
		claudeBackgroundCommand = oldCommand
	})
	claudeBackgroundCommandTimeout = 100 * time.Millisecond
	claudeBackgroundCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return CommandContext(ctx, "sh", "-c", "(sleep 30) & wait")
	}

	started := time.Now()
	if _, held := findBackgroundAgent("session-id"); held {
		t.Fatal("hung CLI reported a live background agent")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("background-agent lookup outlived its timeout: %v", elapsed)
	}
}

func TestFindBackgroundAgentRejectsTruncatedCLIOutput(t *testing.T) {
	oldCommand := claudeBackgroundCommand
	t.Cleanup(func() { claudeBackgroundCommand = oldCommand })
	claudeBackgroundCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return CommandContext(ctx, "sh", "-c", `printf '[{"id":"job","kind":"background","sessionId":"session-id"}]padding'`)
	}
	if _, held := findBackgroundAgentWithLimit("session-id", 16); held {
		t.Fatal("truncated external CLI output was parsed as a complete agent list")
	}
}
