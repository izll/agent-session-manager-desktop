//go:build !windows

package session

import "os"

// platformDefaultShell is what to run when nothing is configured.
//
// $SHELL is what the user's login sets, and bash is the fallback that exists
// on effectively every Unix.
func platformDefaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "bash"
}
