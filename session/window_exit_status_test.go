package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// listWindowsWith runs listWindowsOn against a multiplexer shim that reports
// the given list-windows output.
func listWindowsWith(t *testing.T, output string, inst *Instance) []WindowInfo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test multiplexer shim is a POSIX shell script")
	}
	restoreTmuxBinary(t)
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux-shim")
	body := "#!/bin/sh\ncase \"$1\" in\n  list-windows) printf '%s' \"$LIST_WINDOWS_OUT\" ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(shim, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIST_WINDOWS_OUT", output)
	SetTmuxBinary(shim)
	return inst.listWindowsOn("")
}

// A tab named with a colon must not shift the fields that follow it. The name
// is the only field that can contain one, which is why it is parsed last:
// with it in the middle, "a:b" was read as the name "a" and the dead flag
// came out of the name's own tail.
func TestListWindowsKeepsColonsInNames(t *testing.T) {
	inst := &Instance{ID: "asm_session", Status: StatusRunning}
	windows := listWindowsWith(t, "0:1:0::build: api\n", inst)

	if len(windows) != 1 {
		t.Fatalf("got %d windows, want 1: %+v", len(windows), windows)
	}
	got := windows[0]
	if got.Name != "build: api" {
		t.Errorf("Name = %q, want %q", got.Name, "build: api")
	}
	if got.Dead {
		t.Error("a live pane was reported dead")
	}
	if !got.Active {
		t.Error("the active flag was lost")
	}
}

// The exit status says whether a finished command succeeded, which is the
// difference between "it is done" and "it broke".
func TestListWindowsReadsTheExitStatus(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		dead   bool
		status int
	}{
		// A live pane reports no status at all.
		{"running", "0:1:0::server", false, -1},
		{"exited cleanly", "0:1:1:0:build", true, 0},
		{"exited with an error", "0:1:1:1:build", true, 1},
		{"a larger status", "0:1:1:137:build", true, 137},
		// A pane killed by a signal is dead with an empty status: that is
		// not a failure to report, so it stays unknown.
		{"killed by a signal", "0:1:1::build", true, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inst := &Instance{ID: "asm_session", Status: StatusRunning}
			windows := listWindowsWith(t, c.line+"\n", inst)
			if len(windows) != 1 {
				t.Fatalf("got %d windows, want 1", len(windows))
			}
			if windows[0].Dead != c.dead {
				t.Errorf("Dead = %v, want %v", windows[0].Dead, c.dead)
			}
			if windows[0].ExitStatus != c.status {
				t.Errorf("ExitStatus = %d, want %d", windows[0].ExitStatus, c.status)
			}
		})
	}
}
