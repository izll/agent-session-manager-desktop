package session

import (
	"strings"
	"testing"
)

// The badge and the button read Claude's mode from its footer. The default
// mode names no mode, so it was never taken as a reading: a Shift+Tab from
// YOLO to the default kept showing YOLO.
func TestClaudeYoloFromPane(t *testing.T) {
	cases := []struct {
		name           string
		footer         string
		on, definitive bool
	}{
		{"bypass", "  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← 2 agents", true, true},
		{"bypass while busy", "✻ Working… (esc to interrupt)\n  ⏵⏵ bypass permissions on · esc to interrupt", true, true},
		{"auto", "  ⏵⏵ auto mode on (shift+tab to cycle) · ← 2 agents", true, true},
		{"accept edits", "  ⏵⏵ accept edits on (shift+tab to cycle)", false, true},
		{"plan", "  ⏸ plan mode on (shift+tab to cycle)", false, true},
		{"default, idle", "────────\n❯ \n────────\n  ? for shortcuts", false, true},
		{"default, busy", "✻ Thinking…\n────────\n❯ \n────────\n  esc to interrupt", false, true},
		{"footer covered by a dialog", "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel", false, false},
	}
	for _, c := range cases {
		on, definitive := claudeYoloFromPane(strings.ToLower(c.footer))
		if on != c.on || definitive != c.definitive {
			t.Errorf("%s: got on=%v definitive=%v, want on=%v definitive=%v", c.name, on, definitive, c.on, c.definitive)
		}
	}
}
