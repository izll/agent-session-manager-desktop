package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// remain-on-exit and automatic-rename are WINDOW options, and tmux needs -w to
// know that.
//
// Without it the option is set on the session instead, where neither exists.
// tmux reports no error, so it looks like it worked — and the consequence only
// shows up later: pressing Ctrl+D in a terminal tab closed the shell, and with
// remain-on-exit never actually applied the whole window disappeared instead of
// staying behind as a dead pane. The tab bar reads #{pane_dead} to mark a tab
// stopped, so it had nothing left to read.
//
// A session-wide setting does not help either: windows created afterwards do
// not inherit it, which is why every window-creating path sets it for itself.
func TestWindowOptionsAreSetWithW(t *testing.T) {
	src := readSource(t, "instance.go")

	// Every set-option call naming one of these must carry -w.
	windowOptions := []string{"remain-on-exit", "automatic-rename"}
	callPattern := regexp.MustCompile(`TmuxCommand\("set-option"([^)]*)\)`)

	for _, call := range callPattern.FindAllStringSubmatch(src, -1) {
		args := call[1]
		for _, opt := range windowOptions {
			if !strings.Contains(args, `"`+opt+`"`) {
				continue
			}
			if !strings.Contains(args, `"-w"`) {
				t.Errorf("set-option for %s is missing -w; tmux would set a session option that nothing reads:\n  %s",
					opt, strings.TrimSpace(call[0]))
			}
		}
	}
}

// The tab bar decides a tab is stopped by reading the pane's dead flag, which
// only ever becomes 1 because remain-on-exit kept the window around.
func TestWindowListReadsPaneDead(t *testing.T) {
	src := readSource(t, "instance.go")
	if !strings.Contains(src, "#{pane_dead}") {
		t.Error("GetWindowList must ask tmux for pane_dead, or a closed shell cannot be told from a live one")
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
