package session

import (
	"os"
	"strings"
	"testing"
)

// Nothing on the Windows path sends a redraw keystroke.
//
// Ctrl-L went out in two places: after the pane-size watcher corrected a drift,
// and once at attach to produce the opening frame. Both are covered without
// input. Changing the size is what makes psmux repaint — and it repaints in
// full, 27510 bytes against 0 for a refresh-client at the same size (measured;
// see SetTerminalSize). The frontend always sends a size after attaching,
// because a pipe carries no window dimensions.
//
// The keystroke was never free. It is input, and only means "redraw" to a
// program that chooses to read it that way. Attaching to an existing tab is
// where that hurts most: whatever was left on screen — an open /resume list, a
// half-typed prompt — takes it as a keystroke. On the Unix path the same
// keystroke cleared Codex's /resume list and put a stray "/clear" into Claude
// Code's composer.
//
// The Refresh button (App.RefreshWindow) still sends one. There the user asked
// for it, and it is the only manual recovery there is.
func TestWindowsPathSendsNoRedrawKeystroke(t *testing.T) {
	b, err := os.ReadFile("control_mode_windows.go")
	if err != nil {
		t.Fatalf("reading control_mode_windows.go: %v", err)
	}

	for i, line := range strings.Split(string(b), "\n") {
		code := line
		if at := strings.Index(code, "//"); at >= 0 {
			code = code[:at]
		}
		if strings.Contains(code, `"C-l"`) || strings.Contains(code, "redrawPane(") {
			t.Errorf("control_mode_windows.go:%d sends a redraw keystroke. It "+
				"reaches the pane as input the user did not type, and a size "+
				"change already produces a full repaint:\n  %s",
				i+1, strings.TrimSpace(line))
		}
	}
}

// With no keystroke to fall back on, the size is the only lever left: it is what
// makes psmux repaint, and what keeps a pane drawing at its window's dimensions.
func TestPaneSizeIsStillCorrected(t *testing.T) {
	b, err := os.ReadFile("control_mode_windows.go")
	if err != nil {
		t.Fatalf("reading control_mode_windows.go: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, `"resize-pane"`) {
		t.Error("resize-pane is gone; a pane left at the wrong size draws the " +
			"agent's interface in the wrong place, with nothing to correct it")
	}
	if !strings.Contains(src, "refresh-client -C %d,%d") {
		t.Error("the refresh-client size nudge is gone; it is what makes psmux " +
			"repaint at all, and now the only thing that paints the opening frame")
	}
}
