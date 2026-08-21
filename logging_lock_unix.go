//go:build !windows

package main

import (
	"os"
	"syscall"
)

func lockApplicationLog(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockApplicationLog(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
