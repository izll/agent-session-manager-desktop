package main

import (
	"errors"
	"io"
	"testing"
)

type closeCountingTerminalStream struct {
	closes int
}

func (s *closeCountingTerminalStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (s *closeCountingTerminalStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *closeCountingTerminalStream) Close() error {
	s.closes++
	return nil
}

func TestOutputWriteErrorClosesTerminalTransportExactlyOnce(t *testing.T) {
	stream := &closeCountingTerminalStream{}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}

	if !tc.handleOutputWriteError(errors.New("injected websocket write failure")) {
		t.Fatal("write failure was not handled")
	}
	select {
	case <-tc.done:
	default:
		t.Fatal("write failure did not signal the connection pumps to stop")
	}
	if stream.closes != 1 {
		t.Fatalf("terminal stream close count = %d, want 1", stream.closes)
	}

	// A racing read-side cleanup or reconnect must be harmless.
	tc.closeTransport()
	if stream.closes != 1 {
		t.Fatalf("idempotent cleanup closed terminal stream %d times", stream.closes)
	}
	if tc.handleOutputWriteError(nil) {
		t.Fatal("nil write error was treated as a failure")
	}
}
