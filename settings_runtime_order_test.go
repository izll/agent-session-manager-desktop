package main

import (
	"asmgr-desktop/session"
	"context"
	"os"
	"path/filepath"
	"strings"
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

	// Force the durable write to fail after the read and the mutation callback
	// have succeeded.
	//
	// A read-only config directory does it. Planting a directory at the staging
	// path used to, back when the write staged at a fixed sessions.json.tmp —
	// it now goes through CreateTemp, so there is no name to plant at. The
	// point of the test is the outcome, not how the failure is produced.
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "agent-session-manager-desktop")
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDir, 0o700) })
	if err := app.SaveSettings(settings, ""); err == nil {
		t.Fatal("settings save unexpectedly succeeded with an unusable staging path")
	}
	if mouseCalls.Load() != 0 || shellCalls.Load() != 0 {
		t.Fatalf("failed persistence changed runtime state: mouse=%d shell=%d", mouseCalls.Load(), shellCalls.Load())
	}
	// Let the next save through: the rest of the test checks that a SUCCESSFUL
	// persist does apply the runtime settings.
	if err := os.Chmod(configDir, 0o700); err != nil {
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

func TestReadOnlyProjectDoesNotRewriteSharedTmuxBindings(t *testing.T) {
	storage := guardedTestStorage(t)
	if err := storage.SaveSettings(&session.Settings{TerminalCopyMode: "select"}); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: false}

	oldMouse, oldShell := applyRuntimeMouseCopy, applyRuntimeTerminalShell
	taskMasterMu.Lock()
	oldTaskMasterBlocked := taskMasterStartsBlocked
	taskMasterMu.Unlock()
	var mouseCalls atomic.Int32
	applyRuntimeMouseCopy = func(context.Context, bool) { mouseCalls.Add(1) }
	applyRuntimeTerminalShell = func(string) {}
	t.Cleanup(func() {
		applyRuntimeMouseCopy, applyRuntimeTerminalShell = oldMouse, oldShell
		taskMasterMu.Lock()
		taskMasterStartsBlocked = oldTaskMasterBlocked
		taskMasterMu.Unlock()
	})

	app.applyActiveProjectRuntimeSettings()
	if got := mouseCalls.Load(); got != 0 {
		t.Fatalf("read-only project changed shared tmux bindings %d time(s)", got)
	}
}

// Start-up applies the runtime settings after claiming the project lock.
//
// The mouse-copy half of applyActiveProjectRuntimeSettings is gated on
// a.projectLocked: tmux key tables are server-wide, so only the lock owner may
// rewrite them. Called before the claim, that gate is always shut and the
// setting silently never applies — a binding left by an earlier run stays in
// force, and a fresh install keeps copy-on-select on for someone who never
// asked. The tables outlive the process, so nothing later corrects it.
//
// Checked against the source because the alternative is standing up a Wails
// runtime and a tmux server to observe an ordering; the thing that went wrong
// is the order of two statements, and that is what this reads.
func TestStartupAppliesRuntimeSettingsAfterClaimingTheLock(t *testing.T) {
	source := readTextFile(t, "app.go")

	startup := source[strings.Index(source, "func (a *App) startup("):]
	if end := strings.Index(startup, "\nfunc "); end > 0 {
		startup = startup[:end]
	}

	claim := strings.Index(startup, "a.projectLocked = true")
	apply := strings.Index(startup, "a.applyActiveProjectRuntimeSettings()")

	if claim < 0 {
		t.Fatal("startup no longer claims the project lock")
	}
	if apply < 0 {
		t.Fatal("startup no longer applies the runtime settings")
	}
	if apply < claim {
		t.Error("startup applies runtime settings before claiming the lock, so the " +
			"lock-gated half is skipped; SelectProject has the order right")
	}
}
