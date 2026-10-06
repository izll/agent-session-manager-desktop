package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"asmgr-desktop/session"
)

// Frontend API for the saved-command library: the list, editing it, and
// sending an entry to a terminal with its placeholders filled in.

// SavedCommandInfo is one command as the frontend sees it.
type SavedCommandInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
	GroupID     string `json:"groupId"`
	SendEnter   bool   `json:"sendEnter"`
	// InBackground runs the command in a new terminal tab, left unselected.
	InBackground bool `json:"inBackground"`
	UseCount     int  `json:"useCount"`
	// Placeholders is derived, not stored: the UI asks for these before
	// sending, and deriving it here keeps one definition of what counts.
	// Each carries the default written as {name:default}, if any.
	Placeholders []session.Placeholder `json:"placeholders"`
}

// CommandGroupInfo is one group as the frontend sees it.
type CommandGroupInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Order int    `json:"order"`
}

// CommandLibraryInfo is the whole library, ready for display.
type CommandLibraryInfo struct {
	Commands []SavedCommandInfo `json:"commands"`
	Groups   []CommandGroupInfo `json:"groups"`
}

// GetCommands returns the saved commands, ordered for the picker.
func (a *App) GetCommands() (*CommandLibraryInfo, error) {
	lib, err := a.storage.LoadCommands()
	if err != nil {
		return nil, err
	}
	session.SortCommands(lib.Commands)
	session.SortGroups(lib.Groups)

	out := &CommandLibraryInfo{
		Commands: make([]SavedCommandInfo, 0, len(lib.Commands)),
		Groups:   make([]CommandGroupInfo, 0, len(lib.Groups)),
	}
	for _, c := range lib.Commands {
		out.Commands = append(out.Commands, SavedCommandInfo{
			ID:           c.ID,
			Name:         c.Name,
			Command:      c.Command,
			Description:  c.Description,
			GroupID:      c.GroupID,
			SendEnter:    c.SendEnter,
			InBackground: c.InBackground,
			UseCount:     c.UseCount,
			Placeholders: session.PlaceholderList(c.Command),
		})
	}
	for _, g := range lib.Groups {
		out.Groups = append(out.Groups, CommandGroupInfo{ID: g.ID, Name: g.Name, Order: g.Order})
	}
	return out, nil
}

// SaveCommand adds or updates one command. An empty ID creates a new entry.
func (a *App) SaveCommand(id, name, command, description, groupID string, sendEnter, inBackground bool) (string, error) {
	entry := session.SavedCommand{
		ID:           id,
		Name:         strings.TrimSpace(name),
		Command:      strings.TrimSpace(command),
		Description:  strings.TrimSpace(description),
		GroupID:      groupID,
		SendEnter:    sendEnter,
		InBackground: inBackground,
	}
	if err := entry.Validate(); err != nil {
		return "", err
	}

	err := a.storage.UpdateCommands(func(lib *session.CommandLibrary) error {
		if id == "" {
			taken := make(map[string]bool, len(lib.Commands))
			for _, c := range lib.Commands {
				taken[c.ID] = true
			}
			entry.ID = session.NewUniqueID("cmd", taken)
			entry.CreatedAt = time.Now()
			lib.Commands = append(lib.Commands, entry)
			return nil
		}
		for i := range lib.Commands {
			if lib.Commands[i].ID == id {
				// Usage statistics belong to the command, not to this edit.
				entry.CreatedAt = lib.Commands[i].CreatedAt
				entry.UsedAt = lib.Commands[i].UsedAt
				entry.UseCount = lib.Commands[i].UseCount
				lib.Commands[i] = entry
				return nil
			}
		}
		return fmt.Errorf("no such command")
	})
	if err != nil {
		return "", err
	}
	return entry.ID, nil
}

// DeleteCommand removes one command.
func (a *App) DeleteCommand(id string) error {
	return a.storage.UpdateCommands(func(lib *session.CommandLibrary) error {
		out := lib.Commands[:0]
		removed := false
		for _, c := range lib.Commands {
			if c.ID == id {
				removed = true
				continue
			}
			out = append(out, c)
		}
		if !removed {
			return fmt.Errorf("no such command")
		}
		lib.Commands = out
		return nil
	})
}

// SaveCommandGroup adds or renames a group. An empty ID creates one.
func (a *App) SaveCommandGroup(id, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("the group needs a name")
	}
	resultID := id
	err := a.storage.UpdateCommands(func(lib *session.CommandLibrary) error {
		if id == "" {
			g := session.CommandGroup{
				ID:    session.NewUniqueID("cgrp", takenGroupIDs(lib.Groups)),
				Name:  name,
				Order: len(lib.Groups),
			}
			resultID = g.ID
			lib.Groups = append(lib.Groups, g)
			return nil
		}
		for i := range lib.Groups {
			if lib.Groups[i].ID == id {
				lib.Groups[i].Name = name
				return nil
			}
		}
		return fmt.Errorf("no such group")
	})
	if err != nil {
		return "", err
	}
	return resultID, nil
}

