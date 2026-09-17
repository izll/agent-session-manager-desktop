package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Remote servers: machines where sessions run instead of on this computer, so
// that the agents keep working after the laptop is closed.
//
// The list is global rather than per-project. A server is a machine the user
// has, not a property of one piece of work — and a project that only existed
// on one machine would make "the same project, sometimes local, sometimes
// remote" impossible, which is exactly how this gets used.
//
// It is also kept out of Settings on purpose. Settings is per-project and is
// copied into every backup; a server list carries host names and usernames,
// and the one secret involved — the password — is deliberately not here at all
// (see the note on AuthPassword). Settings.AnthropicAPIKey documents what that
// costs: recovery.go has to strip it from backups by hand.

const (
	serverListMaxBytes = 1 << 20
	serverListMaxItems = 200

	// AuthAgent uses whatever keys ssh-agent already holds. Tried first
	// because most people who use SSH daily are already set up this way, and
	// it needs neither a key path nor a password.
	AuthAgent = "agent"
	// AuthKey reads a private key file.
	AuthKey = "key"
	// AuthPassword asks the system keyring, and the user when there is none.
	AuthPassword = "password"
)

// Server is one remote machine sessions can run on.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	// Port is 0 for the default 22, so an untouched entry round-trips without
	// writing a number nobody chose.
	Port int    `json:"port,omitempty"`
	User string `json:"user"`
	// AuthMethod is AuthAgent, AuthKey or AuthPassword.
	AuthMethod string `json:"auth_method"`
	// KeyPath is the private key for AuthKey. A key protected by a passphrase
	// is asked for at connection time and never stored.
	KeyPath string `json:"key_path,omitempty"`
	// JumpHostID is another server to connect through. Empty for a direct
	// connection.
	JumpHostID string `json:"jump_host_id,omitempty"`
	// ExtraPath is prepended to PATH on the server.
	//
	// An SSH command runs a non-interactive shell, which does not read
	// .bashrc — so an agent installed in ~/.local/bin or through nvm is on the
	// PATH when the user logs in and missing when we run it. This is the field
	// that fixes that without asking anyone to edit their shell profile.
	ExtraPath string `json:"extra_path,omitempty"`
	// HostKey is the accepted host key fingerprint, recorded on first
	// connection. A connection whose key no longer matches is refused rather
	// than reported quietly: that is what a machine-in-the-middle looks like.
	HostKey string `json:"host_key,omitempty"`
	// IsDefault preselects this server for new sessions. At most one entry
	// holds it; SaveServer enforces that rather than trusting the file.
	IsDefault bool `json:"is_default,omitempty"`
	// Order is the position in the list, as arranged by the user.
	Order int `json:"order"`
	// HelperVersion caches the protocol version of the helper last installed
	// there, so a connection that needs no upgrade costs no round trip.
	HelperVersion string `json:"helper_version,omitempty"`
}

// ServerList is the stored file.
type ServerList struct {
	Servers []Server `json:"servers"`
}

// Address returns host:port, with the default port filled in.
func (srv *Server) Address() string {
	port := srv.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", srv.Host, port)
}

// DisplayName is what the sidebar and the pickers show.
func (srv *Server) DisplayName() string {
	if name := strings.TrimSpace(srv.Name); name != "" {
		return name
	}
	if srv.User != "" {
		return srv.User + "@" + srv.Host
	}
	return srv.Host
}

// Validate reports whether the entry can be connected to at all.
func (srv *Server) Validate() error {
	if strings.TrimSpace(srv.ID) == "" {
		return fmt.Errorf("server has no id")
	}
	if strings.TrimSpace(srv.Host) == "" {
		return fmt.Errorf("server %q has no host", srv.DisplayName())
	}
	if strings.TrimSpace(srv.User) == "" {
		return fmt.Errorf("server %q has no user", srv.DisplayName())
	}
	if srv.Port < 0 || srv.Port > 65535 {
		return fmt.Errorf("server %q has an invalid port %d", srv.DisplayName(), srv.Port)
	}
	switch srv.AuthMethod {
	case AuthAgent, AuthPassword:
	case AuthKey:
		if strings.TrimSpace(srv.KeyPath) == "" {
			return fmt.Errorf("server %q uses a key file but names none", srv.DisplayName())
		}
	default:
		return fmt.Errorf("server %q has an unknown authentication method %q",
			srv.DisplayName(), srv.AuthMethod)
	}
	return nil
}

func (s *Storage) serversPath() string {
	return filepath.Join(s.configDir, "servers.json")
}

func (s *Storage) serversLockPath() string {
	return filepath.Join(s.configDir, "servers.lock")
}

// LoadServers returns the server list, or an empty one when nothing is saved.
func (s *Storage) LoadServers() (*ServerList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadServersLocked()
}

