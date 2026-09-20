package main

import (
	"context"
	"errors"
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
		return nil, fmt.Errorf("error.serverNeedsHost")
	}
	if strings.TrimSpace(req.User) == "" {
		return nil, fmt.Errorf("error.serverNeedsUser")
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
		return nil, fmt.Errorf("error.noServerGiven")
	}

	// A session pointing at a deleted server would have nowhere to run and no
	// way to say why, so the deletion is refused while one exists.
	instances, _, _, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.ServerID == id {
			return nil, fmt.Errorf("error.serverStillRunsSessions|%s", inst.Name)
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
		return nil, fmt.Errorf("error.couldNotReadDirectory|%s|%s", path, firstMessageLine(result.Stderr))
	}

	lines := strings.Split(strings.TrimRight(result.Output, "\n"), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("error.couldNotReadDirectory|%s|", path)
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

// CreateServerDirectory makes one new directory on a server and returns the
// refreshed listing of where it was made.
//
// Offered because the alternative is leaving the app: a tab on a server often
// wants a directory that does not exist there yet, and the picker would
// otherwise be able to show that it is missing but not to fix it.
//
// One level only, inside the directory being browsed. A name is a name here,
// not a path — see validateNewDirectoryName.
func (a *App) CreateServerDirectory(serverID, parent, name string) (*RemoteDirListing, error) {
	cleanName, err := validateNewDirectoryName(name)
	if err != nil {
		return nil, err
	}

	connection, err := a.connectionFor(serverID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()

	target := strings.TrimSpace(parent)
	if target == "" {
		target = "~"
	}

	// mkdir without -p: the parent is a directory the user is looking at, and
	// -p would silently succeed on a name that already exists, reporting a
	// creation that did not happen.
	script := fmt.Sprintf("cd %s && mkdir %s",
		shellQuoteForServer(target), shellQuoteForServer(cleanName))

	result, err := connection.helper.Run(ctx, script)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = strings.TrimSpace(result.Output)
		}
		if message == "" {
			message = fmt.Sprintf("could not create %s", cleanName)
		}
		return nil, errors.New(message)
	}

	// The listing returned is of the new directory itself, not of where it was
	// made: someone who creates a folder while choosing one is going to work in
	// it, and stopping outside it would mean a second click to step in.
	//
	// Built by listing the parent first, so the path comes back resolved by the
	// server — "~/x" and a relative parent both become something absolute that
	// the picker can then navigate from.
	parentListing, err := a.ListServerDirectory(serverID, target)
	if err != nil {
		return nil, err
	}
	base := parentListing.Path
	if base == "/" {
		base = ""
	}
	return a.ListServerDirectory(serverID, base+"/"+cleanName)
}

// firstMessageLine reduces a command's error output to one line.
//
// A message carried in a translation key cannot contain the separator, and a
// multi-line one would not fit a dialog anyway.
func firstMessageLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return strings.ReplaceAll(trimmed, "|", " ")
		}
	}
	return ""
}

// validateNewDirectoryName checks that a name is a single directory name.
//
// The value is quoted before it reaches the shell, so this is not about
// quoting: it is about what the name is allowed to mean. A name containing a
// separator would create — or write into — somewhere other than the directory
// on screen, and ".." would climb out of it. Both are refused rather than
// sanitised, because a silently altered name is worse than a rejected one.
func validateNewDirectoryName(name string) (string, error) {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "", errors.New("error.folderNameRequired")
	}
	if strings.ContainsAny(cleaned, "/\\") {
		return "", errors.New("error.folderNameHasSeparator")
	}
	if cleaned == "." || cleaned == ".." {
		return "", errors.New("error.folderNameIsDotted")
	}
	// A leading "-" would be read as an option by mkdir rather than a name.
	if strings.HasPrefix(cleaned, "-") {
		return "", errors.New("error.folderNameStartsWithDash")
	}
	return cleaned, nil
}