// DeleteCommandGroup removes a group. Its commands are kept and become
// ungrouped — deleting a folder should not delete what is in it.
func (a *App) DeleteCommandGroup(id string) error {
	return a.storage.UpdateCommands(func(lib *session.CommandLibrary) error {
		out := lib.Groups[:0]
		removed := false
		for _, g := range lib.Groups {
			if g.ID == id {
				removed = true
				continue
			}
			out = append(out, g)
		}
		if !removed {
			return fmt.Errorf("no such group")
		}
		lib.Groups = out
		for i := range lib.Commands {
			if lib.Commands[i].GroupID == id {
				lib.Commands[i].GroupID = ""
			}
		}
		return nil
	})
}

// RunCommand types a saved command into a session window, substituting the
// given placeholder values, and records that it was used.
//
// A command marked to run in the background goes to a new terminal tab
// instead, and the index of that tab is returned so the caller can show it;
// otherwise the result is -1.
func (a *App) RunCommand(id, sessionID string, windowIdx int, values map[string]string, expectedProjectID string) (int, error) {
	lib, err := a.storage.LoadCommands()
	if err != nil {
		return -1, err
	}
	var found *session.SavedCommand
	for i := range lib.Commands {
		if lib.Commands[i].ID == id {
			found = &lib.Commands[i]
			break
		}
	}
	if found == nil {
		return -1, fmt.Errorf("no such command")
	}

	text := session.Expand(found.Command, values)
	// Refuse rather than send a half-filled command: an unanswered {path} in
	// an rm or a deploy is not something to paste into a live shell.
	if missing := session.Placeholders(text); len(missing) > 0 {
		return -1, fmt.Errorf("still missing a value for: %s", strings.Join(missing, ", "))
	}

	newIdx := -1
	if found.InBackground {
		newIdx, err = a.runCommandInNewTab(sessionID, windowIdx, found.Name, text, expectedProjectID)
	} else {
		err = a.typeCommandIntoTab(sessionID, windowIdx, text, found.SendEnter, expectedProjectID)
	}
	if err != nil {
		return -1, err
	}
	a.recordCommandUse(id)
	return newIdx, nil
}

// typeCommandIntoTab sends the text to an existing tab, as if typed there.
func (a *App) typeCommandIntoTab(sessionID string, windowIdx int, text string, pressEnter bool, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()

	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	return inst.SendTextToWindow(windowIdx, text, pressEnter)
}

// runCommandInNewTab opens a terminal tab beside the one the command was
// picked from and runs the command in its shell.
//
// The tab goes where the picking tab is — the same machine, the same working
// directory — since that is where the user was when they chose the command.
// It is typed into a shell rather than started as the tab's own process, so
// pipes and variables mean what they mean at a prompt, and the output and the
// prompt are still there once the command ends.
func (a *App) runCommandInNewTab(sessionID string, fromWindow int, name, text, expectedProjectID string) (int, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return -1, err
	}
	serverID, workDir := inst.TabPlacement(fromWindow)

	// Connect before taking the mutation lock, as CreateTabWithWorktree does:
	// a dial can take long, and nothing about it needs the lock.
	var tabConnection *serverConnection
	if serverID != "" && serverID != inst.ServerID {
		connection, connErr := a.connectionFor(serverID)
		if connErr != nil {
			return -1, connErr
		}
		tabConnection = connection
	}

	tabInst, newIdx, err := a.openBackgroundTab(inst.ID, serverID, tabConnection, name, workDir, expectedProjectID)
	if err != nil {
		return -1, err
	}
	// Outside the mutation lock: waiting for the shell can take a few seconds,
	// and nothing else in the app should wait with it. The tab is kept if the
	// command cannot be typed into it — it exists, it is saved, and an empty
	// shell says more than a tab that vanished.
	if err := tabInst.TypeIntoFreshShell(newIdx, text, backgroundShellTimeout); err != nil {
		return newIdx, err
	}
	return newIdx, nil
}

// backgroundShellTimeout bounds the wait for a new tab's shell to be ready.
const backgroundShellTimeout = 10 * time.Second

// openBackgroundTab creates and saves an unselected terminal tab. It returns
// the session as it holds the new tab, the one to type into it through.
func (a *App) openBackgroundTab(sessionID, serverID string, connection *serverConnection, name, workDir, expectedProjectID string) (*session.Instance, int, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, -1, err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, -1, err
	}
	if connection != nil {
		session.SetTabExecutor(inst.ID, serverID, connection.executor)
	}

	newIdx, err := inst.NewBackgroundWindowOn(serverID, name, workDir)
	if err != nil {
		return nil, -1, err
	}
	if err := persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.DeleteWindow(newIdx) },
	); err != nil {
		return nil, -1, err
	}
	return inst, newIdx, nil
}

// recordCommandUse bumps the statistics that order the picker.
func (a *App) recordCommandUse(id string) {
	// A failed statistics write must not look like a failed command: the
	// command already ran.
	if serr := a.storage.UpdateCommands(func(latest *session.CommandLibrary) error {
		for i := range latest.Commands {
			if latest.Commands[i].ID == id {
				latest.Commands[i].UsedAt = time.Now()
				latest.Commands[i].UseCount++
				return nil
			}
		}
		return fmt.Errorf("no such command")
	}); serr != nil {
		log.Printf("[commands] could not record usage of %s: %v", id, serr)
	}
}

// takenGroupIDs collects the command-group IDs already in use.
func takenGroupIDs(groups []session.CommandGroup) map[string]bool {
	taken := make(map[string]bool, len(groups))
	for _, g := range groups {
		taken[g.ID] = true
	}
	return taken
}
