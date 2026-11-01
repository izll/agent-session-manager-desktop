package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"

	"github.com/zalando/go-keyring"
)

func passwordTestApp(t *testing.T) *App {
	t.Helper()
	keyring.MockInit()
	return &App{storage: guardedTestStorage(t), projectLocked: true, servers: newServerPool()}
}

// The whole point of the design: nothing the interface can call returns a secret.
func TestNoPasswordMethodReturnsASecret(t *testing.T) {
	app := passwordTestApp(t)
	const secret = "se:cret-value"
	if _, err := app.SaveLocalPassword("", "Prod", "root", secret); err != nil {
		t.Fatal(err)
	}
	list, err := app.GetPasswords(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 1 || strings.Contains(strings.ReplaceAll(strings.Join([]string{list.Entries[0].ID, list.Entries[0].Name, list.Entries[0].Username}, " "), "", ""), secret) {
		t.Fatalf("list = %+v", list.Entries)
	}
	source, err := os.ReadFile("passwords_api.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(line, "func (a *App) ") && strings.Contains(line, "Secret") {
			t.Errorf("exposed to the interface: %s", line)
		}
	}
}

func TestInsertingIntoASessionThatIsNotThereFails(t *testing.T) {
	app := passwordTestApp(t)
	id, err := app.SaveLocalPassword("", "Prod", "", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InsertPassword("local", id, "no-such-session", 0, false, ""); err == nil {
		t.Fatal("no error")
	}
}

func TestInsertingAnUnknownPasswordFailsBeforeTouchingATab(t *testing.T) {
	app := passwordTestApp(t)
	if err := app.storage.AddInstance(&session.Instance{ID: "s1", Name: "s", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	err := app.InsertPassword("local", "nope", "s1", 0, false, "")
	if err == nil || !strings.Contains(err.Error(), "passwordNotFound") {
		t.Fatalf("err = %v", err)
	}
}

// Wiring only the running app exercises.
func TestPasswordsAreForgottenAtShutdown(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(functionBody(t, string(source), "func (a *App) shutdown("), "a.stopPasswords()") {
		t.Fatal("shutdown does not lock the password sources")
	}
}

// A tool waiting for an answer nobody will give: Esc must end it and type nothing.
func TestAFetchCanBeCalledOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	app := passwordTestApp(t)
	tool := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then echo Mail; else sleep 30; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := app.SaveCustomPasswordSource("", "Tool", tool+" ls", tool+" get {name}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetPasswords(false); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- app.InsertPassword("custom:"+id, "Mail", "no-such-session", 0, false, "") }()
	time.Sleep(200 * time.Millisecond)
	app.CancelPasswordInsert()
	select {
	case err := <-finished:
		// The session does not exist, so an error would mean it went on to type.
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting for the tool")
	}
}

func TestBitwardenLoginIsRefusedOnAServer(t *testing.T) {
	for name, inst := range map[string]*session.Instance{
		"session on a server": {ID: "s1", ServerID: "srv"},
		"tab on a server":     {ID: "s2", FollowedWindows: []session.FollowedWindow{{Index: 3, ServerID: "srv"}}},
	} {
		if tabRunsOnThisMachine(inst, 3) {
			t.Errorf("%s: allowed", name)
		}
	}
	if !tabRunsOnThisMachine(&session.Instance{ID: "s3"}, 3) {
		t.Error("a local tab was refused")
	}
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "remote", Name: "remote", Path: t.TempDir(), ServerID: "srv"}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	if _, err := app.OpenBitwardenLogin("acc", "remote", 0, ""); !errors.Is(err, errBitwardenLoginNeedsLocalTab) {
		t.Fatalf("err = %v", err)
	}
}
