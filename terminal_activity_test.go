package main

import (
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestOnlyNewerTerminalOutputCountsAsActivity(t *testing.T) {
	stored := time.Unix(1000, 0)
	instances := []*session.Instance{
		{ID: "newer", LastActiveAt: stored},
		{ID: "older", LastActiveAt: stored},
		{ID: "same", LastActiveAt: stored},
		{ID: "silent", LastActiveAt: stored},
		{ID: "busy", LastActiveAt: stored},
	}
	busyAt := time.Unix(5000, 0)
	active := map[string]time.Time{"busy": busyAt}
	addNewTerminalOutput(active, map[string]time.Time{
		"newer": time.Unix(2000, 0),
		"older": time.Unix(500, 0),
		"same":  stored,
	}, instances)
	if len(active) != 2 || active["newer"].Unix() != 2000 || !active["busy"].Equal(busyAt) {
		t.Fatalf("active = %v", active)
	}
}
