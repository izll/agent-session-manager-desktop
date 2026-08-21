//go:build windows

package session

import (
	"context"
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

func TestControlModeCloseCancelsAndWaitsForBackgroundWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &controlModeStream{
		ctx:         ctx,
		cancel:      cancel,
		in:          discardWriteCloser{},
		closed:      make(chan struct{}),
		keys:        make(chan []byte, 1),
		recheckSize: make(chan struct{}, 1),
	}
	finished := make(chan struct{})
	stream.startBackground(func() {
		defer close(finished)
		// An empty pane never becomes ready and used to keep its six-second
		// startup loop alive after the WebSocket/stream had already closed.
		primePaneSize(stream, "")
	})

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the background pane primer stopped")
	}
}
