//go:build darwin

package session

import "context"

// codexDaemonHoldsLocally asks ps for the daemon and lsof for what it has open:
// macOS has no /proc.
func codexDaemonHoldsLocally(ctx context.Context, conversationID string) bool {
	out, err := CommandContext(ctx, "/bin/ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return false
	}
	for _, pid := range codexDaemonPIDsFromPS(string(out)) {
		files, err := CommandContext(ctx, "/usr/sbin/lsof", "-a", "-Fn", "-p", pid).Output()
		if err == nil && codexDaemonHoldsInLsof(string(files), conversationID) {
			return true
		}
	}
	return false
}
