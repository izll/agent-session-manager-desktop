//go:build linux || darwin

package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

// syncUpdateDirectory makes directory-entry changes durable. Syncing a file
// alone does not persist the name created by rename(2).
func syncUpdateDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = syncUpdateHandle(dir)
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}

// syncUpdateDirectoryTree persists an already-synced staged tree from the
// leaves upward, so child names are durable before their parents are published.
func syncUpdateDirectoryTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := syncUpdateDirectory(directories[i]); err != nil {
			return fmt.Errorf("cannot sync update directory %q: %w", directories[i], err)
		}
	}
	return nil
}
