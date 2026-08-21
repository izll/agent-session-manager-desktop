//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestClaudePaneProbeHonorsCancellation(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "wedged-tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBinary := session.TmuxBinary()
	session.SetTmuxBinary(bin)
	t.Cleanup(func() { session.SetTmuxBinary(oldBinary) })

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	if got := getClaudeSessionIDFromTmuxWindowContext(ctx, "asm-test", 0); got != "" {
		t.Fatalf("cancelled probe returned %q", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Claude pane probe ignored cancellation for %v", elapsed)
	}
}
