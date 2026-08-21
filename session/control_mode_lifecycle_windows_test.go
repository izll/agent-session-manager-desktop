//go:build windows

package session

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"
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

func TestControlModeSendKeysFailureClosesStream(t *testing.T) {
	restoreTmuxBinary(t)
	SetTmuxBinary(filepath.Join(t.TempDir(), "missing-psmux.exe"))

	ctx, cancel := context.WithCancel(context.Background())
	stream := &controlModeStream{
		ctx:         ctx,
		cancel:      cancel,
		in:          discardWriteCloser{},
		closed:      make(chan struct{}),
		keys:        make(chan []byte, 1),
		recheckSize: make(chan struct{}, 1),
		pane:        "session:0.0",
	}
	stream.startBackground(stream.deliverKeys)
	if n, err := stream.Write([]byte("input")); err != nil || n != len("input") {
		t.Fatalf("initial asynchronous Write = (%d, %v)", n, err)
	}

	select {
	case <-stream.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("failed psmux send-keys left the control-mode stream alive")
	}
	if n, err := stream.Write([]byte("late")); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after delivery failure = (%d, %v), want closed pipe", n, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}
