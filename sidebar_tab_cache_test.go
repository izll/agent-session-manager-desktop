package main

import (
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestAReadingStandsOnlyWhileTheScreenCannotHaveChanged(t *testing.T) {
	now := time.Unix(1000, 0)
	state := session.TabState{LastOutput: 900, Identity: "55\t0\tbash\t80x24"}
	read := tabReading{state: state, readAt: time.Unix(995, 0)}
	limit := 15 * time.Second
	if !stillGood(read, state, limit, now) {
		t.Fatal("an unchanged tab was not recognised")
	}
	for name, test := range map[string]struct {
		read  tabReading
		state session.TabState
		now   time.Time
	}{
		"new output":  {read, session.TabState{LastOutput: 901, Identity: state.Identity}, now},
		"new process": {read, session.TabState{LastOutput: 900, Identity: "56\t0\tbash\t80x24"}, now},
		"pane died":   {read, session.TabState{LastOutput: 900, Identity: "55\t1\tbash\t80x24"}, now},
		"resized":     {read, session.TabState{LastOutput: 900, Identity: "55\t0\tbash\t90x24"}, now},
		"too old":     {read, state, time.Unix(1011, 0)},
		// Output later in the second this was read would look exactly the same.
		"same second": {tabReading{state: state, readAt: time.Unix(900, 0)}, state, time.Unix(901, 0)},
		"never read":  {tabReading{}, state, now},
	} {
		if stillGood(test.read, test.state, limit, test.now) {
			t.Errorf("%s: stood", name)
		}
	}
}

// A busy tab stops being busy with no output, by its spinner standing still.
func TestABusyTabIsAlwaysReadAgain(t *testing.T) {
	var readings tabReadings
	states := map[int]session.TabState{1: {LastOutput: 900, Identity: "x"}}
	key := tabReadingKey("s", 1)
	at := time.Unix(995, 0)
	readings.store(key, states, 1, at, session.ActivityBusy, true, TabStatusInfo{})
	if _, ok := readings.usable(key, states, 1, at.Add(time.Second)); ok {
		t.Fatal("busy reading reused")
	}
	readings.store(key, states, 1, at, session.ActivityWaiting, true, TabStatusInfo{Name: "w"})
	if read, ok := readings.usable(key, states, 1, at.Add(time.Second)); !ok || read.status.Name != "w" {
		t.Fatalf("waiting reading: %+v, %v", read, ok)
	}
	// A failed reading says nothing about the screen, and forgets the good one too.
	readings.store(key, states, 1, at, session.ActivityIdle, false, TabStatusInfo{})
	if _, ok := readings.usable(key, states, 1, at.Add(time.Second)); ok {
		t.Fatal("failed reading reused")
	}
	// A listing that failed means not knowing.
	readings.store(key, states, 1, at, session.ActivityIdle, true, TabStatusInfo{})
	if _, ok := readings.usable(key, nil, 1, at.Add(time.Second)); ok {
		t.Fatal("reused without a listing")
	}
	if _, ok := readings.usable(tabReadingKey("other", 1), states, 1, at.Add(time.Second)); ok {
		t.Fatal("another session's reading")
	}
}

func TestAConversationIsLookedUpWhenItCouldHaveChanged(t *testing.T) {
	var readings tabReadings
	states := map[int]session.TabState{1: {LastOutput: 900, Identity: "x"}}
	key := tabReadingKey("s", 1)
	now := time.Unix(1000, 0)
	if !readings.conversationCheckDue(key, states, 1, now) {
		t.Fatal("first time not due")
	}
	if readings.conversationCheckDue(key, states, 1, now.Add(time.Second)) {
		t.Fatal("due with nothing changed")
	}
	moved := map[int]session.TabState{1: {LastOutput: 1000, Identity: "x"}}
	// A working agent prints all the time: not on every tick.
	if readings.conversationCheckDue(key, moved, 1, now.Add(2*time.Second)) {
		t.Fatal("due again right away")
	}
	if !readings.conversationCheckDue(key, moved, 1, now.Add(conversationCheckMinGap+time.Second)) {
		t.Fatal("not due after output")
	}
	if !readings.conversationCheckDue(key, nil, 1, now.Add(7*time.Second)) {
		t.Fatal("not due without a listing")
	}
	if !readings.conversationCheckDue(key, states, 1, now.Add(8*time.Second)) {
		t.Fatal("not due after not knowing")
	}
	if !readings.conversationCheckDue(key, states, 1, now.Add(10*time.Minute)) {
		t.Fatal("not due after the age limit")
	}
}

func TestTheAgeLimitStaysInItsRange(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := range 200 {
		limit := tabReadingLimit(tabReadingKey("session", i))
		if limit < tabReadingMinAge || limit >= tabReadingMinAge+tabReadingAgeSpread {
			t.Fatalf("limit = %v", limit)
		}
		seen[limit] = true
	}
	if len(seen) < 3 {
		t.Fatalf("all tabs fall due together: %v", seen)
	}
}
