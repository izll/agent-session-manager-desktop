//go:build windows

package session

import "os"

// platformDefaultShell is what to run when nothing is configured.
//
// Windows has no $SHELL, so the Unix lookup fell through to "bash" — a command
// that is normally not on a Windows machine at all. A terminal tab restarted
// that way died immediately, leaving an empty pane with no explanation, while
// creating the same tab worked.
//
// COMSPEC is how Windows names its command interpreter and is set on every
// installation; cmd.exe is the fallback if something has unset it. PowerShell
// is not assumed here — it is offered as a choice in settings instead, since
// the system default is what a newly created tab gets.
func platformDefaultShell() string {
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell
	}
	return "cmd.exe"
}
