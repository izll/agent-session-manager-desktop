//go:build windows

package dictation

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockConfigFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

func unlockConfigFile(file *os.File) {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}

func replaceConfigFile(oldPath, newPath string) error {
	return windows.Rename(oldPath, newPath)
}
