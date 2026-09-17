package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
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

// RemoteDirEntryInfo is one item in a directory on a server.
type RemoteDirEntryInfo struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// RemoteDirListing is one directory as the browser sees it.
type RemoteDirListing struct {
	// Path is the directory that was actually read, resolved: the caller may
	// have asked for "~" or a relative path, and the field the user edits
	// should end up holding something they can read back.
	Path string `json:"path"`
	// Parent is empty at the root, which is how the browser knows to stop
	// offering a way up.
	Parent  string               `json:"parent"`
	Entries []RemoteDirEntryInfo `json:"entries"`
}

// ListServerDirectory reads a directory on a server, for choosing a working
// directory without leaving the app.
//
// The native folder picker cannot serve this: it opens on this computer, and
// the path a remote session needs is one that exists on the server. A path
// typed by hand works too — this only saves the typing.
func (a *App) ListServerDirectory(serverID, path string) (*RemoteDirListing, error) {
	connection, err := a.connectionFor(serverID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()

	// Resolved on the server: "~" means the server's home, and a relative path
	// means relative to it. Doing this here would use the wrong home.
	target := strings.TrimSpace(path)
	if target == "" {
		target = "~"
	}

	// One command: the resolved path, then the entries. Two round trips to
	// open one directory is a visible pause while browsing.
	// Sorted here rather than by the shell: "sort -t'\t'" does not mean a tab
	// to a shell, it means the two characters, and sort refuses it outright —
	// "multi-character tab". Ordering a few dozen names in Go costs nothing.
	script := fmt.Sprintf(
		`cd %s 2>/dev/null || cd ~ ; pwd; find . -maxdepth 1 -mindepth 1 -printf '%%y\t%%s\t%%f\n' 2>/dev/null`,
		shellQuoteForServer(target))

	result, err := connection.helper.Run(ctx, script)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 && result.Output == "" {
		return nil, fmt.Errorf("could not read %s: %s", path, strings.TrimSpace(result.Stderr))
	}

	lines := strings.Split(strings.TrimRight(result.Output, "\n"), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("could not read %s", path)
	}

	listing := &RemoteDirListing{Path: strings.TrimSpace(lines[0])}
	if listing.Path != "/" {
		if slash := strings.LastIndex(listing.Path, "/"); slash >= 0 {
			listing.Parent = listing.Path[:slash]
			if listing.Parent == "" {
				listing.Parent = "/"
			}
		}
	}

	for _, line := range lines[1:] {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		// Hidden entries are skipped: a home directory is mostly dotfiles, and
		// none of them is a project.
		if strings.HasPrefix(fields[2], ".") {
			continue
		}
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		listing.Entries = append(listing.Entries, RemoteDirEntryInfo{
			Name:  fields[2],
			IsDir: fields[0] == "d",
			Size:  size,
		})
	}

	// Directories first, then by name: this is a picker for a working
	// directory, and the folders are what is being looked for.
	sort.SliceStable(listing.Entries, func(left, right int) bool {
		if listing.Entries[left].IsDir != listing.Entries[right].IsDir {
			return listing.Entries[left].IsDir
		}
		return strings.ToLower(listing.Entries[left].Name) <
			strings.ToLower(listing.Entries[right].Name)
	})
	return listing, nil
}

// shellQuoteForServer quotes a path for a command run on a server.
//
// Not through the executor's quoting: this value goes inside a script, so it
// needs quoting of its own. A "~" is left bare on purpose — quoted, the shell
// would take it for a directory with that name rather than the home it means.
func shellQuoteForServer(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
