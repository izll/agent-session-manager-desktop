package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

// The preview poller reads project storage and captures tmux panes. It must be
// gone before shutdown releases the project lock or removes GUI mirrors; a
// later cancel leaves a real window in which another tick can enter teardown.
func TestShutdownStopsPreviewPollingBeforeProjectTeardown(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (a *App) shutdown(ctx context.Context)")
	if start < 0 {
		t.Fatal("shutdown method not found")
	}
	text = text[start:]
	stop := strings.Index(text, "a.stopPreviewPolling()")
	teardown := strings.Index(text, "a.projectMu.Lock()")
	if stop < 0 || teardown < 0 {
		t.Fatalf("shutdown lifecycle calls missing: stop=%d teardown=%d", stop, teardown)
	}
	if stop > teardown {
		t.Fatal("preview polling is stopped after project teardown begins")
	}
}

func TestStopPreviewPollingIsIdempotentAndWaits(t *testing.T) {
	app := NewApp()
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	app.previewCancel = cancel
	app.previewWG.Add(1)
	go func() {
		defer app.previewWG.Done()
		<-ctx.Done()
		close(stopped)
	}()

	app.stopPreviewPolling()
	select {
	case <-stopped:
	default:
		t.Fatal("stopPreviewPolling returned before the poller stopped")
	}
	app.stopPreviewPolling()
}