func (s *Storage) loadServersLocked() (*ServerList, error) {
	data, err := readFileAtMost(s.serversPath(), serverListMaxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServerList{}, nil
		}
		return nil, err
	}
	var list ServerList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("the server list is unreadable: %w", err)
	}
	if err := validateServerList(&list); err != nil {
		return nil, fmt.Errorf("the server list is invalid: %w", err)
	}
	SortServers(list.Servers)
	return &list, nil
}

// validateServerList rejects a file that would misbehave rather than loading
// half of it: a duplicate id would make two entries indistinguishable to every
// session that names one.
func validateServerList(list *ServerList) error {
	if list == nil {
		return nil
	}
	if len(list.Servers) > serverListMaxItems {
		return fmt.Errorf("server list exceeds the item limit")
	}
	seen := make(map[string]struct{}, len(list.Servers))
	for index := range list.Servers {
		srv := &list.Servers[index]
		if err := srv.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[srv.ID]; duplicate {
			return fmt.Errorf("duplicate server id %q", srv.ID)
		}
		seen[srv.ID] = struct{}{}
	}
	// A jump host has to exist, and the chain has to end. Without this check a
	// server pointing at itself would hang the connection rather than fail.
	for index := range list.Servers {
		if err := checkJumpChain(list.Servers, index); err != nil {
			return err
		}
	}
	return nil
}

func checkJumpChain(servers []Server, start int) error {
	byID := make(map[string]int, len(servers))
	for index := range servers {
		byID[servers[index].ID] = index
	}
	seen := make(map[string]struct{})
	current := start
	for {
		jump := strings.TrimSpace(servers[current].JumpHostID)
		if jump == "" {
			return nil
		}
		if jump == servers[current].ID {
			return fmt.Errorf("server %q jumps through itself", servers[current].DisplayName())
		}
		if _, visited := seen[jump]; visited {
			return fmt.Errorf("server %q is part of a jump-host loop", servers[start].DisplayName())
		}
		seen[jump] = struct{}{}
		next, exists := byID[jump]
		if !exists {
			return fmt.Errorf("server %q jumps through a server that no longer exists",
				servers[current].DisplayName())
		}
		current = next
	}
}

// SortServers orders the list the way it is shown: by the user's arrangement,
// then by name so a list that was never arranged is still stable.
func SortServers(servers []Server) {
	sort.SliceStable(servers, func(left, right int) bool {
		if servers[left].Order != servers[right].Order {
			return servers[left].Order < servers[right].Order
		}
		return strings.ToLower(servers[left].DisplayName()) <
			strings.ToLower(servers[right].DisplayName())
	})
}

// SaveServers replaces the whole list.
func (s *Storage) SaveServers(list *ServerList) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withCrossProcessFileLock(s.serversLockPath(), func() error {
		return s.saveServersLocked(list)
	})
}

func (s *Storage) saveServersLocked(list *ServerList) error {
	if list == nil {
		list = &ServerList{}
	}
	normaliseServers(list)
	if err := validateServerList(list); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return err
	}
	// 0600: the list names hosts and usernames. No password is in it, but
	// there is no reason for another account on this machine to read it.
	return writeFileAtomic(s.serversPath(), data, 0600)
}

// normaliseServers settles what the UI should not have to: one default at
// most, and Order values that match the order of the slice.
func normaliseServers(list *ServerList) {
	defaultSeen := false
	for index := range list.Servers {
		srv := &list.Servers[index]
		srv.Name = strings.TrimSpace(srv.Name)
		srv.Host = strings.TrimSpace(srv.Host)
		srv.User = strings.TrimSpace(srv.User)
		srv.KeyPath = strings.TrimSpace(srv.KeyPath)
		srv.ExtraPath = strings.TrimSpace(srv.ExtraPath)
		if srv.AuthMethod == "" {
			srv.AuthMethod = AuthAgent
		}
		if srv.IsDefault {
			if defaultSeen {
				srv.IsDefault = false
			}
			defaultSeen = true
		}
		srv.Order = index
	}
}

// UpdateServers runs a complete load/change/save cycle under the lock.
//
// Editing one entry must go through here rather than a separate load and save:
// two Wails calls arriving together would otherwise each save a copy derived
// from the same older list, and the first edit would disappear.
func (s *Storage) UpdateServers(change func(*ServerList) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withCrossProcessFileLock(s.serversLockPath(), func() error {
		list, err := s.loadServersLocked()
		if err != nil {
			return err
		}
		if err := change(list); err != nil {
			return err
		}
		return s.saveServersLocked(list)
	})
}

// FindServer returns the entry with this id.
func (s *Storage) FindServer(id string) (*Server, error) {
	list, err := s.LoadServers()
	if err != nil {
		return nil, err
	}
	for index := range list.Servers {
		if list.Servers[index].ID == id {
			found := list.Servers[index]
			return &found, nil
		}
	}
	return nil, fmt.Errorf("server %q not found", id)
}
