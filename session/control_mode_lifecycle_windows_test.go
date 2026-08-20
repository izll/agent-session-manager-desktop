//go:build windows

package session

import (
	"errors"
	"io"
	"testing"
)

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

func TestClosedControlModeStreamRejectsAndDoesNotQueueInput(t *testing.T) {
	stream := &controlModeStream{
		in:          discardWriteCloser{},
		closed:      make(chan struct{}),
		keys:        make(chan []byte, 1),
		recheckSize: make(chan struct{}, 1),
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := stream.Write([]byte("late input")); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after Close = (%d, %v), want (0, io.ErrClosedPipe)", n, err)
	}
	if got := len(stream.keys); got != 0 {
		t.Fatalf("closed stream queued %d input batches", got)
	}
}
