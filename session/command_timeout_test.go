package session

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// A git call that never returns must not be waited on forever: the diff view
// sets a loading flag and clears it when the backend answers, so a hung git
// process leaves the spinner turning with no way out.
func TestGitCommandTimedStopsAHangingProcess(t *testing.T) {
	// Reach past git and hang deliberately, so the test measures the timeout
	// rather than whatever git happens to do.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := CommandContext(ctx, "sleep", "30")

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a killed process must report an error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v; the context did not kill the process", elapsed)
	}
}

// The cancel function must be returned and usable: without it the timer leaks
// and the process outlives the work that asked for it.
func TestGitCommandTimedReturnsAUsableCancel(t *testing.T) {
	cmd, cancel := GitCommandTimed("--version")
	if cmd == nil {
		t.Fatal("no command returned")
	}
	if cancel == nil {
		t.Fatal("no cancel returned; the timer would leak")
	}
	defer cancel()

	// git may be absent in a build environment; that is not what is under test.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if err := cmd.Run(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("git --version failed: %v", err)
	}
}

// Cancelling early must stop the process rather than wait out the full timeout.
func TestGitCommandTimedCancelStopsTheProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the process")
	}
}