// RemoteSessionInfo is one multiplexer session found on a server.
type RemoteSessionInfo struct {
	Name string `json:"name"`
	// Windows is how many windows it holds, which is the one thing `tmux ls`
	// says about a session's size.
	Windows int `json:"windows"`
	// Attached says something is currently watching it.
	Attached bool `json:"attached"`
	// Created is the session's start time, as a Unix timestamp.
	//
	// Not the multiplexer's own formatted string: tmux 2.6 — which is what
	// the servers people actually have tend to run — leaves
	// session_created_string empty, so the number is the one thing that can
	// be relied on. The frontend formats it in the user's locale anyway.
	Created int64 `json:"created"`
	// Owner, Project, Path and Agent come from the options the app writes when
	// it creates a session. Empty for a session started by hand, or by a
	// version that did not write them.
	Owner   string `json:"owner,omitempty"`
	Project string `json:"project,omitempty"`
	Path    string `json:"path,omitempty"`
	Agent   string `json:"agent,omitempty"`
	// Ours says this computer created it — the owner matches, and it is in
	// this app's storage.
	Ours bool `json:"ours"`
	// ThisMachine says the owner tag names this computer, whether or not the
	// session is still in storage. A session tagged with this machine but
	// missing from storage is one this computer lost track of.
	ThisMachine bool `json:"thisMachine"`
	// View marks the helper sessions the app creates to show one window. They
	// are listed so the picture is complete, but they are not work.
	View bool `json:"view"`
	// ViewSession names the session a view belongs to, as the user knows it:
	// the project name when the session carries one, the raw session name
	// otherwise. A server can hold several sessions, and a tab name alone —
	// "Terminal" — says nothing about which.
	ViewSession string `json:"viewSession,omitempty"`
	// ViewOf names the tab a view session shows, for the ones that are views.
	//
	// The tab's own name, not its index: the index is an internal number the
	// user never sees anywhere else — remote tabs are numbered from 100 so
	// they cannot collide with local ones — and showing it explains nothing.
	// The session name alone is no better, being the base name with a number
	// tacked on and truncated in any list.
	ViewOf string `json:"viewOf,omitempty"`
}

// ListServerSessions reports the multiplexer sessions on a server.
//
// A server is shared. The same machine can hold sessions from this computer,
// from another of the user's machines, and ones started by hand — and they all
// look alike in a listing. What the app creates is tagged with the machine,
// project and path behind it, so a session can say where it came from instead
// of being an unexplained name.
func (a *App) ListServerSessions(serverID string) ([]RemoteSessionInfo, error) {
	connection, err := a.connectionFor(serverID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()

	// One command for everything, including the tags: a listing that needed a
	// round trip per session would be slow on exactly the server that has a
	// lot of them.
	const format = "#{session_name}\t#{session_windows}\t#{session_attached}\t" +
		"#{session_created}\t#{@asmgr_owner}\t#{@asmgr_project}\t" +
		"#{@asmgr_path}\t#{@asmgr_agent}"
	output, err := connection.executor.Output(ctx, "list-sessions", "-F", format)
	if err != nil {
		// No server running is an empty list, not a failure: a server with
		// nothing on it is a perfectly ordinary state.
		return []RemoteSessionInfo{}, nil
	}

	known := a.knownSessionNames()
	thisMachine := session.MachineIdentity()
	tabNames := a.viewTabNames(ctx, connection)

	// Resolved in two passes: the first reads every session, the second names
	// the views from what the first found. A view's base session is the one
	// that knows the project — the view itself carries no tags.
	var sessions []RemoteSessionInfo
	projectOf := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		at := func(index int) string {
			if index < len(fields) {
				return strings.TrimSpace(fields[index])
			}
			return ""
		}

		info := RemoteSessionInfo{
			Name:     at(0),
			Attached: at(2) == "1",

			Owner:       at(4),
			Project:     at(5),
			Path:        at(6),
			Agent:       at(7),
			View:        strings.HasPrefix(at(0), "asmgr_view_"),
			ThisMachine: thisMachine != "" && at(4) == thisMachine,
		}
		if info.View {
			info.ViewOf = tabNames[at(0)]
			info.ViewSession = baseSessionOf(at(0))
		}
		fmt.Sscanf(at(1), "%d", &info.Windows)
		fmt.Sscanf(at(3), "%d", &info.Created)
		info.Ours = known[info.Name]
		if !info.View {
			// The readable name for the row itself, by the same rule.
			info.Project = sessionDisplayName(info.Name, info.Project)
		}
		if !info.View {
			projectOf[info.Name] = sessionDisplayName(info.Name, info.Project)
		}
		sessions = append(sessions, info)
	}

	// Now that every session has been seen, give each view the name of the
	// session behind it.
	//
	// The readable name in both cases: the project tag when there is one, and
	// otherwise the name recovered from the session id.
	for at := range sessions {
		if !sessions[at].View {
			continue
		}
		if name := projectOf[sessions[at].ViewSession]; name != "" {
			sessions[at].ViewSession = name
		} else {
			sessions[at].ViewSession = sessionDisplayName(sessions[at].ViewSession, "")
		}
	}

	// Work first, then the app's own view sessions: the views are bookkeeping,
	// and a list that leads with them buries what the user came to look at.
	sort.SliceStable(sessions, func(left, right int) bool {
		if sessions[left].View != sessions[right].View {
			return !sessions[left].View
		}
		return sessions[left].Name < sessions[right].Name
	})
	return sessions, nil
}

