//go:build linux

package session

import "context"

func detectCursorChatIDFromLiveProcessTree(ctx context.Context, chatsRoot string, rootPID int, expectedCWD string) string {
	return detectCursorChatIDFromProcessTreeContext(ctx, "/proc", chatsRoot, rootPID, expectedCWD)
}
