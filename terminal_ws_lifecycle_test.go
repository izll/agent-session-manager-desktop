package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type overlappingTerminalStream struct {
	active  atomic.Int32
	overlap atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *overlappingTerminalStream) Read([]byte) (int, error) { return 0, io.EOF }
func (s *overlappingTerminalStream) Close() error             { return nil }
func (s *overlappingTerminalStream) Write(p []byte) (int, error) {
	if s.active.Add(1) != 1 {
		s.overlap.Store(true)
	}
	defer s.active.Add(-1)
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return len(p), nil
}

type closeCountingTerminalStream struct {
	closes int
}

func TestMirrorCleanupUsesHandlerLifecycleContext(t *testing.T) {
	source, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatal(err)
	}

	legacy := `terminalTmuxRun(context.Background(), "kill-session", "-t", linkedName)`
	if strings.Contains(string(source), legacy) {
		t.Fatal("mirror cleanup can outlive TerminalServer.Stop through context.Background")
	}

	current := `terminalTmuxRun(handlerCtx, "kill-session", "-t", linkedName)`
	if got := strings.Count(string(source), current); got < 4 {
		t.Fatalf("handler-scoped mirror cleanup count = %d, want at least 4", got)
	}
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

func TestTerminalInputIsSerializedIndependentlyOfWebsocketOutput(t *testing.T) {
	stream := &overlappingTerminalStream{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}

	// Holding the websocket writer must not hold terminal input hostage.
	tc.writeMu.Lock()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = tc.writeInput([]byte("first"))
	}()
	select {
	case <-stream.entered:
	case <-time.After(time.Second):
		t.Fatal("terminal input was blocked by websocket output backpressure")
	}
	tc.writeMu.Unlock()

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = tc.writeInput([]byte("second"))
	}()
	// Give an unserialized second Write ample opportunity to overlap the first.
	time.Sleep(50 * time.Millisecond)
	if stream.overlap.Load() {
		t.Fatal("concurrent terminal input writes overlapped")
	}
	close(stream.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first terminal input did not finish")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("serialized terminal input did not finish")
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
	ts.mu.RLock()
	server := ts.server
	ts.mu.RUnlock()
	if server == nil || server.ReadHeaderTimeout != terminalHTTPReadHeaderTimeout ||
		server.IdleTimeout != terminalHTTPIdleTimeout ||
		server.MaxHeaderBytes != terminalHTTPMaxHeaderBytes {
		t.Fatalf("terminal HTTP bounds = %#v, want read=%v idle=%v headers=%d",
			server, terminalHTTPReadHeaderTimeout, terminalHTTPIdleTimeout, terminalHTTPMaxHeaderBytes)
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
