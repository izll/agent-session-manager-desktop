package mcp

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestTaskMasterProcessLocksDoNotRetainCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	const paths = 32
	var wg sync.WaitGroup
	for i := range paths {
		path := filepath.Join(root, "project", string(rune('a'+i)), ".taskmaster", "tasks", "tasks.json")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := withTaskMasterWriterLock(path, func() error { return nil }); err != nil {
				t.Errorf("lock %s: %v", path, err)
			}
		}()
	}
	wg.Wait()
	taskMasterPathLocks.Lock()
	defer taskMasterPathLocks.Unlock()
	if len(taskMasterPathLocks.entries) != 0 {
		t.Fatalf("released Task Master locks retained %d canonical paths", len(taskMasterPathLocks.entries))
	}
}
