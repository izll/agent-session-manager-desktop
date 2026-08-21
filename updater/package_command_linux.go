//go:build linux

package updater

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Package installers can spawn maintainer scripts. Put the complete tree in a
// fresh process group so a timeout also closes descendants that inherited the
// output pipes; killing only pkexec could otherwise leave Cmd.Wait blocked.
func configurePackageCommand(cmd *exec.Cmd) {
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
