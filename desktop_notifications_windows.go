//go:build windows

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func platformInitializeDesktopNotifications(ctx context.Context) error {
	return runtime.InitializeNotifications(ctx)
}

func platformCleanupDesktopNotifications(ctx context.Context) {
	runtime.CleanupNotifications(ctx)
}

func platformDeliverDesktopNotification(ctx context.Context, title, body string) error {
	return runtime.SendNotification(ctx, runtime.NotificationOptions{
		Title: title,
		Body:  body,
	})
}
