package session

import (
	"errors"
	"io"
	"testing"
)

// The joined stream is only constructed on Windows, but nothing in it is
// OS-specific, so it is tested on every platform — a joiner exercised only by
// the OS nobody develops on would rot unnoticed. io.Pipe stands in for the
// child process's handles.

func TestTerminalPipesReadsFromTheReadHalf(t *testing.T) {
	outR, outW := io.Pipe()
	_, inW := io.Pipe()
	s := newTerminalPipes(outR, inW, nil)

	go func() {
		outW.Write([]byte("\x1b[?61c"))
		outW.Close()
	}()

	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "\x1b[?61c" {
		t.Fatalf("read %q, want the bytes written to the read half", got)
	}
}

func TestTerminalPipesWritesToTheWriteHalf(t *testing.T) {
	outR, _ := io.Pipe()
	inR, inW := io.Pipe()
	s := newTerminalPipes(outR, inW, nil)

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 3)
		io.ReadFull(inR, buf)
		done <- string(buf)
	}()

	if _, err := s.Write([]byte("ls\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := <-done; got != "ls\n" {
		t.Fatalf("write half received %q, want %q", got, "ls\n")
	}
}

func TestTerminalPipesCloseReleasesBothHalvesAndTheProcess(t *testing.T) {
	outR, outW := io.Pipe()
	inR, inW := io.Pipe()

	stopped := false
	s := newTerminalPipes(outR, inW, func() { stopped = true })

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !stopped {
		t.Fatal("Close did not stop the process; a closed WebSocket would leave the multiplexer attached")
	}

	// Closing the read half must make the peer's writes fail, otherwise the
	// child would block forever on a stream nobody drains.
	if _, err := outW.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write to the closed read half returned %v, want ErrClosedPipe", err)
	}
	// Closing the write half must reach the child as EOF on its stdin.
	if _, err := inR.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read from the closed write half returned %v, want EOF", err)
	}
}

func TestTerminalPipesCloseReportsAnErrorButStillClosesBoth(t *testing.T) {
	outR, _ := io.Pipe()
	inR, _ := io.Pipe()
	failing := failingCloser{}

	s := newTerminalPipes(outR, failing, nil)
	if err := s.Close(); err == nil {
		t.Fatal("Close swallowed the write half's error")
	}
	// The read half must have been closed anyway — a failure on one handle
	// cannot be allowed to leak the other.
	if _, err := outR.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read half left open after a failing write-half close: %v", err)
	}
	inR.Close()
}

type failingCloser struct{}

func (failingCloser) Write(p []byte) (int, error) { return len(p), nil }
func (failingCloser) Close() error                { return errors.New("handle already gone") }
