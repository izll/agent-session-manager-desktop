package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// remain-on-exit and automatic-rename are WINDOW options, and every call says so
// with -w.
//
// Not because tmux needs telling — measured on 3.4, it routes a window option
// to the window either way. The reason is the reader: without -w the target
// looks like a session and the scope has to be inferred from tmux's rules,
// which is exactly the kind of thing that goes unnoticed.
//
// What actually broke was scope, not syntax: a window opened after a
// session-wide setting does not inherit it, so a shell exiting in that window
// took the whole window with it instead of leaving a dead pane behind. The tab
// bar reads #{pane_dead} to mark a tab stopped, and had nothing to read. Hence
// every window-creating path setting it for its own window.
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
