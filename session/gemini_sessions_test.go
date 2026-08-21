package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestListGeminiSessionsBoundsCommandRuntimeAndOutput(t *testing.T) {
	oldCommand := geminiSessionCommand
	oldTimeout := geminiSessionListTimeout
	t.Cleanup(func() {
		geminiSessionCommand = oldCommand
		geminiSessionListTimeout = oldTimeout
	})

	geminiSessionCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestGeminiSessionHelperProcess", "--")
	}

	t.Run("timeout", func(t *testing.T) {
		geminiSessionListTimeout = 50 * time.Millisecond
		t.Setenv("ASMGR_GEMINI_HELPER", "sleep")
		started := time.Now()
		got, err := ListGeminiSessions(t.TempDir())
		if err != nil || len(got) != 0 {
			t.Fatalf("timed out listing = %#v, %v", got, err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("Gemini listing ignored its timeout: %s", elapsed)
		}
	})

	t.Run("output cap", func(t *testing.T) {
		geminiSessionListTimeout = 5 * time.Second
		t.Setenv("ASMGR_GEMINI_HELPER", "output")
		got, err := ListGeminiSessions(t.TempDir())
		if err != nil || len(got) != 0 {
			t.Fatalf("truncated listing = %#v, %v", got, err)
		}
	})
}

func TestGeminiSessionHelperProcess(t *testing.T) {
	if os.Getenv("ASMGR_GEMINI_HELPER") == "" {
		return
	}
	switch os.Getenv("ASMGR_GEMINI_HELPER") {
	case "sleep":
		time.Sleep(10 * time.Second)
	case "output":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", geminiSessionListOutputLimit+1))
	}
	os.Exit(0)
}
