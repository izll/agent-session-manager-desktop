//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestopWindowPreservesSessionAndMarksOriginalPaneStopped(t *testing.T) {
	restoreTmuxBinary(t)
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "tmux.log")
	shim := filepath.Join(tmp, "tmux-shim")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ASMGR_TMUX_LOG"
`
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASMGR_TMUX_LOG", logPath)
	SetTmuxBinary(shim)

	inst := &Instance{
		ID:                "rollback-session",
		Status:            StatusRunning,
		MainWindowStopped: false,
		FollowedWindows: []FollowedWindow{
			{Index: 4, Agent: AgentTerminal},
		},
	}
	if err := inst.RestopWindow(4); err != nil {
		t.Fatal(err)
	}
	if !inst.FollowedWindows[0].Stopped || inst.MainWindowStopped {
		t.Fatalf("followed rollback state = followed:%v main:%v", inst.FollowedWindows[0].Stopped, inst.MainWindowStopped)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(data)
	target := inst.TmuxSessionName() + ":4"
	if !strings.Contains(commands, "set-option -w -t "+target+" remain-on-exit on") ||
		!strings.Contains(commands, "respawn-pane -k -t "+target+" exit 0") {
		t.Fatalf("rollback commands = %q", commands)
	}
	if strings.Contains(commands, "kill-session") || strings.Contains(commands, "kill-window") {
		t.Fatalf("rollback destroyed the containing tmux session/window: %q", commands)
	}

	if err := inst.RestopWindow(0); err != nil {
		t.Fatal(err)
	}
	if !inst.MainWindowStopped {
		t.Fatal("main pane rollback did not restore stopped metadata")
	}
}
