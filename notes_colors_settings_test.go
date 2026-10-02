package main

import (
	"context"
	"testing"
)

// The notes' colours are remembered like the interface colours are, and a
// config written before they existed reads back as the default look: an
// empty background (the dark one the notes always had) and automatic text.
func TestNotesColorsSettingsRoundTrip(t *testing.T) {
	storage := guardedTestStorage(t)
	app := &App{storage: storage, projectLocked: true}

	oldMouse, oldShell := applyRuntimeMouseCopy, applyRuntimeTerminalShell
	applyRuntimeMouseCopy = func(context.Context, bool) {}
	applyRuntimeTerminalShell = func(string) {}
	t.Cleanup(func() { applyRuntimeMouseCopy, applyRuntimeTerminalShell = oldMouse, oldShell })

	fresh, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.NotesBackground != "" || fresh.NotesText != "" {
		t.Fatalf("a fresh config has notes colours %q/%q; want the default look (empty)",
			fresh.NotesBackground, fresh.NotesText)
	}

	want := SettingsInfo{
		NotesBackground:      "custom",
		NotesBackgroundColor: "#f4ecd8",
		NotesText:            "custom",
		NotesTextColor:       "#3b2f20",
	}
	if err := app.SaveSettings(want, ""); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.NotesBackground != want.NotesBackground || got.NotesBackgroundColor != want.NotesBackgroundColor ||
		got.NotesText != want.NotesText || got.NotesTextColor != want.NotesTextColor {
		t.Errorf("saved %q %q %q %q, read back %q %q %q %q",
			want.NotesBackground, want.NotesBackgroundColor, want.NotesText, want.NotesTextColor,
			got.NotesBackground, got.NotesBackgroundColor, got.NotesText, got.NotesTextColor)
	}

	// Back to the default: the empty values are stored as such, not kept from
	// the previous save.
	if err := app.SaveSettings(SettingsInfo{}, ""); err != nil {
		t.Fatal(err)
	}
	got, err = app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.NotesBackground != "" || got.NotesText != "" {
		t.Errorf("restoring the default left %q/%q", got.NotesBackground, got.NotesText)
	}
}
