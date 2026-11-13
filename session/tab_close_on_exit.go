package session

import (
	"fmt"
	"strings"
)

// A tab that goes away when what it was opened for has ended.
//
// The tab remembers that, rather than something in memory watching it, so that a tab
// whose command ends while the app is closed still goes the next time it runs.

// Why a tab closes itself, which also says what happens next.
const (
	// TabCloseCommand is kept for tabs saved by versions that ran background
	// commands in a tab: they still close when their command ends.
	TabCloseCommand = "command"
	TabCloseLogin   = "login"
)

// TabToClose is a tab whose process has ended.
type TabToClose struct {
	ID       string
	Index    int
	Reason   string
	ReturnTo int
}

// MarkTabCloseOnExit makes a tab close itself once its process ends. returnTo is the
// tab to go back to if this one is selected by then.
func (i *Instance) MarkTabCloseOnExit(windowIdx int, reason string, returnTo int) {
	if record := i.GetFollowedWindowRecord(windowIdx); record != nil {
		record.CloseOnExit = reason
		record.ReturnToWindow = returnTo
	}
}

// HasTabsClosingOnExit says whether there is anything to ask tmux about.
func (i *Instance) HasTabsClosingOnExit() bool {
	for _, window := range i.FollowedWindows {
		if window.CloseOnExit != "" {
			return true
		}
	}
	return false
}

// ExitedTabsToClose lists the marked tabs whose process has ended.
func (i *Instance) ExitedTabsToClose() []TabToClose {
	var exited []TabToClose
	for _, window := range i.FollowedWindows {
		if window.CloseOnExit != "" && i.TabProcessExited(window.Index) {
			exited = append(exited, TabToClose{window.ID, window.Index, window.CloseOnExit, window.ReturnToWindow})
		}
	}
	return exited
}

// TabProcessExited reports that a tab's process has ended, only when tmux says so: a
// question that failed is not an answer, and callers of this close the tab.
func (i *Instance) TabProcessExited(windowIdx int) bool {
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	// With the index: asked about a window that is gone, tmux answers for another.
	output, err := i.tmuxOutputOn(i.serverForWindow(windowIdx),
		"display-message", "-p", "-t", target, "#{window_index}:#{pane_dead}")
	return err == nil && strings.TrimSpace(string(output)) == fmt.Sprintf("%d:1", windowIdx)
}

// InstancesClosingTabsOnExit lists the open project's sessions that have such a tab.
func (s *Storage) InstancesClosingTabsOnExit() []*Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, _, _, err := s.loadAllWithSettingsLocked()
	if err != nil {
		return nil
	}
	var closing []*Instance
	for _, inst := range instances {
		if inst.HasTabsClosingOnExit() {
			closing = append(closing, inst)
		}
	}
	routeLoaded(closing)
	return closing
}
