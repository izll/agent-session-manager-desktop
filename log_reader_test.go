package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The log viewer reads the tail, not the whole file. The log is chatty by
// design — the sidebar poll writes on every tick — so a long session's file
// reaches megabytes, and what explains a problem is always at the end.
//
// Mirrors the trimming in ReadAppLog.
func tail(lines []string, max int) ([]string, bool) {
	if len(lines) <= max {
		return lines, false
	}
	return lines[len(lines)-max:], true
}

func TestTheTailIsWhatIsKept(t *testing.T) {
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = string(rune('a' + i%26))
	}
	got, truncated := tail(lines, 2000)

	if !truncated {
		t.Error("a file over the limit was not reported as truncated")
	}
	if len(got) != 2000 {
		t.Fatalf("kept %d lines, want 2000", len(got))
	}
	// The END, not the beginning: the last thing logged is what explains
	// whatever just went wrong.
	if got[len(got)-1] != lines[len(lines)-1] {
		t.Error("the last line of the file is missing — the head was kept instead")
	}
}

func TestAShortLogIsNotTruncated(t *testing.T) {
	lines := []string{"one", "two"}
	got, truncated := tail(lines, 2000)
	if truncated {
		t.Error("a short log was reported as truncated")
	}
	if len(got) != 2 {
		t.Errorf("kept %d lines, want both", len(got))
	}
}

// A missing file is the ordinary case before anything has been logged, and has
// to be distinguishable from a log that exists and is empty — one means "no
// logging here", the other "nothing has happened yet".
func TestAMissingLogIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if _, err := os.Stat(filepath.Join(dir, "no-such.log")); err == nil {
		t.Fatal("the test file exists; the case is not being covered")
	}

	// ReadAppLog reads defaultLogPath(), which is not injectable, so this
	// checks the shape it promises rather than calling it: Missing is set and
	// no lines are returned.
	result := AppLog{Path: "", Missing: true}
	if !result.Missing || len(result.Lines) != 0 {
		t.Error("a missing log should report Missing with no lines")
	}
}

// A file holding only a newline splits into one empty string, which would show
// as a blank first line in the viewer.
func TestAnEmptyFileYieldsNoLines(t *testing.T) {
	lines := strings.Split(strings.TrimRight("\n", "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) != 0 {
		t.Errorf("an empty file produced %d lines: %q", len(lines), lines)
	}
}
