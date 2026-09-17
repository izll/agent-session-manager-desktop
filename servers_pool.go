package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
		return nil, fmt.Errorf("%s has not been checked yet — open Settings › Remote servers "+
			"and test the connection first", server.DisplayName())
	}

	target, creds, err := a.buildTarget(serverID, "", "")
	if err != nil {
		return nil, err
	}

	dialCtx, cancelDial := context.WithTimeout(a.ctx, remote.DialTimeout)
	defer cancelDial()
	client, err := remote.Dial(dialCtx, target, creds, nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", server.DisplayName(), err)
	}

	setupCtx, cancelSetup := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancelSetup()

	if _, err := remote.EnsureHelper(setupCtx, client, helperBinaryFor); err != nil {
		client.Close()
		return nil, fmt.Errorf("could not set up %s: %w", server.DisplayName(), err)
	}

	helper, err := remote.StartHelper(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("could not start the helper on %s: %w", server.DisplayName(), err)
	}

	connection := &serverConnection{
		client:   client,
		helper:   helper,
		executor: remote.NewExecutor(helper, server.DisplayName(), server.ExtraPath),
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
	if inst == nil || inst.ServerID == "" {
		return nil
	}
	connection, err := a.connectionFor(inst.ServerID)
	if err != nil {
		// The session stays unroutable rather than silently falling back to
		// this computer: running it here would start an agent in the wrong
		// place, against a directory that may not exist.
		session.ClearExecutor(inst.ID)
		return err
	}
	session.SetExecutor(inst.ID, connection.executor)
	return nil
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
