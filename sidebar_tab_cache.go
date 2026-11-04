package main

import (
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"asmgr-desktop/session"
)

// What the sidebar knows about a tab, kept between sweeps.
//
// A tab's activity, status line and the rest are read off its screen, and reading took
// about nine multiplexer calls per tab, every tick, whether or not anything had changed.
// With a hundred tabs that was most of what the app did while nothing was happening. So
// a tab whose screen cannot have changed is answered from its last reading.
//
// tmux keeps when a window last printed, and that comes with one listing per session.
// A reading stays good while that listing says the same, with two exceptions. A busy tab
// is always read: it stops being busy with no output at all, by its spinner standing
// still or its grace running out. And every reading has an age limit, for whatever
// could still change off screen, like a setting or the helper of a server.

const (
	// tabReadingMinAge is long because it is paid for by every quiet tab: at ten
	// seconds, a hundred tabs meant several full readings a second, measured as most
	// of what was left. A setting that takes effect off screen shows up this late.
	tabReadingMinAge = time.Minute
	// The limit differs per tab between these, as tabs read together at startup
	// would otherwise all fall due together for ever after.
	tabReadingAgeSpread = time.Minute
	// conversationCheckMinGap: a working agent prints all the time, and which
	// conversation it is on hardly ever changes.
	conversationCheckMinGap = 5 * time.Second
)

type tabReading struct {
	state    session.TabState
	readAt   time.Time
	activity session.SessionActivity
	status   TabStatusInfo
}

type tabReadings struct {
	mu      sync.Mutex
	entries map[string]tabReading
	checked map[string]tabReading
}

func tabReadingKey(sessionID string, windowIdx int) string {
	return sessionID + "\x00" + strconv.Itoa(windowIdx)
}

func tabReadingLimit(key string) time.Duration {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return tabReadingMinAge + time.Duration(h.Sum32()%uint32(tabReadingAgeSpread/time.Second))*time.Second
}

// stillGood says whether what was read earlier must still be what the screen shows.
//
// tmux keeps whole seconds, so a reading is only known to have seen a second's output
// if it started in a later second: output later in the same second reads the same.
func stillGood(read tabReading, state session.TabState, limit time.Duration, now time.Time) bool {
	return read.state.Identity == state.Identity &&
		read.state.LastOutput == state.LastOutput &&
		read.readAt.Unix() > state.LastOutput &&
		now.Sub(read.readAt) < limit
}

// usable returns the last reading of a tab if it can stand in for a new one.
func (r *tabReadings) usable(key string, states map[int]session.TabState, windowIdx int, now time.Time) (tabReading, bool) {
	state, known := states[windowIdx]
	if !known {
		return tabReading{}, false
	}
	r.mu.Lock()
	read, found := r.entries[key]
	r.mu.Unlock()
	if !found || read.activity == session.ActivityBusy || !stillGood(read, state, tabReadingLimit(key), now) {
		return tabReading{}, false
	}
	return read, true
}

// store keeps a reading. readAt is from before the reading began, and only a reading
// that worked is kept: one that failed says nothing about the screen.
func (r *tabReadings) store(key string, states map[int]session.TabState, windowIdx int, readAt time.Time, activity session.SessionActivity, valid bool, status TabStatusInfo) {
	state, known := states[windowIdx]
	r.mu.Lock()
	defer r.mu.Unlock()
	if !known || !valid {
		delete(r.entries, key)
		return
	}
	if r.entries == nil {
		r.entries = map[string]tabReading{}
	}
	r.entries[key] = tabReading{state: state, readAt: readAt, activity: activity, status: status}
}

// conversationCheckDue says whether to look up which conversation a tab is on. Going to
// another one inside the agent, as /resume does, draws on its screen.
func (r *tabReadings) conversationCheckDue(key string, states map[int]session.TabState, windowIdx int, now time.Time) bool {
	state, known := states[windowIdx]
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, seen := r.checked[key]; known && seen {
		if now.Sub(last.readAt) < conversationCheckMinGap || stillGood(last, state, tabReadingLimit(key), now) {
			return false
		}
	}
	if r.checked == nil {
		r.checked = map[string]tabReading{}
	}
	if known {
		r.checked[key] = tabReading{state: state, readAt: now}
	} else {
		delete(r.checked, key)
	}
	return true
}
