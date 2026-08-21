//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// The probe is a complete Wails/WebKit process and may launch renderer and GPU
// helpers. A wedged driver must not leave those descendants behind after the
// ten-second startup bound kills the direct probe process.
func configureGPUProbeCommand(cmd *exec.Cmd) {
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
