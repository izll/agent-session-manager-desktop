package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
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

func TestInputWriteErrorClosesTerminalTransportExactlyOnce(t *testing.T) {
	stream := &closeCountingTerminalStream{}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}

	if !tc.handleInputWriteError(errors.New("injected terminal write failure")) {
		t.Fatal("input write failure was not handled")
	}
	select {
	case <-tc.done:
	default:
		t.Fatal("input write failure did not signal the connection pumps to stop")
	}
	if stream.closes != 1 {
		t.Fatalf("terminal stream close count = %d, want 1", stream.closes)
	}

	// The reader/output pumps may discover the same failure concurrently.
	tc.closeTransport()
	if stream.closes != 1 {
		t.Fatalf("idempotent cleanup closed terminal stream %d times", stream.closes)
	}
	if tc.handleInputWriteError(nil) {
		t.Fatal("nil input write error was treated as a failure")
	}
}

func TestTerminalServerStopClosesListenerAndActiveStreams(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	ts := NewTerminalServer(nil, port)
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: time.Second}).Get("http://127.0.0.1:" + fmt.Sprint(ts.GetPort()) + "/terminal")
	if err != nil {
		t.Fatalf("started terminal server did not answer: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("terminal server status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	stream := &closeCountingTerminalStream{}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}
	ts.mu.Lock()
	ts.conns["test-0"] = tc
	ts.connWG.Add(1)
	ts.mu.Unlock()
	go func() {
		defer ts.connWG.Done()
		<-tc.done
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ts.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if stream.closes != 1 {
		t.Fatalf("active terminal stream close count = %d, want 1", stream.closes)
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ts.GetPort()), 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("terminal listener still accepted connections after Stop")
	}

	// Stop is idempotent, and the old default-mux implementation would panic
	// when another TerminalServer registered /terminal in this same process.
	if err := ts.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	second := NewTerminalServer(nil, port)
	if err := second.Start(); err != nil {
		t.Fatalf("a stopped server did not release its port: %v", err)
	}
	if err := second.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalServerStopCancelsAndWaitsForPendingAttachHandler(t *testing.T) {
	ts := NewTerminalServer(nil, 0)
	handlerCtx, done, allowed := ts.beginHandler()
	if !allowed {
		t.Fatal("new terminal server rejected handler")
	}
	handlerExited := make(chan struct{})
	go func() {
		defer close(handlerExited)
		defer done()
		<-handlerCtx.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ts.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerExited:
	default:
		t.Fatal("Stop returned before the pending attach handler exited")
	}
	if _, _, allowed := ts.beginHandler(); allowed {
		t.Fatal("stopped terminal server accepted a new handler")
	}
}
