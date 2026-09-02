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
	closes atomic.Int32
}

type failingTerminalStream struct {
	closes atomic.Int32
}

func (s *failingTerminalStream) Read([]byte) (int, error) { return 0, io.EOF }
func (s *failingTerminalStream) Write([]byte) (int, error) {
	return 0, errors.New("injected terminal write failure")
}
func (s *failingTerminalStream) Close() error {
	s.closes.Add(1)
	return nil
}

type blockingCloseTerminalStream struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingCloseTerminalStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (s *blockingCloseTerminalStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *blockingCloseTerminalStream) Close() error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
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
	s.closes.Add(1)
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
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("terminal stream close count = %d, want 1", got)
	}

	// A racing read-side cleanup or reconnect must be harmless.
	tc.closeTransport()
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("idempotent cleanup closed terminal stream %d times", got)
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
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("terminal stream close count = %d, want 1", got)
	}

	// The reader/output pumps may discover the same failure concurrently.
	tc.closeTransport()
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("idempotent cleanup closed terminal stream %d times", got)
	}
	if tc.handleInputWriteError(nil) {
		t.Fatal("nil input write error was treated as a failure")
	}
}

func TestDirectTerminalInputFailureClosesConnection(t *testing.T) {
	tests := []struct {
		name  string
		write func(*TerminalServer) error
	}{
		{
			name: "dictation",
			write: func(ts *TerminalServer) error {
				return ts.WriteToTerminal("session", 2, "hello")
			},
		},
		{
			name: "backspace",
			write: func(ts *TerminalServer) error {
				return ts.SendBackspace("session", 2, 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := &failingTerminalStream{}
			tc := &termConn{ptmx: stream, done: make(chan struct{})}
			ts := &TerminalServer{conns: map[string]*termConn{"session-2": tc}}

			if err := test.write(ts); err == nil {
				t.Fatal("direct terminal write unexpectedly succeeded")
			}
			select {
			case <-tc.done:
			default:
				t.Fatal("direct terminal write failure left the connection alive")
			}
			if got := stream.closes.Load(); got != 1 {
				t.Fatalf("terminal stream close count = %d, want 1", got)
			}
		})
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
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("active terminal stream close count = %d, want 1", got)
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

func TestTerminalServerFailsClosedWhenSecureTokenGenerationFails(t *testing.T) {
	originalRead := terminalRandomRead
	terminalRandomRead = func([]byte) (int, error) {
		return 0, errors.New("entropy unavailable")
	}
	defer func() { terminalRandomRead = originalRead }()

	ts := NewTerminalServer(nil, 0)
	if ts.AuthToken() != "" {
		t.Fatal("terminal server fell back to a predictable authentication token")
	}
	if err := ts.Start(); err == nil {
		t.Fatal("terminal listener started without a cryptographically secure token")
	}
	if ts.listener != nil || ts.server != nil {
		t.Fatal("failed-closed terminal server opened network resources")
	}
}

func TestReconnectDoesNotHoldServerLockWhileOldNativeStreamCloses(t *testing.T) {
	oldStream := &blockingCloseTerminalStream{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	old := &termConn{ptmx: oldStream, done: make(chan struct{})}
	replacement := &termConn{ptmx: &closeCountingTerminalStream{}, done: make(chan struct{})}
	ts := NewTerminalServer(nil, 0)
	ts.mu.Lock()
	ts.conns["same-tab"] = old
	ts.mu.Unlock()

	if !ts.registerConnection("same-tab", replacement) {
		t.Fatal("running terminal server refused replacement connection")
	}
	// No pumps are launched in this unit-level registration test; balance the
	// production registration accounting before any later Wait.
	ts.connWG.Done()
	ts.connWG.Done()
	ts.connWG.Done()

	select {
	case <-oldStream.started:
	case <-time.After(time.Second):
		t.Fatal("replacement did not begin retiring the previous terminal")
	}

	lockAcquired := make(chan struct{})
	go func() {
		ts.mu.Lock()
		ts.mu.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
		// Expected: the native close is still blocked, but server lifecycle work
		// can already acquire its mutex.
	case <-time.After(200 * time.Millisecond):
		close(oldStream.release)
		t.Fatal("blocked old native close retained the terminal server mutex")
	}

	close(oldStream.release)
	// A second idempotent close joins the in-flight sync.Once callback, ensuring
	// the asynchronous retirement goroutine cannot leak past the test.
	old.closeTransport()
}

func TestTerminalServerStopHonoursDeadlineWhenNativeStreamCloseBlocks(t *testing.T) {
	stream := &blockingCloseTerminalStream{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}
	ts := NewTerminalServer(nil, 0)
	ts.mu.Lock()
	ts.conns["blocked-0"] = tc
	ts.mu.Unlock()

	// Always release the injected native close so this regression test leaves
	// no goroutine behind, even when an assertion below fails.
	defer close(stream.release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	err := ts.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want context deadline", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Stop ignored its deadline while TerminalStream.Close blocked: %v", elapsed)
	}
	select {
	case <-stream.started:
	default:
		t.Fatal("Stop did not start closing the native terminal stream")
	}
	select {
	case <-tc.done:
	default:
		t.Fatal("Stop did not signal the connection before the native close finished")
	}
}

func TestCloseConnectionsDrainsStreamsWithoutStoppingServer(t *testing.T) {
	stream := &closeCountingTerminalStream{}
	tc := &termConn{ptmx: stream, done: make(chan struct{})}
	ts := NewTerminalServer(nil, 0)
	ts.mu.Lock()
	ts.conns["old-project-0"] = tc
	ts.connWG.Add(1)
	ts.mu.Unlock()
	go func() {
		defer ts.connWG.Done()
		<-tc.done
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ts.CloseConnections(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stream.closes.Load(); got != 1 {
		t.Fatalf("old-project terminal close count = %d, want 1", got)
	}
	// A project switch reuses the listener/lifecycle. Only final Stop closes the
	// handler gate.
	_, done, allowed := ts.beginHandler()
	if !allowed {
		t.Fatal("connection drain stopped the reusable terminal server")
	}
	done()
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
		// Long enough that a Stop which does not wait will certainly return
		// first, short enough not to slow the suite.
		time.Sleep(150 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ts.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	// Stop must not return until the handler is done. Asserting that with a
	// bare default case tests something slightly different — that the handler
	// goroutine has already been *scheduled* — and Go promises no such thing
	// the instant the channel it waits on closes, which is why this failed on
	// loaded CI runners while passing everywhere else.
	//
	// The distinction that matters is kept by making the handler observably
	// slow: if Stop waits, that time has already passed when it returns, and
	// the channel is closed. If Stop does not wait, it returns while the
	// handler is still sleeping and the check fails — as it must.
	select {
	case <-handlerExited:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Stop returned before the pending attach handler exited")
	}
	if _, _, allowed := ts.beginHandler(); allowed {
		t.Fatal("stopped terminal server accepted a new handler")
	}
}
