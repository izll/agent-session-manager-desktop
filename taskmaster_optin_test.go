package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"asmgr-desktop/mcp"
	"asmgr-desktop/session"
)

func TestTaskMasterCacheKeyCanonicalizesProjectAliases(t *testing.T) {
	realPath := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(realPath, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got, want := taskMasterProjectPath(alias), taskMasterProjectPath(realPath); got != want {
		t.Fatalf("Task Master cache keys differ for aliases: %q != %q", got, want)
	}
}

func TestStopTaskMasterRejectsSessionWithoutProjectPath(t *testing.T) {
	storage := guardedTestStorage(t)
	instance := &session.Instance{ID: "pathless", Name: "pathless", Status: session.StatusStopped}
	if err := storage.AddInstance(instance); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: true}
	key := taskMasterProjectPath("")
	dummy := mcp.NewTaskMaster(key)
	taskMasterMu.Lock()
	taskMasterCache[key] = dummy
	taskMasterMu.Unlock()
	t.Cleanup(func() {
		taskMasterMu.Lock()
		delete(taskMasterCache, key)
		taskMasterMu.Unlock()
	})

	if err := app.StopTaskMaster(instance.ID); err == nil || !strings.Contains(err.Error(), "sessionNoPath") {
		t.Fatalf("pathless StopTaskMaster error = %v", err)
	}
	taskMasterMu.RLock()
	kept := taskMasterCache[key] == dummy
	taskMasterMu.RUnlock()
	if !kept {
		t.Fatal("pathless session stopped an unrelated Task Master cache entry")
	}
}

// Proves the opt-in actually prevents the npx install: a fake `npx` is put at
// the front of PATH which records every invocation, then every exported
// TaskMaster* method is called with the setting off and on.
func TestTaskMasterOptInBlocksNpx(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tripwire := filepath.Join(tmp, "npx-was-called")
	script := "#!/bin/sh\necho \"$@\" >> " + tripwire + "\nexit 1\n"
	for _, name := range []string{"npx", "npm"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", tmp)

	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: true}

	inst, err := session.NewInstance("guard-test", tmp, false, session.AgentClaude, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}

	npxCalled := func() bool {
		_, err := os.Stat(tripwire)
		return err == nil
	}

	// --- setting OFF (the default) ---
	if app.taskMasterEnabled() {
		t.Fatal("Task Master must default to disabled")
	}

	callAll := func() []error {
		return []error{
			errOf(app.TaskMasterInit(inst.ID)),
			errOf(app.TaskMasterParsePRD(inst.ID, "prd", 3)),
			errOf(app.TaskMasterSetStatus(inst.ID, "1", "done")),
			errOf(app.TaskMasterUpdateTask(inst.ID, "1", "x", false)),
			errOf(app.TaskMasterUpdateSubtask(inst.ID, "1.1", "x")),
			errOf(app.TaskMasterExpandTask(inst.ID, "1", false, false)),
			errOf(app.TaskMasterExpandAll(inst.ID, false)),
			errOf(app.TaskMasterRemoveTask(inst.ID, "1")),
			errOf(app.TaskMasterSendToAgent(inst.ID, "1")),
			errOf(app.TaskMasterAddSubtask(inst.ID, "1", "t", "d")),
			errOf(app.TaskMasterRemoveSubtask(inst.ID, "1.1")),
			errOf(app.TaskMasterClearSubtasks(inst.ID, "1")),
			errOf(app.TaskMasterSetSubtaskStatus(inst.ID, "1.1", "done")),
			errOf(app.TaskMasterAddDependency(inst.ID, "1", "2")),
			errOf(app.TaskMasterRemoveDependency(inst.ID, "1", "2")),
			errOf(app.TaskMasterUpdateTaskDirect(inst.ID, "1", "t", "d", "x", "high", "", "")),
		}
	}

	errs := callAll()
	_, _ = app.TaskMasterGetTasks(inst.ID, "")
	_, _ = app.TaskMasterGetTask(inst.ID, "1")
	_, _ = app.TaskMasterNextTask(inst.ID)
	_, _ = app.TaskMasterAddTask(inst.ID, "p", false, "high")
	_, _ = app.TaskMasterAddManualTask(inst.ID, "t", "d", "x", "high")
	_, _ = app.TaskMasterAnalyzeComplexity(inst.ID, false)
	status := app.TaskMasterStatus(inst.ID)

	for i, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "taskMasterDisabled") {
			t.Errorf("call %d: expected taskMasterDisabled, got %v", i, err)
		}
	}
	if status["initialized"] != false || status["running"] != false {
		t.Errorf("status should report not initialized while disabled: %v", status)
	}
	if npxCalled() {
		data, _ := os.ReadFile(tripwire)
		t.Fatalf("npx WAS EXECUTED while Task Master was disabled: %s", data)
	}

	// --- setting ON: the guard must not be what blocks it any more ---
	if err := storage.UpdateSettings(func(s *session.Settings) {
		s.TaskMasterEnabled = true
	}); err != nil {
		t.Fatal(err)
	}
	if !app.taskMasterEnabled() {
		t.Fatal("enabling the setting must be observed")
	}
	stopAllTaskMasters()
	blockedStatus := app.TaskMasterStatus(inst.ID)
	if blockedStatus["error"] == nil || !strings.Contains(blockedStatus["error"].(string), "taskMasterDisabled") {
		t.Fatalf("a stop in progress must close the late-start registration gate: %v", blockedStatus)
	}
	if npxCalled() {
		t.Fatal("a caller that observed the old setting must not start npx after stopAllTaskMasters")
	}
	// This mirrors the successful SaveSettings(enabled=true) path. The test
	// writes storage directly so it opens the global gate directly as well.
	taskMasterMu.Lock()
	taskMasterStartsBlocked = false
	taskMasterMu.Unlock()
	t.Cleanup(func() {
		taskMasterMu.Lock()
		taskMasterStartsBlocked = false
		taskMasterMu.Unlock()
	})
	_ = app.TaskMasterStatus(inst.ID)
	if !npxCalled() {
		t.Fatal("with the setting ON npx should have been attempted; the guard is not what gates it")
	}
}

func errOf(e error) error { return e }
