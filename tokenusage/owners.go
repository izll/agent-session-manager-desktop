package tokenusage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Owner says which of a machine's sessions holds a conversation or a directory.
//
// The names live on the machine the sessions run on. A desktop reading a server's
// logs knows nothing about them, and would show a conversation by its directory
// even when it is a named tab of a named session over there.
type Owner struct {
	Conversation string `json:"c,omitempty"`
	Directory    string `json:"d,omitempty"`
	ProjectID    string `json:"p,omitempty"`
	ProjectName  string `json:"q,omitempty"`
	SessionID    string `json:"s"`
	SessionName  string `json:"n"`
	TabName      string `json:"t,omitempty"`
}

// DefaultConfigDir is where the app keeps its sessions on this machine.
func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "agent-session-manager-desktop"), nil
}

type storedWindow struct {
	Name            string `json:"name"`
	ResumeSessionID string `json:"resume_session_id"`
	WorkDir         string `json:"work_dir"`
	WorktreeDir     string `json:"worktree_dir"`
	ServerID        string `json:"server_id"`
}

type storedInstance struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Path            string         `json:"path"`
	Agent           string         `json:"agent"`
	MainWindowName  string         `json:"main_window_name"`
	ResumeSessionID string         `json:"resume_session_id"`
	ServerID        string         `json:"server_id"`
	FollowedWindows []storedWindow `json:"followed_windows"`
}

// ReadOwners reads the sessions kept in a config directory. No app on the machine
// means no sessions, which is no error. Only the names and ids are taken, however
// much else those files hold.
//
// A session or tab placed on another machine is left out: its logs are that
// machine's, and it has a list of its own to say so.
func ReadOwners(configDir string) ([]Owner, error) {
	type project struct{ id, name string }
	projects := []project{{}}

	var catalog struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if data, err := os.ReadFile(filepath.Join(configDir, "projects.json")); err == nil {
		if json.Unmarshal(data, &catalog) == nil {
			for _, entry := range catalog.Projects {
				if entry.ID != "" && filepath.Base(entry.ID) == entry.ID {
					projects = append(projects, project{entry.ID, entry.Name})
				}
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	var owners []Owner
	for _, entry := range projects {
		path := filepath.Join(configDir, "sessions.json")
		if entry.id != "" {
			path = filepath.Join(configDir, "projects", entry.id, "sessions.json")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var stored struct {
			Instances []storedInstance `json:"instances"`
		}
		// One damaged project must not cost the names of the others.
		if json.Unmarshal(data, &stored) != nil {
			continue
		}
		for _, instance := range stored.Instances {
			base := Owner{ProjectID: entry.id, ProjectName: entry.name, SessionID: instance.ID, SessionName: instance.Name}
			main := base
			main.TabName = instance.mainTabName()
			owners = appendOwner(owners, main, instance.ResumeSessionID, instance.Path, instance.ServerID == "")
			for _, window := range instance.FollowedWindows {
				tab := base
				tab.TabName = window.Name
				here := window.ServerID == "" && instance.ServerID == ""
				directory := window.WorkDir
				if directory == "" {
					directory = instance.Path
				}
				owners = appendOwner(owners, tab, window.ResumeSessionID, directory, here)
				if window.WorktreeDir != "" {
					owners = appendOwner(owners, tab, "", window.WorktreeDir, here)
				}
			}
		}
	}
	return owners, nil
}

// mainTabName is what the app calls a session's own tab, as Instance.WindowName
// does on the machine that runs it.
func (i storedInstance) mainTabName() string {
	if i.MainWindowName != "" {
		return i.MainWindowName
	}
	if i.Agent != "" {
		return i.Agent
	}
	return AgentClaude
}

func appendOwner(owners []Owner, owner Owner, conversation, directory string, here bool) []Owner {
	if !here || (conversation == "" && directory == "") {
		return owners
	}
	owner.Conversation = conversation
	owner.Directory = directory
	return append(owners, owner)
}
