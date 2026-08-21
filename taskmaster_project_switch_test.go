package main

import (
	"testing"
	"time"

	"asmgr-desktop/mcp"
	"asmgr-desktop/session"
)

func TestSelectProjectDrainsTaskMasterProvidersAndReopensGate(t *testing.T) {
	storage := guardedTestStorage(t)
	project, err := storage.AddProject("next")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetActiveProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveSettings(&session.Settings{TaskMasterEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetActiveProject(""); err != nil {
		t.Fatal(err)
	}
	if err := storage.LockProjectForUse(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.UnlockProject)

	taskMasterMu.Lock()
	oldCache, oldStarts, oldBlocked, oldEpoch := taskMasterCache, taskMasterStarts, taskMasterStartsBlocked, taskMasterDrainEpoch
	taskMasterCache = map[string]*mcp.TaskMaster{"/old/worktree": mcp.NewTaskMaster(t.TempDir())}
	taskMasterStarts = make(map[string]*taskMasterStart)
	taskMasterStartsBlocked = false
	taskMasterDrainEpoch = 0
	taskMasterMu.Unlock()
	taskManagerMu.Lock()
	oldManagers := taskManagerCache
	taskManagerCache = map[string]*session.TaskManager{"/old/worktree": session.NewTaskManager(t.TempDir())}
	taskManagerMu.Unlock()
	fileIndexMu.Lock()
	oldFileIndexes := fileIndexCache
	fileIndexCache = map[fileIndexKey]*fileIndexCacheEntry{
		{sessionID: "old", root: "/old/worktree"}: {index: &session.FileIndex{}, expiresAt: time.Now().Add(time.Hour)},
	}
	fileIndexMu.Unlock()
	gitBranchMu.Lock()
	oldBranches := gitBranchCache
	gitBranchCache = map[string]gitBranchCacheEntry{"/old/worktree": {expiresAt: time.Now().Add(time.Hour)}}
	gitBranchMu.Unlock()
	gitBranchListMu.Lock()
	oldBranchLists := gitBranchListCache
	gitBranchListCache = map[string]gitBranchListCacheEntry{"/old/worktree": {expiresAt: time.Now().Add(time.Hour)}}
	gitBranchListMu.Unlock()
	tabWorkingDirMu.Lock()
	oldWorkingDirs := tabWorkingDirCache
	tabWorkingDirCache = map[string]tabWorkingDirCacheEntry{"old:0": {path: "/old/worktree", expiresAt: time.Now().Add(time.Hour)}}
	tabWorkingDirMu.Unlock()
	t.Cleanup(func() {
		drainTaskMasters(true)
		taskMasterMu.Lock()
		taskMasterCache, taskMasterStarts = oldCache, oldStarts
		taskMasterStartsBlocked, taskMasterDrainEpoch = oldBlocked, oldEpoch
		taskMasterMu.Unlock()
		taskManagerMu.Lock()
		taskManagerCache = oldManagers
		taskManagerMu.Unlock()
		fileIndexMu.Lock()
		fileIndexCache = oldFileIndexes
		fileIndexMu.Unlock()
		gitBranchMu.Lock()
		gitBranchCache = oldBranches
		gitBranchMu.Unlock()
		gitBranchListMu.Lock()
		gitBranchListCache = oldBranchLists
		gitBranchListMu.Unlock()
		tabWorkingDirMu.Lock()
		tabWorkingDirCache = oldWorkingDirs
		tabWorkingDirMu.Unlock()
	})

	app := &App{storage: storage, projectLocked: true}
	if err := app.SelectProject(project.ID); err != nil {
		t.Fatal(err)
	}
	taskMasterMu.RLock()
	defer taskMasterMu.RUnlock()
	if len(taskMasterCache) != 0 || len(taskMasterStarts) != 0 {
		t.Fatalf("project switch retained providers: cache=%d starts=%d", len(taskMasterCache), len(taskMasterStarts))
	}
	if taskMasterStartsBlocked {
		t.Fatal("ordinary project switch left Task Master starts disabled")
	}
	taskManagerMu.RLock()
	managerCount := len(taskManagerCache)
	taskManagerMu.RUnlock()
	fileIndexMu.Lock()
	indexCount := len(fileIndexCache)
	fileIndexMu.Unlock()
	if managerCount != 0 || indexCount != 0 {
		t.Fatalf("project switch retained local caches: task managers=%d file indexes=%d", managerCount, indexCount)
	}
	gitBranchMu.Lock()
	branchCount := len(gitBranchCache)
	gitBranchMu.Unlock()
	gitBranchListMu.Lock()
	branchListCount := len(gitBranchListCache)
	gitBranchListMu.Unlock()
	tabWorkingDirMu.Lock()
	workingDirCount := len(tabWorkingDirCache)
	tabWorkingDirMu.Unlock()
	if branchCount != 0 || branchListCount != 0 || workingDirCount != 0 {
		t.Fatalf("project switch retained root caches: branches=%d branch lists=%d working dirs=%d", branchCount, branchListCount, workingDirCount)
	}
}

func TestProjectDrainDoesNotReopenPersistentTaskMasterGate(t *testing.T) {
	taskMasterMu.Lock()
	oldCache, oldStarts := taskMasterCache, taskMasterStarts
	oldBlocked, oldEpoch := taskMasterStartsBlocked, taskMasterDrainEpoch
	taskMasterCache = make(map[string]*mcp.TaskMaster)
	taskMasterStarts = make(map[string]*taskMasterStart)
	taskMasterStartsBlocked = true
	taskMasterDrainEpoch = 0
	taskMasterMu.Unlock()
	t.Cleanup(func() {
		taskMasterMu.Lock()
		taskMasterCache, taskMasterStarts = oldCache, oldStarts
		taskMasterStartsBlocked, taskMasterDrainEpoch = oldBlocked, oldEpoch
		taskMasterMu.Unlock()
	})

	drainTaskMasters(false)
	taskMasterMu.RLock()
	defer taskMasterMu.RUnlock()
	if !taskMasterStartsBlocked {
		t.Fatal("project drain reopened a gate already closed by shutdown or feature disable")
	}
}
