package main

import (
	"strings"
	"testing"

	"asmgr-desktop/session"
)

func TestHistoryPreviewUsesIndexedOpaqueIdentity(t *testing.T) {
	index := session.NewHistoryIndex()
	app := &App{historyIndex: index}
	if _, err := app.GetHistoryPreview("/tmp/frontend-supplied-history.json"); err == nil {
		t.Fatal("arbitrary frontend file path was accepted as a history identity")
	}
}

func TestHistoryIndexIsInvalidatedAndRequestsArePinnedAcrossProjectSwitch(t *testing.T) {
	storage := guardedTestStorage(t)
	if err := storage.LockProjectForUse(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.UnlockProject)
	other, err := storage.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage, projectLocked: true, historyIndex: session.NewHistoryIndex()}
	if err := app.SelectProject(other.ID); err != nil {
		t.Fatal(err)
	}
	app.historyMu.Lock()
	index := app.historyIndex
	app.historyMu.Unlock()
	if index != nil {
		t.Fatal("project switch retained the previous project's history index")
	}
	if _, err := app.GetHistoryPreview("old-project-entry"); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("old history identity after switch error = %v", err)
	}
}
