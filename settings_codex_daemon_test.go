package main

import (
	"asmgr-desktop/session"
	"context"
	"testing"
)

// Codex is started without its background server unless the settings say
// otherwise: a fresh settings file reads as "no daemon", a saved choice is
// read back, and it takes effect on the next start without a restart.
func TestCodexDaemonSettingDefaultsOffAndApplies(t *testing.T) {
	storage := guardedTestStorage(t)
	app := &App{storage: storage, projectLocked: true}

	oldMouse, oldShell := applyRuntimeMouseCopy, applyRuntimeTerminalShell
	applyRuntimeMouseCopy = func(context.Context, bool) {}
	applyRuntimeTerminalShell = func(string) {}
	t.Cleanup(func() {
		applyRuntimeMouseCopy, applyRuntimeTerminalShell = oldMouse, oldShell
		session.SetCodexUseDaemon(false)
	})

	info, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if info.CodexUseDaemon {
		t.Fatal("a fresh install lets Codex use its background server")
	}

	info.CodexUseDaemon = true
	if err := app.SaveSettings(*info, ""); err != nil {
		t.Fatal(err)
	}
	if !session.CodexUseDaemon() {
		t.Error("choosing the daemon did not reach the next Codex start")
	}
	reread, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !reread.CodexUseDaemon {
		t.Error("the choice was not stored")
	}

	// A project switch re-applies what that project has stored.
	session.SetCodexUseDaemon(false)
	app.applyActiveProjectRuntimeSettings()
	if !session.CodexUseDaemon() {
		t.Error("the stored choice was not applied when the project was loaded")
	}
}
