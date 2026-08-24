package main

import (
	"strings"
	"testing"
)

// The attach PTY must carry a UTF-8 locale.
//
// tmux decides whether to run in UTF-8 mode from LC_ALL/LC_CTYPE/LANG. A GUI
// launch inherits none of them — measured on macOS, the running app had no
// locale variable at all and `launchctl getenv LANG` was empty — so tmux falls
// back to a non-UTF-8 mode and mangles multi-byte characters on their way to
// the client.
//
// What makes this worth pinning with a test is how convincingly it imitates
// other faults. The pane's own contents stay correct (capture-pane showed
// "Zoltán" intact), so accented letters, box drawing and emoji come out as
// replacement blocks regardless of renderer or font. It was chased through the
// canvas renderer, Unicode width tables, glyph caches and font stacks before
// the environment turned out to be the cause.
func TestAttachSetsUTF8Locale(t *testing.T) {
	src := readTextFile(t, "terminal_ws.go")

	at := strings.Index(src, `"attach-session"`)
	if at < 0 {
		t.Fatal("attach-session call not found; if it moved, update this test")
	}
	// The environment is assembled just after the command is built.
	window := src[at:]
	if end := strings.Index(window, "StartTerminal"); end > 0 {
		window = window[:end]
	}

	for _, want := range []string{"LANG=", "LC_ALL="} {
		if !strings.Contains(window, want) {
			t.Errorf("the attach environment does not set %s — tmux will not run in "+
				"UTF-8 mode when the app is launched from a menu, and every multi-byte "+
				"character reaches the terminal mangled", want)
		}
	}
	if !strings.Contains(window, "UTF-8") {
		t.Error("the attach environment sets a locale that is not UTF-8")
	}
	// TERM matters for the same reason and shares the failure mode.
	if !strings.Contains(window, "TERM=") {
		t.Error("the attach environment no longer pins TERM")
	}
}
