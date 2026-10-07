package main

import (
	"fmt"
	"strings"
	"time"

	"asmgr-desktop/session"
)

// Frontend API for build-tool suggestions: what a tab's directory looks like
// it can run, and adding a chosen one to the saved-command library.
//
// Suggesting is all this does. Nothing here runs a command — the user adds
// what they want to the library and runs it from there, like any other saved
// command, which is also where they can edit it first.

// RunnerSuggestionInfo is one suggestion as the frontend sees it, with
// whether the library already holds it.
type RunnerSuggestionInfo struct {
	Tool    string `json:"tool"`
	Name    string `json:"name"`
	Command string `json:"command"`
	RunIn   string `json:"runIn"`
	// Added is true when a command with this text is already saved, so the
	// list can show it as added rather than offering a duplicate.
	Added bool `json:"added"`
}

// GetRunnerSuggestions returns what the given tab's directory suggests it can
// run. An empty list means nothing recognisable was found, which is the
// normal answer for most directories.
func (a *App) GetRunnerSuggestions(sessionID string, windowIdx int) ([]RunnerSuggestionInfo, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return nil, err
	}
	serverID, workDir := inst.TabPlacement(windowIdx)
	// A tab on a remote server runs its commands there, where this process
	// cannot see the files. Guessing from the local disk would offer commands
	// for the wrong machine.
	if serverID != "" {
		return nil, nil
	}

	suggestions := session.SuggestRunners(workDir)
	if len(suggestions) == 0 {
		return nil, nil
	}

	// Mark the ones already saved. Compared on the command text rather than
	// the name, since the name is the user's to change and the text is what
	// would actually be duplicated.
	saved := make(map[string]bool)
	if lib, err := a.storage.LoadCommands(); err == nil {
		for _, c := range lib.Commands {
			saved[strings.TrimSpace(c.Command)] = true
		}
	}

	out := make([]RunnerSuggestionInfo, 0, len(suggestions))
	for _, s := range suggestions {
		out = append(out, RunnerSuggestionInfo{
			Tool:    s.Tool,
			Name:    s.Name,
			Command: s.Command,
			RunIn:   s.RunIn,
			Added:   saved[strings.TrimSpace(s.Command)],
		})
	}
	return out, nil
}

// AddRunnerSuggestion saves one suggested command to the library and returns
// its new ID.
//
// The command text is taken from the request rather than looked up again:
// the user may have edited it in the dialog, and re-deriving it would
// silently discard that.
func (a *App) AddRunnerSuggestion(name, command, runIn string) (string, error) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if name == "" {
		return "", fmt.Errorf("the command needs a name")
	}
	if command == "" {
		return "", fmt.Errorf("the command is empty")
	}
	if !session.ValidRunIn(runIn) {
		runIn = session.RunInCurrent
	}

	var newID string
	err := a.storage.UpdateCommands(func(lib *session.CommandLibrary) error {
		// Adding the same suggestion twice is a no-op rather than an error:
		// the list marks what is already there, but two clicks in a row
		// should not produce two identical entries.
		for i := range lib.Commands {
			if strings.TrimSpace(lib.Commands[i].Command) == command {
				newID = lib.Commands[i].ID
				return nil
			}
		}
		taken := make(map[string]bool, len(lib.Commands))
		for _, c := range lib.Commands {
			taken[c.ID] = true
		}
		entry := session.SavedCommand{
			ID:        session.NewUniqueID("cmd", taken),
			Name:      name,
			Command:   command,
			RunIn:     runIn,
			CreatedAt: time.Now(),
		}
		if err := entry.Validate(); err != nil {
			return err
		}
		lib.Commands = append(lib.Commands, entry)
		newID = entry.ID
		return nil
	})
	if err != nil {
		return "", err
	}
	return newID, nil
}
