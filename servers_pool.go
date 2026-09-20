package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/remote"
	"asmgr-desktop/session"
)

// Keeping one connection per server, shared by everything that needs it.
//
// Connections are expensive to make and cheap to keep: a handshake is 50-200
// ms, and the sidebar polls every server on a timer. They are also where the
// helper lives, so a reconnection means reinstalling nothing but does mean a
// new helper process.

// serverConnection is one server's live connection.
type serverConnection struct {
	client *remote.Client
	helper *remote.Helper
	// executor is what sessions on this server route their commands through.
	executor *remote.Executor
	// failed marks a connection whose helper has stopped answering, so the
	// next caller replaces it rather than waiting on something dead.
	failed bool
}

// serverPool holds the connections.
type serverPool struct {
	mu          sync.Mutex
	connections map[string]*serverConnection
	// connecting serialises per server, so two sessions starting at once do
	// not both dial and both install the helper.
	connecting map[string]*sync.Mutex
}

func newServerPool() *serverPool {
	return &serverPool{
		connections: make(map[string]*serverConnection),
		connecting:  make(map[string]*sync.Mutex),
	}
}

// lockFor returns the per-server mutex, creating it on first use.
func (p *serverPool) lockFor(serverID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	lock, found := p.connecting[serverID]
	if !found {
		lock = &sync.Mutex{}
		p.connecting[serverID] = lock
	}
	return lock
}

// get returns a live connection, or nil.
func (p *serverPool) get(serverID string) *serverConnection {
	p.mu.Lock()
	defer p.mu.Unlock()
	connection, found := p.connections[serverID]
	if !found || connection.failed {
		return nil
	}
	return connection
}

func (p *serverPool) put(serverID string, connection *serverConnection) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connections[serverID] = connection
}

// drop closes a server's connection and forgets it.
func (p *serverPool) drop(serverID string) {
	p.mu.Lock()
	connection, found := p.connections[serverID]
	delete(p.connections, serverID)
	p.mu.Unlock()

	if !found {
		return
	}
	if connection.helper != nil {
		connection.helper.Close()
	}
	if connection.client != nil {
		connection.client.Close()
	}
}

// closeAll ends every connection, for shutdown.
func (p *serverPool) closeAll() {
	p.mu.Lock()
	ids := make([]string, 0, len(p.connections))
	for id := range p.connections {
		ids = append(ids, id)
	}
	p.mu.Unlock()

	for _, id := range ids {
		p.drop(id)
	}
}

// connectionFor returns a working connection to a server, making one if
// needed — including installing or upgrading the helper.
func (a *App) connectionFor(serverID string) (*serverConnection, error) {
	if existing := a.servers.get(serverID); existing != nil {
		return existing, nil
	}

	// One dial at a time per server. Without this, two sessions starting
	// together would both connect and both try to install the helper, and the
	// install lock on the server would fail one of them for no reason.
	lock := a.servers.lockFor(serverID)
	lock.Lock()
	defer lock.Unlock()

	// Someone else may have finished while we waited.
	if existing := a.servers.get(serverID); existing != nil {
		return existing, nil
	}

	server, err := a.storage.FindServer(serverID)
	if err != nil {
		return nil, err
	}
	if server.HostKey == "" {
		// Connecting would mean accepting whatever key answers, which is the
		// decision the connection test exists to put in front of the user.
		return nil, fmt.Errorf("error.serverNotChecked|%s", server.DisplayName())
	}

	target, creds, err := a.buildTarget(serverID, "", "")
	if err != nil {
		return nil, err
	}

	dialCtx, cancelDial := context.WithTimeout(a.ctx, remote.DialTimeout)
	defer cancelDial()
	client, err := remote.Dial(dialCtx, target, creds, nil)
	if err != nil {
		return nil, fmt.Errorf("error.couldNotReachServer|%s|%s", server.DisplayName(), firstMessageLine(err.Error()))
	}

	setupCtx, cancelSetup := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancelSetup()

	if _, err := remote.EnsureHelper(setupCtx, client, helperBinaryFor); err != nil {
		client.Close()
		return nil, fmt.Errorf("error.couldNotSetUpServer|%s|%s", server.DisplayName(), firstMessageLine(err.Error()))
	}

	helper, err := remote.StartHelper(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("error.couldNotStartHelper|%s|%s", server.DisplayName(), firstMessageLine(err.Error()))
	}

	// Find the agents ourselves when the server's PATH does not reach them.
	//
	// An agent installed in ~/.local/bin — where Claude's own installer puts
	// it — is invisible to a non-interactive SSH shell, so every tab using it
	// failed to start. The program can see where it is; asking the user to
	// copy that directory into a settings field is work it can do itself.
	//
	// Only when the configured PATH finds nothing: a server already set up
	// correctly is left alone, and discovery never overrides a deliberate
	// choice.
	extraPath := server.ExtraPath
	if strings.TrimSpace(extraPath) == "" {
		discoverCtx, cancelDiscover := context.WithTimeout(a.ctx, remote.CommandTimeout)
		if found := remote.DiscoverAgentPath(discoverCtx, client, target); found != "" {
			extraPath = found
			log.Printf("[servers] %s: agents found in %s, which is not on its PATH; using it",
				server.DisplayName(), found)
			// Remembered, so the next connection starts with it and the server
			// editor shows what is in force rather than an empty field.
			a.rememberDiscoveredPath(serverID, found)
		}
		cancelDiscover()
	}

	connection := &serverConnection{
		client:   client,
		helper:   helper,
		executor: remote.NewExecutor(helper, server.DisplayName(), extraPath),
	}
	// When the helper ends — the network dropped, the server rebooted, someone
	// killed it — the connection is marked so the next use builds a fresh one
	// rather than waiting on something that cannot answer. The tmux sessions on
	// the server are unaffected: that is the point of them living there.
	helper.OnClosed(func(reason error) {
		log.Printf("[servers] lost the connection to %s: %v", server.DisplayName(), reason)
		a.servers.markFailed(serverID)
	})
	a.servers.put(serverID, connection)
	log.Printf("[servers] connected to %s", server.DisplayName())
	return connection, nil
}

