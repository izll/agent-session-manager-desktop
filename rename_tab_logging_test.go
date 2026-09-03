package main

import (
	"strings"
	"testing"
)

// A tab name that goes missing leaves nothing to look at: the name lives in
// tmux and in the store, and when the two disagree there is no way afterwards
// to tell which step failed. That happened, and could not be traced.
//
// So every exit from RenameTab has to say what it did. Checked against the
// source because the alternative is a fake logger threaded through the App,
// which would test the plumbing rather than the property that matters.
func TestRenameTabLogsEveryOutcome(t *testing.T) {
	src := readTextFile(t, "app.go")
	start := strings.Index(src, "func (a *App) RenameTab(")
	if start < 0 {
		t.Fatal("RenameTab is gone")
	}
	body := src[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}

	// Every error return, plus the success. Counting "return" outright would
	// also catch the rollback closure's, which is not an exit from RenameTab.
	errorExits := strings.Count(body, "\t\treturn err\n")
	logs := strings.Count(body, "log.Printf(")
	if logs < errorExits+1 {
		t.Fatalf("RenameTab has %d error exits and a success path, but only %d log "+
			"lines — a rename can fail silently again", errorExits, logs)
	}
	// The success line is the one that proves a rename actually happened.
	if !strings.Contains(body, `%q→%q`) {
		t.Error("no log line records the old and new name together, so a rename " +
			"cannot be told from a no-op afterwards")
	}
	// Whether it was the main tab decides which field stores the name, and the
	// two are restored by different code.
	if !strings.Contains(body, "GetMainWindowIndex()") {
		t.Error("the log does not say whether the main tab was renamed, which is " +
			"what decides where the name is stored")
	}
}
