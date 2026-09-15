//go:build !linux && !windows && !darwin

package session

import "context"

func detectAntigravityConversationIDFromLiveProcessTree(_ context.Context, _ string, _ int, _ string) string {
	return ""
}
