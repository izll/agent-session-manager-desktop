package session

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// LastTerminalOutputContext says when a terminal tab of this session last printed
// anything, for the activity ordering. A shell has no busy or waiting state to read
// the way an agent has, so what it printed last is the nearest thing to when it was
// last in use: typing shows up too, as a shell echoes it. Zero when the session has
// none, or it cannot be told.
//
// One listing of the session's own machine; a tab on another one is not asked, as
// that would be a round trip per server per poll.
func (i *Instance) LastTerminalOutputContext(ctx context.Context) time.Time {
	wanted := map[int]bool{}
	for _, window := range i.FollowedWindows {
		if window.Agent == AgentTerminal && window.sameMachine(i.ServerID, i.ServerID) {
			wanted[window.Index] = true
		}
	}
	if i.Agent == AgentTerminal {
		wanted[i.GetMainWindowIndex()] = true
	}
	if len(wanted) == 0 {
		return time.Time{}
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.tmuxOutputContext(commandCtx, "list-windows", "-t", i.TmuxSessionName(),
		"-F", "#{window_index}\t#{window_activity}")
	if err != nil {
		return time.Time{}
	}
	return latestOutputFromListing(output, wanted, time.Now())
}

// latestOutputFromListing reads lines of index and unix time. A server's clock can be
// ahead of this one, and a time still to come would hold the session at the top.
func latestOutputFromListing(output []byte, wanted map[int]bool, now time.Time) time.Time {
	var latest time.Time
	for _, line := range strings.Split(string(output), "\n") {
		index, stamp, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found {
			continue
		}
		windowIdx, err := strconv.Atoi(index)
		if err != nil || !wanted[windowIdx] {
			continue
		}
		seconds, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || seconds <= 0 {
			continue
		}
		if at := time.Unix(seconds, 0); at.After(latest) {
			latest = at
		}
	}
	if latest.After(now) {
		return now
	}
	return latest
}
