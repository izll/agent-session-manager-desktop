//go:build windows

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Wails' notifications live in the window; without one (--serve) its calls end the
// process, so there are none.

func platformInitializeDesktopNotifications(ctx context.Context) error {
	if windowless(ctx) {
		return nil
	}
	return runtime.InitializeNotifications(ctx) //nolint:windowless-checked
}

func platformCleanupDesktopNotifications(ctx context.Context) {
	if windowless(ctx) {
		return
	}
	runtime.CleanupNotifications(ctx) //nolint:windowless-checked
}

func platformDeliverDesktopNotification(ctx context.Context, title, body string) error {
	if windowless(ctx) {
		return nil
	}
	return runtime.SendNotification(ctx, runtime.NotificationOptions{ //nolint:windowless-checked
		Title: title,
		Body:  body,
	})
}
