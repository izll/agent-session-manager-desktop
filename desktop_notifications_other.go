//go:build !windows

package main

import (
	"context"
	"fmt"
	"runtime"

	"asmgr-desktop/session"
)

func platformInitializeDesktopNotifications(context.Context) error { return nil }
func platformCleanupDesktopNotifications(context.Context)          {}

func platformDeliverDesktopNotification(ctx context.Context, title, body string) error {
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		return session.CommandContext(ctx, "osascript", "-e", script).Run()
	}
	return session.CommandContext(ctx, "notify-send", "-a", "ASMGR Desktop", "-u", "normal", title, body).Run()
}
