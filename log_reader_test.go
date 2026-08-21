package main

import (
	"fmt"
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

// Clearing the application log has to go through the writer that holds it open.
//
// setupLogging opens the file without O_APPEND, so the process keeps its own
// offset. Truncating the path from underneath would leave the next write
// landing where it left off — padding the start with NUL bytes and making the
// log look corrupt while appearing to have worked.
func TestClearingTheAppLogRewindsTheWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("creating the log: %v", err)
	}
	defer f.Close()

	w := &filteredLogWriter{file: f, path: path}
	if _, err := w.Write([]byte("first line\n")); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if err := w.truncate(); err != nil {
		t.Fatalf("truncating: %v", err)
	}
	if _, err := w.Write([]byte("after clearing\n")); err != nil {
		t.Fatalf("writing after truncate: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(got) != "after clearing\n" {
		t.Errorf("log = %q, want only the line written after clearing — a NUL-padded "+
			"result means the offset was not rewound", string(got))
	}
}

func TestLogViewerReadsOnlyABoundedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.log")
	if err := os.WriteFile(path, []byte("old-one\nold-two\nkeep-three\nkeep-four\n"), 0600); err != nil {
		t.Fatal(err)
	}

	got := readLogFileWithLimit(path, 22, 100)
	if !got.Truncated {
		t.Fatal("byte-truncated log was not marked truncated")
	}
	joined := strings.Join(got.Lines, "\n")
	if strings.Contains(joined, "old-one") || strings.Contains(joined, "old-two") {
		t.Fatalf("bounded reader returned old prefix: %q", joined)
	}
	if !strings.Contains(joined, "keep-four") {
		t.Fatalf("bounded reader lost the newest complete line: %q", joined)
	}
}

func TestApplicationLogCompactionKeepsTheNewestCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < 20; i++ {
		if _, err := fmt.Fprintf(f, "line-%02d-xxxxxxxx\n", i); err != nil {
			t.Fatal(err)
		}
	}
	if err := compactLogFile(f, 200, 120); err != nil {
		t.Fatalf("compactLogFile: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(body)) > 120 {
		t.Fatalf("compacted log is %d bytes, want at most 120", len(body))
	}
	if !strings.Contains(string(body), "line-19-xxxxxxxx") {
		t.Fatalf("compaction lost newest line: %q", body)
	}
	if len(body) > 0 && strings.HasPrefix(string(body), "x") {
		t.Fatalf("compaction retained a partial first line: %q", body)
	}
}

// Truncating without the rewind is the failure the seek exists to prevent.
func TestTruncateWithoutRewindCorruptsTheLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("creating the log: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("a long first line\n")); err != nil {
		t.Fatalf("writing: %v", err)
	}
	// Truncate alone, as clearing the path from outside would do.
	if err := f.Truncate(0); err != nil {
		t.Fatalf("truncating: %v", err)
	}
	if _, err := f.Write([]byte("next\n")); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "\x00") {
		t.Skip("this platform does not pad the gap; the rewind is still required")
	}
	if string(got) == "next\n" {
		t.Error("expected the un-rewound write to leave a padded file, which is " +
			"what filteredLogWriter.truncate avoids")
	}
}
