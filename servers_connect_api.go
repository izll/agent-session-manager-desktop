package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"asmgr-desktop/remote"
	"asmgr-desktop/session"
)

// Frontend API for connecting to a server: the test, and accepting a host key.

// ConnectionTestResult is what the manager shows after "Test".
type ConnectionTestResult struct {
	Steps           []remote.CheckStep `json:"steps"`
	HostKey         string             `json:"hostKey"`
	HostKeyIsNew    bool               `json:"hostKeyIsNew"`
	HostKeyChanged  bool               `json:"hostKeyChanged"`
	NeedsPassphrase bool               `json:"needsPassphrase"`
	NeedsPassword   bool               `json:"needsPassword"`
	OK              bool               `json:"ok"`
	// SuggestedExtraPath is a directory holding agents the server's
	// non-interactive shell cannot see, offered for the extra PATH setting.
	SuggestedExtraPath string `json:"suggestedExtraPath,omitempty"`
}

// TestServerConnection reports what a server offers, step by step.
//
// password and passphrase are supplied by the dialog for the attempt only —
// when the server is saved with a password, the stored one is used and nothing
// needs to be typed. They are never written anywhere from here: accepting a
// host key is a separate call, made once the user has seen the fingerprint.
func (a *App) TestServerConnection(serverID, password, passphrase string) (*ConnectionTestResult, error) {
	target, creds, err := a.buildTarget(serverID, password, passphrase)
	if err != nil {
		if missing, isMissing := err.(*missingPasswordError); isMissing {
			// Not a failure — the dialog asks and calls again.
			return &ConnectionTestResult{NeedsPassword: true, Steps: []remote.CheckStep{{
				Name:   remote.StepAuth,
				Status: remote.StepAttention,
				Detail: missing.Error(),
			}}}, nil
		}
		return nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()

	// nil rather than a callback that accepts: an unknown key comes back as a
	// result the user is shown, and accepting it is a deliberate second step.
	// Accepting silently here would make the fingerprint decorative.
	result := remote.Check(ctx, target, creds, nil)

	return &ConnectionTestResult{
		Steps:           result.Steps,
		HostKey:         result.HostKey,
		HostKeyIsNew:    result.HostKeyIsNew,
		HostKeyChanged:  result.HostKeyChanged,
		NeedsPassphrase: result.NeedsPassphrase,
		OK:              result.OK,

		SuggestedExtraPath: result.SuggestedExtraPath,
	}, nil
}

// AcceptServerHostKey records the fingerprint the user confirmed.
//
// Deliberately separate from the test, and deliberately takes the fingerprint
// the user was shown rather than re-reading it: a key that changed between the
// two calls must not be accepted on the strength of the earlier prompt.
func (a *App) AcceptServerHostKey(serverID, fingerprint string) error {
	if strings.TrimSpace(fingerprint) == "" {
		return fmt.Errorf("no fingerprint given")
	}
	return a.storage.UpdateServers(func(list *session.ServerList) error {
		for index := range list.Servers {
			if list.Servers[index].ID == serverID {
				log.Printf("[AcceptServerHostKey] server=%s host key accepted", serverID)
				list.Servers[index].HostKey = fingerprint
				return nil
			}
		}
		return fmt.Errorf("server %q not found", serverID)
	})
}

// PlanServerMultiplexerInstall reports how tmux would be installed on a server.
//
// Nothing is installed by this call. The plan carries the exact command so the
// user sees what would run on their machine before agreeing to it — this is
// software being installed on someone's server, and a button that does it
// quietly would be the wrong shape for that.
func (a *App) PlanServerMultiplexerInstall(serverID, password, passphrase string) (*remote.MultiplexerPlan, error) {
	client, closeClient, err := a.connectServer(serverID, password, passphrase)
	if err != nil {
		return nil, err
	}
	defer closeClient()

	ctx, cancel := context.WithTimeout(a.ctx, remote.CommandTimeout)
	defer cancel()
	return remote.PlanMultiplexerInstall(ctx, client)
}

// InstallServerMultiplexer installs tmux, and reports the version that ends up
// there.
//
// The plan is passed back in rather than worked out again, so what runs is
// what the user was shown.
func (a *App) InstallServerMultiplexer(serverID, password, passphrase string,
	plan remote.MultiplexerPlan) (string, error) {

	client, closeClient, err := a.connectServer(serverID, password, passphrase)
	if err != nil {
		return "", err
	}
	defer closeClient()

	// A package manager fetching and unpacking takes longer than an ordinary
	// command, and being cut off half way through leaves the server's package
	// database locked.
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()

	log.Printf("[InstallServerMultiplexer] server=%s running: %s", serverID, plan.Command)
	version, err := remote.InstallMultiplexer(ctx, client, &plan)
	if err != nil {
		return "", err
	}
	log.Printf("[InstallServerMultiplexer] server=%s installed %s", serverID, version)
	return version, nil
}

// connectServer opens a connection for one call and returns how to close it.
func (a *App) connectServer(serverID, password, passphrase string) (*remote.Client, func(), error) {
	target, creds, err := a.buildTarget(serverID, password, passphrase)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, remote.DialTimeout)
	defer cancel()

	// nil: a server whose key has not been accepted cannot be installed onto
	// either. The test is where that decision is made, deliberately.
	client, err := remote.Dial(ctx, target, creds, nil)
	if err != nil {
		return nil, nil, err
	}
	return client, func() { client.Close() }, nil
}

