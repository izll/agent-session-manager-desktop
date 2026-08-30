package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// runtime.Screen is an alias for an internal type, and its ScreenSize is not
// re-exported, so the size is filled in by field rather than named literal.
func screen(w, h int, current, primary bool) runtime.Screen {
	var s runtime.Screen
	s.IsCurrent = current
	s.IsPrimary = primary
	s.Size.Width = w
	s.Size.Height = h
	return s
}

// The window must reopen where it was left, so an ordinary saved position has
// to survive. This is the case the whole feature exists for.
func TestSavedPositionOnTheDesktopIsAccepted(t *testing.T) {
	screens := []runtime.Screen{screen(2560, 1440, true, true)}
	for _, p := range []struct{ x, y int }{{0, 0}, {256, 144}, {1200, 700}, {2400, 1300}} {
		if !positionLooksReachable(screens, p.x, p.y) {
			t.Errorf("position (%d,%d) was rejected but is plainly on screen", p.x, p.y)
		}
	}
}

// A window may hang off the top or left edge — dragging one there is normal,
// and the title bar stays reachable.
func TestSlightlyOffscreenPositionsAreAccepted(t *testing.T) {
	screens := []runtime.Screen{screen(2560, 1440, true, true)}
	if !positionLooksReachable(screens, -80, -40) {
		t.Error("a window overlapping the top-left edge should still be restored")
	}
}

// The failure this guards against: a monitor unplugged since the position was
// saved leaves coordinates pointing at a desktop that no longer exists.
func TestPositionOnAVanishedMonitorIsRejected(t *testing.T) {
	screens := []runtime.Screen{screen(2560, 1440, true, true)}
	if positionLooksReachable(screens, 5200, 300) {
		t.Error("a position beyond every screen should fall back to centring")
	}
	if positionLooksReachable(screens, 300, 3400) {
		t.Error("a position below every screen should fall back to centring")
	}
}

// With a second monitor attached, the same position must be accepted again.
func TestPositionOnASecondMonitorIsAccepted(t *testing.T) {
	screens := []runtime.Screen{
		screen(2560, 1440, true, true),
		screen(1920, 1080, false, false),
	}
	if !positionLooksReachable(screens, 3000, 300) {
		t.Error("a position on the second screen should be restored")
	}
}

func TestCurrentScreenIsPreferredOverTheFirst(t *testing.T) {
	screens := []runtime.Screen{
		screen(1280, 720, false, false),
		screen(2560, 1440, true, false),
	}
	if got := currentScreen(screens).Size.Width; got != 2560 {
		t.Fatalf("currentScreen picked the %dpx screen, want the current one (2560)", got)
	}
}

func TestPrimaryScreenIsUsedWhenNoneIsCurrent(t *testing.T) {
	screens := []runtime.Screen{
		screen(1280, 720, false, false),
		screen(2560, 1440, false, true),
	}
	if got := currentScreen(screens).Size.Width; got != 2560 {
		t.Fatalf("currentScreen picked the %dpx screen, want the primary one (2560)", got)
	}
}

func TestCurrentScreenFallsBackToTheFirst(t *testing.T) {
	screens := []runtime.Screen{screen(1280, 720, false, false)}
	if got := currentScreen(screens).Size.Width; got != 1280 {
		t.Fatalf("currentScreen returned %dpx with nothing marked, want the only screen", got)
	}
}
