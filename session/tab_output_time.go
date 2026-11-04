package session

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const tabStateFormat = "#{window_index}\t#{window_activity}\t#{pane_pid}\t#{pane_dead}\t#{pane_current_command}\t#{window_width}x#{window_height}"

// windowSnapshot is one instant of a session's windows, in the formats its readers
// expect, so a sweep can ask once for all sessions rather than several times for each.
type windowSnapshot struct {
	states  []byte
	markers []byte
	// sessions is shared by the whole sweep. A tab being read needs it to find its
	// terminal's own view; a sweep of quiet tabs never asks.
	sessions *sharedSessionListing
}

type sharedSessionListing struct {
	once   sync.Once
	output []byte
	err    error
}

func (l *sharedSessionListing) get(ctx context.Context, inst *Instance) ([]byte, error) {
	l.once.Do(func() {
		l.output, l.err = inst.tmuxOutputContext(ctx, "list-sessions", "-F", "#{session_name} #{session_attached}")
	})
	return l.output, l.err
}

// SnapshotLocalWindows lists the windows of every session on this computer in one call,
// for the rest of a sweep to read from. The instances must be ones loaded for this sweep
// and thrown away with it, as they will go on answering from this. A session the listing
// does not cover, or a listing that failed, simply goes on asking for itself.
func SnapshotLocalWindows(ctx context.Context, instances []*Instance) {
	var ask *Instance
	for _, inst := range instances {
		if inst.ServerID == "" && inst.Status == StatusRunning {
			ask = inst
			break
		}
	}
	if ask == nil {
		return
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := ask.tmuxOutputContext(commandCtx, "list-windows", "-a", "-F", "#{session_name}\t#{@asmgr_main}\t"+tabStateFormat)
	if err != nil {
		return
	}
	sessions := &sharedSessionListing{}
	snapshots := map[string]*windowSnapshot{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		snapshot := snapshots[fields[0]]
		if snapshot == nil {
			snapshot = &windowSnapshot{sessions: sessions}
			snapshots[fields[0]] = snapshot
		}
		index, _, _ := strings.Cut(fields[2], "\t")
		snapshot.states = append(append(snapshot.states, fields[2]...), '\n')
		snapshot.markers = append(append(snapshot.markers, index+"\t"+fields[1]...), '\n')
	}
	for _, inst := range instances {
		if inst.ServerID == "" {
			inst.windowSnapshot = snapshots[inst.TmuxSessionName()]
		}
	}
}

// TabState is what one listing of a session's windows says about a tab, without
// reading its contents.
type TabState struct {
	// LastOutput is the unix second the pane last printed anything. tmux keeps it
	// per window, and it moves for nothing but output and resizing.
	LastOutput int64
	// Identity holds what else would change a reading with no output to show for it:
	// the process, whether it has died, and the size.
	Identity string
}

// TabStatesContext lists the state of the session's tabs on its own machine, by window
// index, in one round trip. Nil when it cannot be told, which callers treat as not
// knowing. A tab on another machine is left out, as its index means nothing here.
func (i *Instance) TabStatesContext(ctx context.Context) map[int]TabState {
	var states map[int]TabState
	if i.windowSnapshot != nil {
		states = parseTabStates(i.windowSnapshot.states)
	} else {
		commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
		defer cancel()
		output, err := i.tmuxOutputContext(commandCtx, "list-windows", "-t", i.TmuxSessionName(), "-F", tabStateFormat)
		if err != nil {
			return nil
		}
		states = parseTabStates(output)
	}
	for _, window := range i.FollowedWindows {
		if !window.sameMachine(i.ServerID, i.ServerID) {
			delete(states, window.Index)
		}
	}
	return states
}

// parseTabStates skips a line it cannot read in full. A multiplexer that does not know
// one of these fields answers with it empty, and then nothing is claimed for that tab.
func parseTabStates(output []byte) map[int]TabState {
	states := map[int]TabState{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) != 6 {
			continue
		}
		index, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || seconds <= 0 || fields[2] == "" {
			continue
		}
		states[index] = TabState{LastOutput: seconds, Identity: strings.Join(fields[2:], "\t")}
	}
	return states
}

// LastTerminalOutputContext says when a terminal tab of this session last printed
// anything; see LastTerminalOutputFrom.
func (i *Instance) LastTerminalOutputContext(ctx context.Context) time.Time {
	return i.LastTerminalOutputFrom(i.TabStatesContext(ctx), time.Now())
}

// LastTerminalOutputFrom picks out of a listing when a terminal tab last printed, for
// the activity ordering. A shell has no busy or waiting state to read the way an agent
// has, so what it printed last is the nearest thing to when it was last in use: typing
// shows up too, as a shell echoes it. Zero when the session has none. A server's clock
// can be ahead of this one, and a time still to come would hold the session at the top.
func (i *Instance) LastTerminalOutputFrom(states map[int]TabState, now time.Time) time.Time {
	var latest int64
	note := func(windowIdx int) {
		if state, ok := states[windowIdx]; ok && state.LastOutput > latest {
			latest = state.LastOutput
		}
	}
	for _, window := range i.FollowedWindows {
		if window.Agent == AgentTerminal {
			note(window.Index)
		}
	}
	if i.Agent == AgentTerminal {
		note(i.GetMainWindowIndex())
	}
	if latest == 0 {
		return time.Time{}
	}
	if at := time.Unix(latest, 0); at.Before(now) {
		return at
	}
	return now
}

// String is for logs and test failures.
func (s TabState) String() string { return fmt.Sprintf("%d %q", s.LastOutput, s.Identity) }
