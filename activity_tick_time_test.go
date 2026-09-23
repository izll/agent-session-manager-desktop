package main

import (
	"strings"
	"testing"
)

// Terminal directories were saved only when a session or tab was stopped, and
// a machine that restarts stops nothing. The poll saves them while the
// sessions run, through the targeted write that leaves the rest of the record
// alone, and only while this instance holds the project.
func TestThePollSavesTerminalDirectories(t *testing.T) {
	source := readSourceFile(t, "app.go")
	at := strings.Index(source, "func (a *App) getSidebarUpdates(")
	if at < 0 {
		t.Fatal("getSidebarUpdates is gone; this test needs rewriting")
	}
	poll := source[at:]
	poll = poll[:strings.Index(poll, "\n}\n")]
	if !strings.Contains(poll, "if mayPersist && saveTerminalDirs {") ||
		!strings.Contains(poll, "a.storage.RecordTerminalDirsForProject(projectID, inst.ID, inst.TerminalDirsNow(ctx))") {
		t.Error("the poll does not save where the terminal tabs are, so a restart " +
			"brings them back where they were last stopped")
	}
}
