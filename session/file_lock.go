package session

import (
	"os"
	"path/filepath"
)

func withCrossProcessFileLock(path string, action func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return withOpenedFileLock(file, action)
}

// withCrossProcessRootFileLock opens the lock relative to an already-open
// directory capability. Unlike joining an absolute pathname, this cannot be
// redirected outside the project by replacing a parent with a symlink.
func withCrossProcessRootFileLock(root *os.Root, name string, action func() error) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return withOpenedFileLock(file, action)
}

func withOpenedFileLock(file *os.File, action func() error) error {
	if err := lockFile(file); err != nil {
		return err
	}
	defer unlockFile(file)
	return action()
}