// missingPasswordError says the server authenticates with a password and none
// is available, so the caller asks rather than reporting a failure.
type missingPasswordError struct{ server string }

func (e *missingPasswordError) Error() string {
	return fmt.Sprintf("%s needs a password", e.server)
}

// buildTarget turns a stored server into something the remote package can dial,
// resolving the jump host chain and the credentials.
func (a *App) buildTarget(serverID, password, passphrase string) (*remote.Target, *remote.Credentials, error) {
	list, err := a.storage.LoadServers()
	if err != nil {
		return nil, nil, err
	}
	return buildTargetFrom(list.Servers, serverID, password, passphrase, 0)
}

// maxJumpDepth stops a chain that the stored list should already have rejected.
// Belt and braces: this runs against whatever is on disk, which may have been
// edited by hand.
const maxJumpDepth = 8

func buildTargetFrom(servers []session.Server, serverID, password, passphrase string,
	depth int) (*remote.Target, *remote.Credentials, error) {

	if depth > maxJumpDepth {
		return nil, nil, fmt.Errorf("the jump host chain is too long")
	}

	var found *session.Server
	for index := range servers {
		if servers[index].ID == serverID {
			found = &servers[index]
			break
		}
	}
	if found == nil {
		return nil, nil, fmt.Errorf("server %q not found", serverID)
	}

	target := &remote.Target{
		ID:           found.ID,
		Host:         found.Host,
		Port:         found.Port,
		User:         found.User,
		AuthMethod:   found.AuthMethod,
		KeyPath:      found.KeyPath,
		ExtraPath:    found.ExtraPath,
		KnownHostKey: found.HostKey,
	}

	creds := &remote.Credentials{Passphrase: passphrase}
	if found.AuthMethod == session.AuthPassword {
		if password != "" {
			creds.Password = password
		} else if stored, ok := session.LookupServerPassword(found.ID); ok {
			creds.Password = stored
		} else {
			return nil, nil, &missingPasswordError{server: found.DisplayName()}
		}
	}

	if found.JumpHostID != "" {
		// The jump host's own credentials come from its own record. A
		// passphrase typed for this server is not passed along: they are
		// different keys, and trying one on the other only produces a
		// confusing failure.
		jumpTarget, jumpCreds, err := buildTargetFrom(servers, found.JumpHostID, "", "", depth+1)
		if err != nil {
			return nil, nil, fmt.Errorf("jump host: %w", err)
		}
		target.Jump = jumpTarget
		target.JumpCredentials = jumpCreds
	}

	return target, creds, nil
}
