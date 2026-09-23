package main

import (
	"strings"
	"testing"
	"time"
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
		!strings.Contains(poll, "terminalDirs[inst.ID] = dirs") ||
		!strings.Contains(poll, "a.storage.RecordTerminalDirsForProject(projectID, terminalDirs)") {
		t.Error("the poll does not save where the terminal tabs are, so a restart " +
			"brings them back where they were last stopped")
	}
}

// The pane read a tab's unreachable and missing marks from TabStatuses, which
// carries only agent tabs and only for sessions with more than one — so a
// terminal tab, or a session's single agent tab, never showed either. Every
// such tab goes into TabAvailability, taken before that filter.
func TestEveryUnavailableTabIsReported(t *testing.T) {
	source := readSourceFile(t, "app.go")
	at := strings.Index(source, "for _, ts := range tabStatuses {")
	if at < 0 {
		t.Fatal("the per-tab loop is gone; this test needs rewriting")
	}
	loop := source[at:]
	loop = loop[:strings.Index(loop, "if ts.Agent != string(session.AgentTerminal) {")]
	if !strings.Contains(loop, "if ts.Unreachable || ts.Missing {") ||
		!strings.Contains(loop, "sr.unavailable = append(sr.unavailable") {
		t.Error("unavailable tabs are collected after the agent filter, or not at all")
	}
	if !strings.Contains(source, "result.TabAvailability[sr.instID] = sr.unavailable") {
		t.Error("the collected tabs never reach the update")
	}
}

// The terminal-directory saves are paced by the app's shared throttle, set to
// terminalDirSaveInterval when the app is made.
func TestTerminalDirSavesArePaced(t *testing.T) {
	app := NewApp()
	now := time.Now()
	if !app.terminalDirSaves.allow(now) {
		t.Fatal("the first save was held back")
	}
	if app.terminalDirSaves.allow(now.Add(terminalDirSaveInterval / 2)) {
		t.Error("a second save went through inside the interval")
	}
	if !app.terminalDirSaves.allow(now.Add(terminalDirSaveInterval)) {
		t.Error("a save after the interval was held back")
	}
}
