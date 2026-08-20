package mcp

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingWriteCloser struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	closed  sync.Once
	bytes.Buffer
}

func (w *blockingWriteCloser) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return w.Buffer.Write(p)
}

func (w *blockingWriteCloser) Close() error {
	w.closed.Do(func() { close(w.release) })
	return nil
}

func TestReadResponsesAcceptsLargeMessage(t *testing.T) {
	largeText := strings.Repeat("x", 128*1024)
	message := `{"jsonrpc":"2.0","id":1,"result":{"text":"` + largeText + `"}}` + "\n"
	responseCh := make(chan *JSONRPCResponse, 1)
	c := &Client{
		running:      true,
		runID:        1,
		responseChan: map[int64]chan *JSONRPCResponse{1: responseCh},
	}

	c.readResponses(1, newMCPScanner(strings.NewReader(message)))

	select {
	case response := <-responseCh:
		if response.Error != nil {
			t.Fatalf("large response failed: %v", response.Error)
		}
		if !strings.Contains(string(response.Result), largeText) {
			t.Fatal("large response was truncated")
		}
	case <-time.After(time.Second):
		t.Fatal("large response was not delivered")
	}
}

func TestResponseEOFFailsPendingRequest(t *testing.T) {
	responseCh := make(chan *JSONRPCResponse, 1)
	c := &Client{
		running:      true,
		runID:        7,
		responseChan: map[int64]chan *JSONRPCResponse{42: responseCh},
	}

	c.readResponses(7, newMCPScanner(strings.NewReader("")))

	if c.IsRunning() {
		t.Fatal("client remained running after response EOF")
	}
	select {
	case response := <-responseCh:
		if response.Error == nil {
			t.Fatal("pending request did not receive transport error")
		}
	case <-time.After(time.Second):
		t.Fatal("pending request was left waiting after EOF")
	}
}

func TestStopUnblocksInFlightWriter(t *testing.T) {
	w := &blockingWriteCloser{started: make(chan struct{}), release: make(chan struct{})}
	c := &Client{
		running: true, runID: 1, stdin: w,
		responseChan: make(map[int64]chan *JSONRPCResponse),
	}

	written := make(chan struct{})
	go func() {
		c.sendSuccessResponse(1, 9, map[string]any{"ok": true})
		close(written)
	}()
	select {
	case <-w.started:
	case <-time.After(time.Second):
		t.Fatal("response write did not start")
	}

	stopped := make(chan struct{})
	go func() {
		_ = c.Stop()
		close(stopped)
	}()
	deadline := time.Now().Add(time.Second)
	for c.IsRunning() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if c.IsRunning() {
		t.Fatal("Stop never began its state transition")
	}
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("closing stdin did not release the response writer")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop stayed blocked behind an in-flight pipe write")
	}
}

func TestStaleRunCannotWriteToReplacementProcess(t *testing.T) {
	w := &writeCloser{Writer: &bytes.Buffer{}}
	c := &Client{
		running: true, runID: 2, stdin: w,
		responseChan: make(map[int64]chan *JSONRPCResponse),
	}

	if err := c.writeForRun(1, []byte(`{"old":true}`)); err == nil {
		t.Fatal("a stale process was allowed to write to the current MCP server")
	}
	if got := w.Writer.(*bytes.Buffer).Len(); got != 0 {
		t.Fatalf("stale process wrote %d bytes to the replacement server", got)
	}
}

type writeCloser struct{ io.Writer }

func (w *writeCloser) Close() error { return nil }
