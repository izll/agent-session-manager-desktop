package main

import (
	"strings"
	"testing"
)

func TestGetPreviewRejectsStaleProjectTarget(t *testing.T) {
	storage := guardedTestStorage(t)
	app := &App{storage: storage, projectLocked: true}

	preview, err := app.GetPreview("same-session-id", 100, "previous-project")
	if err == nil || !strings.Contains(err.Error(), "active project changed") {
		t.Fatalf("GetPreview = (%+v, %v), want stale-project error", preview, err)
	}
}
