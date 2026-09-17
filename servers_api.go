package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"asmgr-desktop/remote"
	"asmgr-desktop/session"
)

// Frontend API for the remote servers sessions can run on.

// ServerInfo is one server as the frontend sees it.
//
// HasPassword rather than the password: the secret lives in the system keyring
// and never travels to the UI. The editor shows a placeholder and a "replace"
// action, which is what this flag drives.
type ServerInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	AuthMethod  string `json:"authMethod"`
	KeyPath     string `json:"keyPath"`
	JumpHostID  string `json:"jumpHostId"`
	ExtraPath   string `json:"extraPath"`
	IsDefault   bool   `json:"isDefault"`
	Order       int    `json:"order"`
	HasPassword bool   `json:"hasPassword"`
	// DisplayName is derived so the sidebar and the pickers do not each
	// reimplement the fallback to user@host.
	DisplayName string `json:"displayName"`
}

// ServerSaveRequest is what the editor sends back.
//
// Password is separate from the stored entry and is only read when the user
// typed a new one — an empty value leaves whatever is in the keyring alone, so
// editing a server's name does not wipe its password.
type ServerSaveRequest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"authMethod"`
	KeyPath    string `json:"keyPath"`
	JumpHostID string `json:"jumpHostId"`
	ExtraPath  string `json:"extraPath"`
	IsDefault  bool   `json:"isDefault"`
	Password   string `json:"password"`
}

func serverToInfo(srv *session.Server) ServerInfo {
	_, hasPassword := session.LookupServerPassword(srv.ID)
	return ServerInfo{
		ID:          srv.ID,
		Name:        srv.Name,
		Host:        srv.Host,
		Port:        srv.Port,
		User:        srv.User,
		AuthMethod:  srv.AuthMethod,
		KeyPath:     srv.KeyPath,
		JumpHostID:  srv.JumpHostID,
		ExtraPath:   srv.ExtraPath,
		IsDefault:   srv.IsDefault,
		Order:       srv.Order,
		HasPassword: hasPassword,
		DisplayName: srv.DisplayName(),
	}
}

// GetServers returns the configured servers in display order.
func (a *App) GetServers() ([]ServerInfo, error) {
	list, err := a.storage.LoadServers()
	if err != nil {
		return nil, err
	}
	out := make([]ServerInfo, 0, len(list.Servers))
	for index := range list.Servers {
		out = append(out, serverToInfo(&list.Servers[index]))
	}
	return out, nil
}

// SaveServer creates or updates one entry and returns the saved list.
func (a *App) SaveServer(req ServerSaveRequest) ([]ServerInfo, error) {
	if strings.TrimSpace(req.Host) == "" {
		return nil, fmt.Errorf("a server needs a host")
	}
	if strings.TrimSpace(req.User) == "" {
		return nil, fmt.Errorf("a server needs a user")
	}

	id := strings.TrimSpace(req.ID)
	created := id == ""
	if created {
		id = uuid.New().String()
	}

	err := a.storage.UpdateServers(func(list *session.ServerList) error {
		entry := session.Server{
			ID:         id,
			Name:       req.Name,
			Host:       req.Host,
			Port:       req.Port,
			User:       req.User,
			AuthMethod: req.AuthMethod,
			KeyPath:    req.KeyPath,
			JumpHostID: req.JumpHostID,
			ExtraPath:  req.ExtraPath,
			IsDefault:  req.IsDefault,
		}

		found := false
		for index := range list.Servers {
			if list.Servers[index].ID != id {
				// One default at a time. Cleared here as well as in the
				// storage layer, so the list the caller gets back already
				// reflects what was saved.
				if req.IsDefault {
					list.Servers[index].IsDefault = false
				}
				continue
			}
			// Carry across the fields the editor does not own: the accepted
			// host key and the installed helper version are learned by
			// connecting, and retyping a name must not discard them.
			entry.HostKey = list.Servers[index].HostKey
			entry.HelperVersion = list.Servers[index].HelperVersion
			entry.Order = list.Servers[index].Order
			list.Servers[index] = entry
			found = true
		}
		if !found {
			entry.Order = len(list.Servers)
			list.Servers = append(list.Servers, entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := a.applyServerPassword(id, req); err != nil {
		// The entry is saved either way — reporting this as a plain failure
		// would suggest nothing was written, and the user would add it twice.
		log.Printf("[SaveServer] server=%s saved, but the password was not stored: %v", id, err)
		return nil, err
	}

	return a.GetServers()
}

// applyServerPassword decides what happens to the secret behind an edit.
func (a *App) applyServerPassword(id string, req ServerSaveRequest) error {
	if req.AuthMethod != session.AuthPassword {
		// Switching away from password authentication leaves a secret behind
		// that nothing will ever use or clean up.
		session.ForgetServerPassword(id)
		return nil
	}
	if req.Password == "" {
		// Nothing typed: keep whatever is already held.
		return nil
	}
	if err := session.StoreServerPassword(id, req.Password); err != nil {
		// No keyring on this machine. Keep it for this run so the user can
		// work, and let the caller say so.
		session.RememberServerPasswordForSession(id, req.Password)
		return err
	}
	return nil
}

// DeleteServer removes an entry and its stored password.
func (a *App) DeleteServer(id string) ([]ServerInfo, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("no server given")
	}

	// A session pointing at a deleted server would have nowhere to run and no
	// way to say why, so the deletion is refused while one exists.
	instances, _, _, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.ServerID == id {
			return nil, fmt.Errorf("this server still runs sessions (%q) — move or delete them first",
				inst.Name)
		}
	}

	err = a.storage.UpdateServers(func(list *session.ServerList) error {
		kept := list.Servers[:0]
		for _, srv := range list.Servers {
			if srv.ID == id {
				continue
			}
			// A server that jumped through the deleted one would fail
			// validation, so the reference is dropped with it.
			if srv.JumpHostID == id {
				srv.JumpHostID = ""
			}
			kept = append(kept, srv)
		}
		list.Servers = kept
		return nil
	})
	if err != nil {
		return nil, err
	}

	session.ForgetServerPassword(id)
	return a.GetServers()
}

// ReorderServers applies the order the user arranged.
func (a *App) ReorderServers(ids []string) ([]ServerInfo, error) {
	err := a.storage.UpdateServers(func(list *session.ServerList) error {
		position := make(map[string]int, len(ids))
		for index, id := range ids {
			position[id] = index
		}
		for index := range list.Servers {
			if at, known := position[list.Servers[index].ID]; known {
				list.Servers[index].Order = at
				continue
			}
			// An entry the caller did not mention goes to the end rather than
			// to the front, which is where a zero would put it.
			list.Servers[index].Order = len(ids) + index
		}
		session.SortServers(list.Servers)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a.GetServers()
}

// KeyringAvailable reports whether passwords can be remembered on this machine,
// so the editor can say so before the user types one.
func (a *App) KeyringAvailable() bool {
	return session.KeyringAvailable()
}

// serverDisplayName names a session's server for the sidebar.
//
// Resolved here rather than in the frontend so a session carries the name it
// should show even when the server list has not been loaded yet — and empty
// for a local session, which is what the sidebar treats as "no marker".
func (a *App) serverDisplayName(serverID string) string {
	if serverID == "" {
		return ""
	}
	server, err := a.storage.FindServer(serverID)
	if err != nil {
		// A session pointing at a server that no longer exists: shown as
		// unknown rather than as local, because it will not start here either.
		return "?"
	}
	return server.DisplayName()
}

// SSHConfigHostInfo is one entry from ~/.ssh/config, offered as a starting
// point when adding a server.
type SSHConfigHostInfo struct {
	Alias    string `json:"alias"`
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	KeyPath  string `json:"keyPath"`
	// AlreadyAdded marks an entry that is already in the server list, so the
	// picker can say so instead of letting the user add it twice.
	AlreadyAdded bool `json:"alreadyAdded"`
}

// GetSSHConfigHosts lists the machines described in the user's SSH
// configuration.
//
// Read only, and only when asked. The file is where someone who uses SSH daily
// has already written down every host, port, user and key — retyping all of
// that into another form is the small friction that makes a feature feel like
// work.
func (a *App) GetSSHConfigHosts() ([]SSHConfigHostInfo, error) {
	hosts, err := remote.ReadSSHConfig()
	if err != nil {
		// A configuration that cannot be read is not a failure worth stopping
		// for: the user can still type the details in.
		log.Printf("[GetSSHConfigHosts] could not read the SSH configuration: %v", err)
		return nil, nil
	}

	existing, err := a.storage.LoadServers()
	if err != nil {
		return nil, err
	}

	out := make([]SSHConfigHostInfo, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, SSHConfigHostInfo{
			Alias:        host.Alias,
			HostName:     host.HostName,
			User:         host.User,
			Port:         host.Port,
			KeyPath:      host.KeyPath,
			AlreadyAdded: serverListHas(existing.Servers, host),
		})
	}
	return out, nil
}

// serverListHas reports whether a config entry is already in the server list.
//
// Matched on host and user rather than on the alias: the alias is a local
// nickname, and the same machine may well have been added under a different
// one.
func serverListHas(servers []session.Server, host remote.SSHConfigHost) bool {
	wantPort := host.Port
	if wantPort == 0 {
		wantPort = 22
	}
	for _, server := range servers {
		serverPort := server.Port
		if serverPort == 0 {
			serverPort = 22
		}
		if server.Host == host.HostName && server.User == host.User && serverPort == wantPort {
			return true
		}
	}
	return false
}
