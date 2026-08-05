package session

import (
	"os"
	"strings"
	"testing"
)

// Sending a task to an agent must not press Escape first.
//
// It was sent to dismiss an autocomplete popup before submitting, described as
// closing suggestions "without affecting the pasted text". That is a guess
// about what a keystroke means, and the receiving program decides: Claude Code
// reads Escape as "clear the composer", so the text just pasted was discarded
// and the Enter that followed submitted nothing.
//
// The same mistake as the redraw keystroke that put a stray "/clear" into that
// composer — see terminal_ws.go. A suggestion popup left open is harmless;
// Enter submits the line either way.
//
// QuickReplyTab in app.go still sends Escape, and should: there the user picked
// "Esc" from the attention inbox, so it is the requested action rather than an
// assumption.
func TestSendingATaskPressesNoEscape(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatalf("reading instance.go: %v", err)
	}

	start := strings.Index(string(source), "func (i *Instance) SendTaskToAgent")
	if start < 0 {
		// Named differently? Fall back to scanning the whole file rather than
		// silently passing.
		start = 0
	}
	body := string(source)[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 && start > 0 {
		body = body[:end]
	}

	for i, line := range strings.Split(body, "\n") {
		code := line
		if at := strings.Index(code, "//"); at >= 0 {
			code = code[:at]
		}
		if strings.Contains(code, `"Escape"`) {
			t.Errorf("line %d sends Escape before submitting. The receiving "+
				"program decides what it means, and Claude Code clears its "+
				"composer — discarding the text about to be submitted:\n  %s",
				i+1, strings.TrimSpace(line))
		}
	}
}
