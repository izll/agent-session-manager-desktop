package session

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// A tab that goes away when what it was opened for has ended.
//
// The tab remembers that, rather than something in memory watching it, so that a tab
// whose command ends while the app is closed still goes the next time it runs.

// Why a tab closes itself, which also says what happens next.
const (
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

// RunUntilDoneInFreshShell runs a command in a just-opened tab so that the tab's shell
// leaves when the command ends, and says whether it was able to arrange that. In a
// shell it does not know it types the command as it is, and the tab stays.
func (i *Instance) RunUntilDoneInFreshShell(windowIdx int, text string, timeout time.Duration) (bool, error) {
	i.waitForFreshShell(windowIdx, timeout)
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
	shell := ""
	if output, err := i.tmuxOutputOn(i.serverForWindow(windowIdx),
		"display-message", "-p", "-t", target, "#{pane_current_command}"); err == nil {
		shell = strings.TrimSpace(string(output))
	}
	line, leaves := commandThatLeavesItsShell(text, shell)
	return leaves, i.SendTextToWindow(windowIdx, line, true)
}

// commandThatLeavesItsShell wraps a command so its tab's shell exits once it has ended:
// by itself, by Ctrl+C or by being stopped. A real failure keeps the shell, and with it
// the reason on the screen, which would otherwise be gone with the tab.
//
// It runs in a shell of its own, because an interactive shell drops the rest of a line
// after Ctrl+C: nothing written after the command would run. That shell ignores the
// interrupt, so it can see how the command ended, and reports only what matters. The
// parentheses and the line break keep a trailing & or comment in the command from
// touching what follows it. An alias or function from the user's own shell files is the
// price: it is not known to this shell.
func commandThatLeavesItsShell(text, shell string) (string, bool) {
	inner := ""
	switch path.Base(strings.TrimPrefix(shell, "-")) {
	case "bash", "zsh", "ksh":
		inner = path.Base(strings.TrimPrefix(shell, "-"))
	case "sh", "dash", "ash", "fish":
		inner = "sh"
	default:
		return text, false
	}
	script := "trap : INT TERM\n( " + text + "\n)\nstatus=$?\n" +
		"case $status in 0|130|143) exit 0 ;; esac\nexit $status"
	return inner + " -c '" + strings.ReplaceAll(script, "'", `'\''`) + "' && exit", true
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
