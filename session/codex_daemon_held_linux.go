//go:build linux

package session

import "context"

func codexDaemonHoldsLocally(ctx context.Context, conversationID string) bool {
	return codexDaemonHoldsInProc(ctx, "/proc", conversationID)
}
