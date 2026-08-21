//go:build !windows

package session

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureClaudeBackgroundCommand gives each short-lived Claude CLI probe
// its own process group. Claude may launch helpers; killing only the direct
// process on timeout returned from Cmd.Wait (thanks to WaitDelay) but left the
// descendants running after the app had abandoned the operation.
func configureClaudeBackgroundCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
