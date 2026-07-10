package mcp

import (
	"strings"
	"testing"
	"time"
)

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
