package mcp

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestTaskMasterProcessLocksDoNotRetainCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	const paths = 32
	var wg sync.WaitGroup
	for i := range paths {
		// Numbered, not lettered: 'a'+i runs past 'z' at 26 into '{', '|', '}',
		// which Windows refuses in a filename. The test only needs 32 distinct
		// paths, and what they are called is beside the point.
		path := filepath.Join(root, "project", fmt.Sprintf("p%02d", i), ".taskmaster", "tasks", "tasks.json")
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
