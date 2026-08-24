package main

import (
	"os"
	"strings"
	"testing"
)

// Nothing on the automatic path may send a keystroke into a pane.
//
// Ctrl-L was sent after a resize, to stop a bottom-aligned TUI keeping a frame
// laid out for the old geometry. But Ctrl-L is INPUT: it means "redraw" only if
// the program reading the pane chooses to read it that way, and the programs
// here often do not. Claude Code turned a run of them into a "/clear" typed
// into its composer. Codex's /resume picker read one as a keystroke and wiped
// its screen, so choosing a conversation from the list cleared it.
//
// Each attempt to make it safe — rate limits, then a per-tab queue coalescing a
// burst into one — removed a symptom and left the cause: a keystroke the user
// did not type, arriving at a moment nothing here can predict. The redraw it
// was covering does not need it. resize-window signals the program, which lays
// itself out again, and a tab returning from the background replays the output
// it missed rather than asking for a repaint.
//
// The Refresh button (app.go) still sends one. There the user has asked for it,
// and it is the only manual recovery there is when a pane looks wrong.
func TestTerminalServerSendsNoKeystrokes(t *testing.T) {
	b, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatalf("reading terminal_ws.go: %v", err)
	}

	for i, line := range strings.Split(string(b), "\n") {
		code := line
		if at := strings.Index(code, "//"); at >= 0 {
			code = code[:at]
		}
		if strings.Contains(code, `"send-keys"`) {
			t.Errorf("terminal_ws.go:%d sends a keystroke on the automatic path. "+
				"It reaches the pane as input the user did not type — this is what "+
				"put a stray \"/clear\" into Claude Code's composer and cleared "+
				"Codex's /resume list:\n  %s", i+1, strings.TrimSpace(line))
		}
	}
}

// A resize must still reach the program, since that is now what stands in for
// the keystroke: resize-window is what tells a TUI its geometry changed, and
// refresh-client is what repaints the clients watching it.
func TestResizeStillSignalsThePane(t *testing.T) {
	src := readGoSource(t, "terminal_ws.go")

	for _, want := range []string{`"resize-window"`, "RefreshSessionClients"} {
		if !strings.Contains(src, want) {
			t.Errorf("%s is gone from terminal_ws.go. With no keystroke to fall "+
				"back on, this is the only thing that makes a resized pane lay "+
				"itself out again", want)
		}
	}
}
