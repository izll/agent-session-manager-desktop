//go:build linux

package session

import "context"

func detectAntigravityConversationIDFromLiveProcessTree(ctx context.Context, presenceRoot string, rootPID int, expectedCWD string) string {
	return detectAntigravityConversationIDFromProcessTreeContext(ctx, "/proc", presenceRoot, rootPID, expectedCWD)
}
