package main

import (
	"os"
	"strings"
	"testing"
)

// Sessions started out of view take the viewer's size: the terminal has to
// report it, the app has to remember it before anything can start a session,
// and a size still waiting to be saved must not be lost at quit.
func TestTheViewersSizeReachesSessionStart(t *testing.T) {
	app, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	startup := functionBody(t, string(app), "func (a *App) startup(")
	configure := strings.Index(startup, "session.ConfigureStartSize(storage.StartSizePath())")
	if configure < 0 {
		t.Fatal("startup does not load the remembered terminal size")
	}
	if reopen := strings.Index(startup, "Reopen"); reopen >= 0 && reopen < configure {
		t.Fatal("startup can reopen sessions before the terminal size is loaded")
	}
	if !strings.Contains(functionBody(t, string(app), "func (a *App) shutdown("), "session.FlushStartSize()") {
		t.Fatal("shutdown does not save a pending terminal size")
	}

	ws, err := os.ReadFile("terminal_ws.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{
		"session.SetStartSize(attachCols, attachRows)",
		"session.SetStartSize(cols, rows)",
	} {
		if !strings.Contains(string(ws), call) {
			t.Errorf("terminal_ws.go does not report the size: %s missing", call)
		}
	}
}
