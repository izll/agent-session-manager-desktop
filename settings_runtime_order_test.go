package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestSaveSettingsAppliesRuntimeOnlyAfterDurableCommit(t *testing.T) {
	storage := guardedTestStorage(t)
	app := &App{storage: storage, projectLocked: true}
	settings := SettingsInfo{
		TerminalCopyMode:  "select",
		TerminalShell:     "new-shell",
		TaskMasterEnabled: true,
	}

	oldMouse, oldShell := applyRuntimeMouseCopy, applyRuntimeTerminalShell
	taskMasterMu.Lock()
	oldTaskMasterBlocked := taskMasterStartsBlocked
	taskMasterMu.Unlock()
	var mouseCalls, shellCalls atomic.Int32
	checkPersisted := func() {
		_, _, persisted, err := storage.LoadAllWithSettings()
		if err != nil {
			t.Errorf("runtime apply could not reload committed settings: %v", err)
			return
		}
		if persisted.TerminalCopyMode != settings.TerminalCopyMode || persisted.TerminalShell != settings.TerminalShell {
			t.Errorf("runtime apply ran before commit: copy=%q shell=%q", persisted.TerminalCopyMode, persisted.TerminalShell)
		}
	}
	applyRuntimeMouseCopy = func(context.Context, bool) {
		mouseCalls.Add(1)
		checkPersisted()
	}
	applyRuntimeTerminalShell = func(string) {
		shellCalls.Add(1)
		checkPersisted()
	}
	t.Cleanup(func() {
		applyRuntimeMouseCopy, applyRuntimeTerminalShell = oldMouse, oldShell
		taskMasterMu.Lock()
		taskMasterStartsBlocked = oldTaskMasterBlocked
		taskMasterMu.Unlock()
	})

	// writeStorageDataLocked stages at sessions.json.tmp. A directory at that
	// exact path lets the read and mutation callback succeed but forces the
	// durable write to fail deterministically.
	tmpPath := filepath.Join(os.Getenv("HOME"), ".config", "agent-session-manager-desktop", "sessions.json.tmp")
	if err := os.Mkdir(tmpPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveSettings(settings, ""); err == nil {
		t.Fatal("settings save unexpectedly succeeded with an unusable staging path")
	}
	if mouseCalls.Load() != 0 || shellCalls.Load() != 0 {
		t.Fatalf("failed persistence changed runtime state: mouse=%d shell=%d", mouseCalls.Load(), shellCalls.Load())
	}
	if err := os.Remove(tmpPath); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- app.SaveSettings(settings, "") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime apply ran while the storage mutex was still held")
	}
	if mouseCalls.Load() != 1 || shellCalls.Load() != 1 {
		t.Fatalf("committed settings were not applied once: mouse=%d shell=%d", mouseCalls.Load(), shellCalls.Load())
	}
}
