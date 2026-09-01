package main

import (
	"testing"

	"asmgr-desktop/session"
)

// isolateHomeForTest points the config at a scratch directory. Both variables:
// os.UserHomeDir reads USERPROFILE on Windows, not HOME.
func isolateHomeForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// Forgetting must clear the geometry in a way the restore path actually treats
// as "never saved" — main.go gates on a positive width and height, so zeroing
// only the position would leave the old size in force.
func TestForgetWindowGeometryClearsWhatRestoreChecks(t *testing.T) {
	isolateHomeForTest(t)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpdateSettings(func(s *session.Settings) {
		s.WindowX, s.WindowY = 900, 400
		s.WindowWidth, s.WindowHeight = 1200, 800
	}); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage}
	if err := app.ForgetWindowGeometry(); err != nil {
		t.Fatalf("ForgetWindowGeometry: %v", err)
	}

	_, _, settings, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.WindowWidth != 0 || settings.WindowHeight != 0 {
		t.Fatalf("size survived: %dx%d — restore would still honour it",
			settings.WindowWidth, settings.WindowHeight)
	}
	if settings.WindowX != 0 || settings.WindowY != 0 {
		t.Fatalf("position survived: (%d,%d)", settings.WindowX, settings.WindowY)
	}
}

// Forgetting is a targeted edit: the rest of the settings must survive it.
func TestForgetWindowGeometryLeavesOtherSettingsAlone(t *testing.T) {
	isolateHomeForTest(t)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpdateSettings(func(s *session.Settings) {
		s.WindowWidth, s.WindowHeight = 1200, 800
		s.CompactList = true
		s.Language = "hu"
		s.TerminalRenderer = "dom"
	}); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage}
	if err := app.ForgetWindowGeometry(); err != nil {
		t.Fatal(err)
	}

	_, _, settings, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.CompactList || settings.Language != "hu" || settings.TerminalRenderer != "dom" {
		t.Fatalf("unrelated settings were disturbed: compact=%v lang=%q renderer=%q",
			settings.CompactList, settings.Language, settings.TerminalRenderer)
	}
}

// Nothing saved yet is not an error worth surfacing to the user.
func TestForgetWindowGeometryWithNothingSaved(t *testing.T) {
	isolateHomeForTest(t)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage}
	if err := app.ForgetWindowGeometry(); err != nil {
		t.Fatalf("forgetting an unset geometry should be a no-op, got: %v", err)
	}
}
