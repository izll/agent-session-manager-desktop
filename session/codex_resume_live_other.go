//go:build !linux && !windows && !darwin

package session

import "context"

func detectCodexSessionIDFromLiveProcessTree(_ context.Context, _ string, _ int, _ string) string {
	return ""
}
