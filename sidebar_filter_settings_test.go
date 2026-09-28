package main

import (
	"context"
	"testing"
)

// The sidebar filter is remembered like the sort order is: through the
// settings, so it survives a restart.
func TestSidebarFilterSettingsRoundTrip(t *testing.T) {
	storage := guardedTestStorage(t)
	app := &App{storage: storage, projectLocked: true}

	oldMouse, oldShell := applyRuntimeMouseCopy, applyRuntimeTerminalShell
	applyRuntimeMouseCopy = func(context.Context, bool) {}
	applyRuntimeTerminalShell = func(string) {}
	t.Cleanup(func() { applyRuntimeMouseCopy, applyRuntimeTerminalShell = oldMouse, oldShell })

	for _, tc := range []struct {
		hide       bool
		days, want int
	}{
		{true, 7, 7},
		{false, 1, 1},
		{true, 30, 30},
		{false, 0, 0},
		// Not a choice the menu offers: stored as no window rather than as a
		// filter nobody can see or turn off.
		{true, 3, 0},
		{false, -1, 0},
	} {
		if err := app.SaveSettings(SettingsInfo{SidebarHideInactive: tc.hide, SidebarActiveWithinDays: tc.days}, ""); err != nil {
			t.Fatal(err)
		}
		got, err := app.GetSettings()
		if err != nil {
			t.Fatal(err)
		}
		if got.SidebarHideInactive != tc.hide || got.SidebarActiveWithinDays != tc.want {
			t.Errorf("saved hide=%v days=%d, read back hide=%v days=%d (want days=%d)",
				tc.hide, tc.days, got.SidebarHideInactive, got.SidebarActiveWithinDays, tc.want)
		}
	}
}
