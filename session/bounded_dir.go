package session

import (
	"fmt"
	"io"
	"os"
)

// History/config directories are user-owned input reached from interactive
// Wails calls. Read them in pages and fail closed before a corrupt directory
// with millions of entries can become one equally large allocation.
const maxDiscoveryDirectoryEntries = 4096

func readDirAtMost(path string, limit int) ([]os.DirEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("invalid directory entry limit")
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries := make([]os.DirEntry, 0, min(limit, 128))
	for {
		pageSize := min(128, limit+1-len(entries))
		page, readErr := dir.ReadDir(pageSize)
		entries = append(entries, page...)
		if len(entries) > limit {
			return nil, fmt.Errorf("directory contains more than %d entries", limit)
		}
		if readErr == io.EOF {
			return entries, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}
