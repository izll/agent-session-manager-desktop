package main

import (
	"testing"
	"time"

	"asmgr-desktop/session"
)

// The sidebar's activity ordering asks "where was I". UpdatedAt cannot answer
// it: that moves only when a session is started or stopped, so a session
// started in the morning and busy ever since sorted below one started five
// minutes ago and idle throughout — which is what made the descending sort look
// ascending.
func TestActivityOrderPrefersObservedWork(t *testing.T) {
	morning := time.Date(2026, 9, 4, 6, 35, 0, 0, time.UTC)
	busyAt := time.Date(2026, 9, 4, 14, 50, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 4, 14, 41, 0, 0, time.UTC)

	workedAllDay := &session.Instance{UpdatedAt: morning, LastActiveAt: busyAt}
	startedLater := &session.Instance{UpdatedAt: recent}

	if !lastActivityTime(workedAllDay).After(lastActivityTime(startedLater)) {
		t.Fatal("a session busy until 14:50 sorted below one merely started at 14:41")
	}
}

// A session that has not run since the app started has nothing observed. It
// still needs a place in the order, or every such session collapses into one
// undated group.
func TestActivityOrderFallsBackToStartStop(t *testing.T) {
	started := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	inst := &session.Instance{UpdatedAt: started}
	if got := lastActivityTime(inst); !got.Equal(started) {
		t.Fatalf("no fallback for an unobserved session: got %v want %v", got, started)
	}
}

// Restarting a session moves UpdatedAt to now, which is genuinely more recent
// than work observed before it. The later of the two has to win in both
// directions, not just one.
func TestActivityOrderTakesTheLaterOfTheTwo(t *testing.T) {
	oldWork := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	restarted := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
	inst := &session.Instance{UpdatedAt: restarted, LastActiveAt: oldWork}
	if got := lastActivityTime(inst); !got.Equal(restarted) {
		t.Fatalf("a restart older than the last observed work: got %v want %v", got, restarted)
	}
}

// A never-recorded time must not be formatted: the zero time reads as year 1,
// which sorts as the oldest thing on the list instead of as "unknown".
func TestUnrecordedTimeIsSentAsEmpty(t *testing.T) {
	if got := formatSessionTimestamp(time.Time{}); got != "" {
		t.Fatalf("the zero time was sent as %q", got)
	}
}
