package main

import (
	"strings"
	"testing"
)

func TestAttachSizeFrom(t *testing.T) {
	cases := []struct {
		cols, rows   string
		wantC, wantR int
	}{
		{"222", "60", 222, 60},
		{"", "", 0, 0},
		{"abc", "60", 0, 0},
		{"5", "60", 0, 0},
		{"222", "1", 0, 0},
		{"5000", "60", 0, 0},
	}
	for _, c := range cases {
		gotC, gotR := attachSizeFrom(c.cols, c.rows)
		if gotC != c.wantC || gotR != c.wantR {
			t.Errorf("attachSizeFrom(%q, %q) = %d, %d; want %d, %d", c.cols, c.rows, gotC, gotR, c.wantC, c.wantR)
		}
	}
}

// Attached without a size, the pane shrank to 80x24 until the real size came:
// an agent redrew at 80 columns, and the scrollback kept it. Both the local
// attach and the server attach start at the viewer's size now.
func TestTheAttachStartsAtTheViewersSize(t *testing.T) {
	src := readTextFile(t, "terminal_ws.go")
	for _, want := range []string{
		`attachSizeFrom(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))`,
		`session.StartTerminalWithSize(cmd, attachCols, attachRows)`,
		`ts.attachRemote(inst, winIdx, windowTarget, attachCols, attachRows)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("terminal_ws.go no longer has %s", want)
		}
	}
}
