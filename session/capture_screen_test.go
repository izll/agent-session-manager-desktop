package session

import (
	"bytes"
	"strings"
	"testing"
)

// The snapshot must be exactly as tall as the pane. One row too many scrolls
// the screen down; one too few shifts it up. Both were seen in practice, in
// that order, from trimming too little and then too much.
func TestCaptureScreenRowCount(t *testing.T) {
	// capture-pane emits one newline-terminated line per row, so a 56-row pane
	// arrives with 56 newlines — one more than fits.
	const rows = 56
	var raw bytes.Buffer
	for i := 0; i < rows; i++ {
		if i == rows-1 {
			raw.WriteString("\n") // the pane's last row is genuinely blank
		} else {
			raw.WriteString("row\n")
		}
	}

	screen := trimCaptureTrailer(raw.Bytes())

	// rows-1 newlines separate rows lines: the picture is as tall as the pane.
	if got := bytes.Count(screen, []byte("\n")) + 1; got != rows {
		t.Fatalf("snapshot is %d rows, want %d", got, rows)
	}
	// The blank final row has to survive, or everything shifts up a line.
	if !strings.HasSuffix(string(screen), "row\n") {
		t.Fatalf("the pane's blank last row was trimmed away: %q", tail(string(screen), 12))
	}
}

// A capture with no trailing newline at all must be left alone.
func TestCaptureScreenWithoutTrailingNewline(t *testing.T) {
	in := []byte("a\nb\nc")
	if got := trimCaptureTrailer(in); !bytes.Equal(got, in) {
		t.Fatalf("got %q, want it unchanged", got)
	}
}

// CRLF endings must not leave a stray CR behind.
func TestCaptureScreenTrimsCRLF(t *testing.T) {
	if got := trimCaptureTrailer([]byte("a\r\nb\r\n")); string(got) != "a\r\nb" {
		t.Fatalf("got %q, want %q", got, "a\r\nb")
	}
}

// Only ONE newline goes: a pane ending in several blank rows keeps them.
func TestCaptureScreenKeepsInteriorBlanks(t *testing.T) {
	got := trimCaptureTrailer([]byte("x\n\n\n\n"))
	if string(got) != "x\n\n\n" {
		t.Fatalf("got %q, want %q — blank rows are part of the screen", got, "x\n\n\n")
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
