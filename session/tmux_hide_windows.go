//go:build windows

package session

import (
	"os/exec"
	"syscall"
)

// Windows opens a console window for every child process started from a GUI
// application. This app runs external commands constantly — the multiplexer for
// every status poll, git for the branch indicator and the diff — so without
// this the screen fills with console windows flashing open and shut, which is
// what "endless windows popping up" turns out to be.
//
// Exported because it is needed wherever a command is built, not only for the
// multiplexer: git alone accounts for a dozen call sites.
//
// CREATE_NO_WINDOW suppresses it. HideWindow alone is not enough: it hides the
// window a process creates, but the console is allocated before the process
// gets a say.
func HideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
