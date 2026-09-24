//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachFromTerminal starts git in a session of its own. Without a
// controlling terminal, ssh and git cannot open /dev/tty to ask for a
// passphrase behind the null stdin — which they would do when the app was
// launched from a terminal. It also makes git and everything it started one
// process group, so the timeout kills ssh along with git instead of leaving it
// holding the connection open.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
