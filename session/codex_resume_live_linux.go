//go:build linux

package session

import "context"

func detectCodexSessionIDFromLiveProcessTree(_ context.Context, sessionsRoot string, rootPID int, expectedCWD string) string {
	return detectCodexSessionIDFromProcessTree("/proc", sessionsRoot, rootPID, expectedCWD)
}
