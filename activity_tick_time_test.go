package main

import (
	"strings"
	"testing"
)

// Sessions working at the same moment must share one activity time per poll.
//
// Each used to get its own time.Now(), taken in the order its poll happened to
// finish, so they swapped places every two seconds in the activity-sorted list
// and stepping through it landed somewhere unexpected. With one time they tie,
// and ties sort by name.
func TestSessionsActiveInOneTickShareTheirTime(t *testing.T) {
	source := readSourceFile(t, "app.go")
	at := strings.Index(source, "activeNow := make(map[string]time.Time)")
	if at < 0 {
		t.Fatal("the activity collection loop is gone; this test needs rewriting")
	}
	loop := source[at:]
	loop = loop[:strings.Index(loop, "result.observations = append")]

	if strings.Contains(loop, "activeNow[sr.instID] = time.Now()") {
		t.Error("each active session gets its own time, so their order " +
			"reshuffles on every poll")
	}
	if !strings.Contains(loop, "tickTime := time.Now()") ||
		!strings.Contains(loop, "activeNow[sr.instID] = tickTime") {
		t.Error("the sessions active in one poll do not share a time")
	}
}
