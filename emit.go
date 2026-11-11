package main

import (
	"context"
	"log"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Events and error reports to the window, the one place that knows whether there is
// one. Without a window, as with --serve, Wails has no runtime in the context, and
// its calls end the whole process.

type headlessKey struct{}

// headlessContext marks a context as having no window behind it.
func headlessContext(parent context.Context) context.Context {
	return context.WithValue(parent, headlessKey{}, true)
}

func windowless(ctx context.Context) bool {
	return ctx == nil || ctx.Value(headlessKey{}) != nil
}

// emitEvent sends an event to the window, if there is one.
func emitEvent(ctx context.Context, name string, data ...interface{}) {
	if windowless(ctx) {
		return
	}
	runtime.EventsEmit(ctx, name, data...)
}

// logStartupError reports a failure the user should see, to the window when there is
// one and to the log always.
func logStartupError(ctx context.Context, message string) {
	log.Print(message)
	if windowless(ctx) {
		return
	}
	runtime.LogError(ctx, message)
}
