package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ErrTaskMasterConflict means another writer changed tasks.json while a local
// read-modify-write operation was being prepared. Failing closed is safer than
// replacing that writer's newer snapshot.
var ErrTaskMasterConflict = errors.New("tasks.json changed during update")

var taskMasterPathLocks sync.Map // canonical path -> *sync.Mutex

func canonicalTaskMasterPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// MutateTaskMasterFile runs one complete tasks.json transaction. The process
// mutex joins callers that constructed different TaskMaster values for the same
// project; the OS lock joins cooperating app processes. A final byte-for-byte
// revision check also detects Task Master's own process or an editor, neither of
// which knows about our lock file.
func MutateTaskMasterFile(path string, mutate func(root map[string]interface{}) error) error {
	path = canonicalTaskMasterPath(path)
	return withTaskMasterWriterLock(path, func() error {
		before, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read tasks.json: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(before))
		decoder.UseNumber()
		var root map[string]interface{}
		if err := decoder.Decode(&root); err != nil {
			return fmt.Errorf("failed to parse tasks.json: %w", err)
		}
		if err := mutate(root); err != nil {
			return err
		}
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize tasks.json: %w", err)
		}

		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to verify tasks.json revision: %w", err)
		}
		if !bytes.Equal(current, before) {
			return ErrTaskMasterConflict
		}
		if err := AtomicReplaceTaskMasterFile(path, out); err != nil {
			return fmt.Errorf("failed to replace tasks.json: %w", err)
		}
		return nil
	})
}

// withTaskMasterWriterLock is shared by direct JSON edits and every MCP tool
// call that can write provider state. Serializing only the direct path still
// allowed two app windows/processes to ask Task Master to overwrite the same
// tasks.json concurrently.
func withTaskMasterWriterLock(path string, action func() error) error {
	path = canonicalTaskMasterPath(path)
	value, _ := taskMasterPathLocks.LoadOrStore(path, &sync.Mutex{})
	processLock := value.(*sync.Mutex)
	processLock.Lock()
	defer processLock.Unlock()

	// The stable lock lives outside Task Master's provider-owned directory. In
	// particular, initialize_project must not create .taskmaster/tasks merely by
	// trying to acquire a lock: providers can interpret that partial tree as an
	// already-initialized project. Keep taking the older adjacent lock once the
	// directory exists so concurrently running app versions remain compatible.
	projectRoot := canonicalTaskMasterPath(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	stableLock := filepath.Join(projectRoot, ".asmgr-taskmaster.lock")
	return withTaskMasterOSLock(stableLock, func() error {
		if info, err := os.Stat(filepath.Dir(path)); err == nil && info.IsDir() {
			return withTaskMasterOSLock(path+".asmgr.lock", action)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		return action()
	})
}

func withTaskMasterOSLock(path string, action func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := lockTaskMasterFile(file); err != nil {
		return err
	}
	defer unlockTaskMasterFile(file)
	return action()
}
