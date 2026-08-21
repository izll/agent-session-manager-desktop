package main

import (
	"os"
	"path/filepath"
)

func withLogFileLock(path string, action func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	_ = lock.Chmod(0600)
	if err := lockApplicationLog(lock); err != nil {
		return err
	}
	defer unlockApplicationLog(lock)
	return action()
}
