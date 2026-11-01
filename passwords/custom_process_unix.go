//go:build !windows

package passwords

import (
	"os/exec"
	"syscall"
)

// stopWholeProcessGroup makes a timeout end the command with whatever it started, as
// a wrapper script's child would otherwise go on holding the output open.
func stopWholeProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
