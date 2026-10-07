package main

import (
	"fmt"
	"log"
	"strings"

	"asmgr-desktop/session"
)

// Frontend API for runner tabs: a tab that is the home of one build or
// server command, started and stopped from the tab bar.

// SetTabRunCommand makes a tab a runner, or stops it being one when the
// command is empty.
//
// Only a followed tab can be a runner: the session's own window is the
// agent's, and giving it a second command to own would put two things that
// both want the pane in one place.
func (a *App) SetTabRunCommand(sessionID string, windowIdx int, command, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()

	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if windowIdx == inst.GetMainWindowIndex() {
		return fmt.Errorf("the session's own tab cannot run a command of its own")
	}
	command = strings.TrimSpace(command)
	found := false
	for at := range inst.FollowedWindows {
		if inst.FollowedWindows[at].Index == windowIdx {
			inst.FollowedWindows[at].RunCommand = command
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no such tab")
	}
	return a.storage.UpdateInstance(inst)
}

// StartTabCommand runs a runner tab's command.
func (a *App) StartTabCommand(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()

	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	if err := inst.StartRunnerCommand(a.ctx, windowIdx); err != nil {
		return err
	}
	// Starting clears the parked flag: the tab is live again, and a stored
	// "stopped" would keep the bar showing it as not running.
	for at := range inst.FollowedWindows {
		if inst.FollowedWindows[at].Index == windowIdx {
			inst.FollowedWindows[at].Stopped = false
			break
		}
	}
	return a.storage.UpdateInstance(inst)
}

// StopTabCommand stops a runner tab's command: Ctrl+C, then the pane for
// whatever ignored it.
func (a *App) StopTabCommand(sessionID string, windowIdx int, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()

	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	gentle, err := inst.StopRunnerCommand(a.ctx, windowIdx)
	if err != nil {
		return err
	}
	if !gentle {
		// Worth a line in the log: a command that needs the hard stop is
		// one that ignored an interrupt, which is a property of that
		// command rather than a one-off.
		log.Printf("[runner] %s tab %d did not stop on its own; the pane was taken",
			sessionID, windowIdx)
	}
	// Parked, not deleted: the tab keeps its command and its scrollback, and
	// ▶ starts it again.
	for at := range inst.FollowedWindows {
		if inst.FollowedWindows[at].Index == windowIdx {
			inst.FollowedWindows[at].Stopped = true
			break
		}
	}
	return a.storage.UpdateInstance(inst)
}

// RunningTabCommands lists the runner tabs whose command is still going, for
// the quit confirmation.
//
// Across every session in the active project, since quitting is not about
// the one session being looked at.
type RunningTabCommand struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	WindowIdx   int    `json:"windowIdx"`
	TabName     string `json:"tabName"`
	Command     string `json:"command"`
}

// RunningTabCommands returns what is still running, newest session first.
//
// Read from the stored flag and the live pane together: a tab marked running
// whose pane has exited is not running, and asking about it would be a
// warning for nothing.
func (a *App) RunningTabCommands() []RunningTabCommand {
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return nil
	}
	var out []RunningTabCommand
	for _, inst := range instances {
		if inst == nil || inst.Status != session.StatusRunning {
			continue
		}
		for _, fw := range inst.FollowedWindows {
			if strings.TrimSpace(fw.RunCommand) == "" || fw.Stopped {
				continue
			}
			if !inst.PaneRunning(fw.Index) {
				continue
			}
			name := fw.Name
			if name == "" {
				name = string(fw.Agent)
			}
			out = append(out, RunningTabCommand{
				SessionID:   inst.ID,
				SessionName: inst.Name,
				WindowIdx:   fw.Index,
				TabName:     name,
				Command:     fw.RunCommand,
			})
		}
	}
	return out
}
