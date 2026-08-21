package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenameWindowTargetsExactIndexAndPersistsFollowedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test multiplexer shim is a POSIX shell script")
	}
	restoreTmuxBinary(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	shim := filepath.Join(dir, "tmux-shim")
	body := `#!/bin/sh
case "$1" in
  list-windows) printf '2\n' ;;
  display-message) printf 'old-name\n' ;;
  rename-window) printf '%s\n' "$*" >> "$ASMGR_TMUX_LOG" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(shim, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASMGR_TMUX_LOG", logPath)
	SetTmuxBinary(shim)

	inst := &Instance{
		ID: "asm_session", Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 2, Name: "old-name", Agent: AgentTerminal}},
	}
	oldName, err := inst.RenameWindow(2, "new name")
	if err != nil {
		t.Fatal(err)
	}
	if oldName != "old-name" || inst.FollowedWindows[0].Name != "new name" {
		t.Fatalf("rename metadata = old:%q window:%+v", oldName, inst.FollowedWindows[0])
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := string(calls)
	if !strings.Contains(line, "rename-window -t asm_session:2 new name") {
		t.Fatalf("rename did not target exact window: %q", line)
	}
	if strings.Contains(line, "select-window") {
		t.Fatalf("rename still depends on racy active-window selection: %q", line)
	}
}
