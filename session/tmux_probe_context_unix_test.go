//go:build !windows

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleTmuxProbesHonorCancellation(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "wedged-tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBinary := TmuxBinary()
	SetTmuxBinary(bin)
	t.Cleanup(func() { SetTmuxBinary(oldBinary) })

	inst := &Instance{ID: "context-probe", Agent: AgentClaude}
	assertPrompt := func(name string, call func(context.Context)) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
		defer cancel()
		started := time.Now()
		call(ctx)
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("%s ignored cancellation for %v", name, elapsed)
		}
	}

	assertPrompt("IsAlive", func(ctx context.Context) { _ = inst.IsAliveContext(ctx) })
	assertPrompt("capture target", func(ctx context.Context) { _ = inst.GetCaptureTargetContext(ctx, 0) })
	assertPrompt("activity", func(ctx context.Context) { _, _ = inst.DetectActivityForWindowWithValidityContext(ctx, 0) })
	assertPrompt("status", func(ctx context.Context) { _ = inst.GetStatusInfoForWindowContext(ctx, 0, AgentClaude) })
	assertPrompt("yolo", func(ctx context.Context) { _ = inst.DetectYoloForWindowContext(ctx, 0) })
	assertPrompt("dead pane", func(ctx context.Context) { _ = inst.IsMainWindowDeadContext(ctx) })
	assertPrompt("codex resume", func(ctx context.Context) { _ = DetectCodexSessionIDFromTmuxContext(ctx, inst.ID, 0, "") })
}
