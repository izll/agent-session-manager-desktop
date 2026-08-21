package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

var taskMasterPathLocks = struct {
	sync.Mutex
	entries map[string]*taskMasterPathLock
}{entries: make(map[string]*taskMasterPathLock)}

type taskMasterPathLock struct {
	mu   sync.Mutex
	refs int
}

const maxTaskMasterFileBytes = 64 << 20

func readTaskMasterFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTaskMasterFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTaskMasterFileBytes {
		return nil, fmt.Errorf("tasks.json exceeds the %d-byte limit", maxTaskMasterFileBytes)
	}
	return data, nil
}

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
		before, err := readTaskMasterFile(path)
		if err != nil {
			return fmt.Errorf("failed to read tasks.json: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(before))
		decoder.UseNumber()
		var root map[string]interface{}
		if err := decoder.Decode(&root); err != nil {
			return fmt.Errorf("failed to parse tasks.json: %w", err)
		}
		var trailing interface{}
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return fmt.Errorf("failed to parse tasks.json: contains multiple JSON values")
			}
			return fmt.Errorf("failed to parse tasks.json: trailing data: %w", err)
		}
		if err := mutate(root); err != nil {
			return err
		}
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize tasks.json: %w", err)
		}

		current, err := readTaskMasterFile(path)
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
	taskMasterPathLocks.Lock()
	processLock := taskMasterPathLocks.entries[path]
	if processLock == nil {
		processLock = &taskMasterPathLock{}
		taskMasterPathLocks.entries[path] = processLock
	}
	processLock.refs++
	taskMasterPathLocks.Unlock()
	processLock.mu.Lock()
	defer func() {
		processLock.mu.Unlock()
		taskMasterPathLocks.Lock()
		processLock.refs--
		if processLock.refs == 0 && taskMasterPathLocks.entries[path] == processLock {
			delete(taskMasterPathLocks.entries, path)
		}
		taskMasterPathLocks.Unlock()
	}()

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
