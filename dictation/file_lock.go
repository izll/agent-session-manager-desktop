package dictation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// dictationConfigMu complements the OS lock: platform file locks coordinate
// independent processes, while this mutex also serializes AppService instances
// in the same process.
var dictationConfigMu sync.Mutex

func withDictationConfigLock(action func() error) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	dictationConfigMu.Lock()
	defer dictationConfigMu.Unlock()

	lock, err := os.OpenFile(filepath.Join(configDir, ".config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockConfigFile(lock); err != nil {
		return err
	}
	defer unlockConfigFile(lock)
	return action()
}

func atomicWriteConfigFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return cause
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return cleanup(err)
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := replaceConfigFile(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