// rememberDiscoveredPath stores a PATH addition that was found rather than
// configured.
//
// Written back so it survives a restart and is visible in the editor: a value
// in force but not shown would be a setting the user cannot reason about.
func (a *App) rememberDiscoveredPath(serverID, path string) {
	err := a.storage.UpdateServers(func(list *session.ServerList) error {
		for index := range list.Servers {
			if list.Servers[index].ID == serverID && strings.TrimSpace(list.Servers[index].ExtraPath) == "" {
				list.Servers[index].ExtraPath = path
			}
		}
		return nil
	})
	if err != nil {
		// The connection works either way; this only means the next one will
		// have to look again.
		log.Printf("[servers] could not store the discovered PATH for %s: %v", serverID, err)
	}
}

// markFailed records that a server's connection has stopped answering, so the
// next caller builds a new one rather than waiting on something dead.
func (p *serverPool) markFailed(serverID string) {
	p.mu.Lock()
	connection, found := p.connections[serverID]
	if found {
		connection.failed = true
	}
	p.mu.Unlock()

	if found {
		// Closed on the way out: the SSH connection may still be holding a
		// socket open, and nothing will ever read from it again.
		go func() {
			if connection.helper != nil {
				connection.helper.Close()
			}
			if connection.client != nil {
				connection.client.Close()
			}
		}()
	}
}

// ReconnectServer drops a server's connection so the next use builds a fresh
// one.
//
// Exposed for the UI, because a user who has just fixed something on the
// server — restarted it, repaired the network — should not have to restart the
// app to have it noticed.
func (a *App) ReconnectServer(serverID string) error {
	a.servers.drop(serverID)
	log.Printf("[servers] connection to %s dropped; it will be rebuilt on next use", serverID)
	return nil
}

// routeSessionCommands points a session's commands at its server.
//
// Called whenever a session is loaded or started. A session with no server
// keeps running locally, which is what the empty executor means.
func (a *App) routeSessionCommands(inst *session.Instance) error {
	if inst == nil {
		return nil
	}

	// Never dial from here.
	//
	// This runs inside Storage.GetInstance, which holds the storage lock that
	// every other part of the app goes through — the sidebar poller, the tab
	// bar, each terminal attach. Building an SSH connection under that lock
	// froze the entire window for as long as the server took to answer, and a
	// server that is simply unreachable froze it for the full dial timeout.
	//
	// So routing uses connections that already exist, and asks for the missing
	// ones to be built in the background. The session is routed a moment later
	// instead of the app stopping until it can be.
	return a.routeUsingReadyConnections(inst)
}

// routeUsingReadyConnections points a session at the connections already open,
// and starts building any that are missing.
func (a *App) routeUsingReadyConnections(inst *session.Instance) error {
	var missing []string
	if inst.ServerID != "" {
		if connection := a.servers.get(inst.ServerID); connection != nil {
			session.SetExecutor(inst.ID, connection.executor)
		} else {
			session.ClearExecutor(inst.ID)
			missing = append(missing, inst.ServerID)
		}
	}
	for _, serverID := range tabServers(inst) {
		if connection := a.servers.get(serverID); connection != nil {
			session.SetTabExecutor(inst.ID, serverID, connection.executor)
			continue
		}
		session.ClearTabExecutor(inst.ID, serverID)
		missing = append(missing, serverID)
	}

	for _, serverID := range missing {
		a.connectInBackground(serverID)
	}
	if len(missing) > 0 {
		return fmt.Errorf("not connected yet")
	}
	return nil
}

// connectInBackground builds a server's connection off the caller's goroutine.
//
// Deduplicated per server: routing runs on every session load, and without
// this a sidebar poll would queue one dial per session per pass.
func (a *App) connectInBackground(serverID string) {
	if _, already := a.connecting.LoadOrStore(serverID, true); already {
		return
	}
	go func() {
		defer a.connecting.Delete(serverID)
		if _, err := a.connectionFor(serverID); err != nil {
			log.Printf("[servers] could not connect to %s: %v", serverID, err)
		}
	}()
}


// tabServers lists the servers this session's tabs sit on, other than the
// session's own machine.
func tabServers(inst *session.Instance) []string {
	var servers []string
	for _, window := range inst.FollowedWindows {
		if window.ServerID == "" || window.ServerID == inst.ServerID {
			continue
		}
		known := false
		for _, existing := range servers {
			if existing == window.ServerID {
				known = true
				break
			}
		}
		if !known {
			servers = append(servers, window.ServerID)
		}
	}
	return servers
}

// helperBinaryFor supplies the helper for a server's architecture.
//
// Built on demand from this repository during development; a release ships the
// binaries beside the app. Kept as a variable so a test can substitute one.
var helperBinaryFor = func(arch string) ([]byte, error) {
	path, err := helperBinaryPath(arch)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
