//go:build !windows

package mcp

import (
	"os"
	"syscall"
)

func lockTaskMasterFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockTaskMasterFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
