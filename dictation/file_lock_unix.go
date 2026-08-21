//go:build !windows

package dictation

import (
	"os"
	"syscall"
)

func lockConfigFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockConfigFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func replaceConfigFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