// KillServerSession stops one multiplexer session on a server.
//
// Whatever runs inside it ends with it, which is exactly what makes a session
// on a server worth having — so this is never done on the app's own initiative.
// The caller asks first.
func (a *App) KillServerSession(serverID, sessionName string) error {
	name := strings.TrimSpace(sessionName)
	if name == "" {
		return fmt.Errorf("error.noSessionGiven")
	}
	// A name is a name, not a target expression. Without this a value
	// containing a colon would address a window, and one starting with "-"
	// would be read as an option.
	if strings.ContainsAny(name, ":.") || strings.HasPrefix(name, "-") {
		return fmt.Errorf("error.notASessionName|%s", name)
	}

	connection, err := a.connectionFor(serverID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()

	if err := connection.executor.Run(ctx, "kill-session", "-t", name); err != nil {
		return err
	}
	log.Printf("[servers] killed session %s on %s", name, serverID)
	return nil
}

// sessionDisplayName is the readable name of a multiplexer session.
//
// The project tag when the session carries one. Failing that, the name is
// recovered from the session id itself: the app builds them as
// "asm_<agent>_<name>_<timestamp>", so the name is in there — which matters
// for every session created before the app started tagging them, and those are
// exactly the ones a user is most likely to be looking at right now.
//
// Falls back to the raw id when it does not have that shape, since a session
// started by hand can be called anything.
func sessionDisplayName(sessionID, projectTag string) string {
	if projectTag != "" {
		return projectTag
	}
	if !strings.HasPrefix(sessionID, "asm_") {
		return sessionID
	}
	rest := sessionID[len("asm_"):]

	// The timestamp at the end: everything after the last underscore, provided
	// it is all digits.
	lastUnderscore := strings.LastIndex(rest, "_")
	if lastUnderscore <= 0 {
		return sessionID
	}
	for _, character := range rest[lastUnderscore+1:] {
		if character < '0' || character > '9' {
			return sessionID
		}
	}

	// The agent at the front: everything before the first underscore.
	firstUnderscore := strings.Index(rest, "_")
	if firstUnderscore < 0 || firstUnderscore >= lastUnderscore {
		return sessionID
	}
	if name := rest[firstUnderscore+1 : lastUnderscore]; name != "" {
		return name
	}
	return sessionID
}

// baseSessionOf returns the multiplexer session a view belongs to.
//
// A view is named "asmgr_view_<session>_<window index>", so the session is
// what sits between the prefix and the trailing index. Returns empty when the
// name does not have that shape.
func baseSessionOf(viewName string) string {
	const prefix = "asmgr_view_"
	if !strings.HasPrefix(viewName, prefix) {
		return ""
	}
	rest := viewName[len(prefix):]
	at := strings.LastIndex(rest, "_")
	if at <= 0 {
		return ""
	}
	// Only when what follows is an index; a session name can contain
	// underscores of its own, and cutting at the wrong one would invent a
	// session that does not exist.
	for _, character := range rest[at+1:] {
		if character < '0' || character > '9' {
			return ""
		}
	}
	if at+1 == len(rest) {
		return ""
	}
	return rest[:at]
}

// viewTabNames maps each view session to the name of the tab it shows.
//
// A view holds the linked window itself, and that window carries the tab's
// name — so the readable answer is already on the server. Asked with one
// list-windows across every session rather than one per view: this runs while
// a dialog waits, and a round trip per session would be slowest on the server
// with the most of them.
//
// A view that cannot be resolved is simply left without a name, which the
// caller renders as the session name.
func (a *App) viewTabNames(ctx context.Context, connection *serverConnection) map[string]string {
	names := make(map[string]string)

	output, err := connection.executor.Output(ctx, "list-windows", "-a",
		"-F", "#{session_name}\t#{window_index}\t#{window_name}")
	if err != nil {
		return names
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		sessionName := strings.TrimSpace(fields[0])
		if !strings.HasPrefix(sessionName, "asmgr_view_") {
			continue
		}
		// A view holds its placeholder at index 0 and the linked window at the
		// index it came from; the placeholder is not what the view is of.
		if strings.TrimSpace(fields[1]) == "0" {
			continue
		}
		if name := strings.TrimSpace(fields[2]); name != "" {
			names[sessionName] = name
		}
	}
	return names
}

// knownSessionNames is the set of multiplexer session names this app holds in
// storage, across every project.
func (a *App) knownSessionNames() map[string]bool {
	known := make(map[string]bool)
	instances, _, _, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return known
	}
	for _, inst := range instances {
		known[inst.TmuxSessionName()] = true
	}
	return known
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
