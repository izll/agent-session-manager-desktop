//go:build !linux && !darwin

package session

import "context"

// codexDaemonHoldsLocally is not answered here. On Windows, finding which
// process holds a file needs the Restart Manager or a walk of every handle —
// more than this is worth — so a conversation the daemon holds shows Codex's
// own "open in another app" screen, as before.
func codexDaemonHoldsLocally(_ context.Context, _ string) bool {
	return false
}
