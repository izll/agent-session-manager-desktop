//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// killWholeGroup makes a timeout reach what the shell started.
//
// Without it the deadline only kills /bin/sh, and whatever it launched carries
// on running on the server — unreachable, and holding whatever it holds.
// Measured: a `sleep 30` behind a 200 ms timeout ran the full thirty seconds,
// and the helper waited for it.
//
// The command is given a process group of its own so the signal can be sent to
// the group rather than to one process.
func killWholeGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		// A negative pid addresses the group, not its leader.
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
