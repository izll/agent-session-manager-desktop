//go:build linux

package session

import "context"

func detectCodexSessionIDFromLiveProcessTree(ctx context.Context, sessionsRoot string, rootPID int, expectedCWD string) string {
	return detectCodexSessionIDFromProcessTreeContext(ctx, "/proc", sessionsRoot, rootPID, expectedCWD)
}
