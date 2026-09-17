// Package remote connects to the machines where sessions run.
//
// One SSH connection per server, shared by everything that needs it: the
// commands that manage sessions, the streams that carry terminals, and the
// checks that report whether the server is usable at all. Opening a connection
// per command would cost a handshake each time — 50-200 ms — and the sidebar
// polls every server on a timer.
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	// DialTimeout bounds reaching the server at all.
	//
	// A machine that is switched off does not refuse the connection; it
	// answers nothing, and the operating system keeps retrying for minutes.
	// The dialog that asked cannot wait that long, and neither can the user
	// wondering whether they typed the address wrong.
	DialTimeout = 10 * time.Second

	// CommandTimeout bounds one command run over an established connection.
	// Generous, because a first helper install copies a binary.
	CommandTimeout = 60 * time.Second

	// keepAliveInterval stops a NAT or a firewall dropping an idle connection
	// without telling either end. A session left open overnight is the normal
	// case here, not the exception.
	keepAliveInterval = 30 * time.Second
)

// Credentials carry what a connection needs beyond the server record.
//
// Kept apart from session.Server on purpose: the password and the passphrase
// come from the keyring or from the user at connection time, and must not be
// stored on the record that gets written to disk.
type Credentials struct {
	Password string
	// Passphrase unlocks an encrypted private key.
	Passphrase string
}

// Target is one machine to connect to, flattened from a stored server.
type Target struct {
	ID         string
	Host       string
	Port       int
	User       string
	AuthMethod string
	KeyPath    string
	// ExtraPath is prepended to PATH for every command run on this server.
	// An SSH command runs a non-interactive shell, which never reads .bashrc,
	// so an agent under ~/.local/bin or nvm is otherwise invisible to us.
	ExtraPath string
	// KnownHostKey is the fingerprint accepted previously. Empty means this
	// server has never been connected to.
	KnownHostKey string
	// Jump is the machine to connect through, or nil for a direct connection.
	Jump *Target
	// JumpCredentials belong to the jump host, not to this one.
	JumpCredentials *Credentials
}

func (t *Target) address() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", port))
}

// HostKeyMismatchError is returned when a server presents a different key from
// the one accepted before.
//
// Its own type because the caller must not treat it as an ordinary connection
// failure: retrying will not help, and the honest explanations (a rebuilt
// server, a changed port) look exactly like the dishonest one.
type HostKeyMismatchError struct {
	ServerID string
	Expected string
	Actual   string
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("the server's identity has changed: expected %s, got %s",
		e.Expected, e.Actual)
}

// UnknownHostKeyError is returned the first time a server is seen, carrying the
// fingerprint for the user to confirm.
type UnknownHostKeyError struct {
	ServerID    string
	Fingerprint string
}

func (e *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("this server has not been connected to before (%s)", e.Fingerprint)
}

// Client is a live connection to one server.
type Client struct {
	ssh *ssh.Client
	// jump is held so closing this client closes the connection it was
	// tunnelled through; otherwise a bastion connection would be left behind
	// every time an inner server disconnects.
	jump *ssh.Client

	mu     sync.Mutex
	closed bool
	stop   chan struct{}
}

// Dial opens a connection.
//
// acceptNewHostKey decides what happens when the server is unknown: the UI
// passes a function that asks the user, and a caller that must not prompt
// passes nil, which refuses. A mismatch is never negotiable and never reaches
// this callback.
func Dial(ctx context.Context, target *Target, creds *Credentials,
	acceptNewHostKey func(fingerprint string) bool) (*Client, error) {

	if target == nil {
		return nil, fmt.Errorf("no server given")
	}
	if strings.TrimSpace(target.Host) == "" {
		return nil, fmt.Errorf("the server has no host")
	}

	auth, err := authMethods(target, creds)
	if err != nil {
		return nil, err
	}

	var seenKey string
	config := &ssh.ClientConfig{
		User:    target.User,
		Auth:    auth,
		Timeout: DialTimeout,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			seenKey = ssh.FingerprintSHA256(key)
			switch {
			case target.KnownHostKey == "":
				if acceptNewHostKey == nil || !acceptNewHostKey(seenKey) {
					return &UnknownHostKeyError{ServerID: target.ID, Fingerprint: seenKey}
				}
				return nil
			case target.KnownHostKey != seenKey:
				return &HostKeyMismatchError{
					ServerID: target.ID,
					Expected: target.KnownHostKey,
					Actual:   seenKey,
				}
			default:
				return nil
			}
		},
	}

	// A jump host is dialled first, and the inner connection is made through
	// its network rather than through ours.
	var jumpClient *ssh.Client
	if target.Jump != nil {
		jumped, err := Dial(ctx, target.Jump, target.JumpCredentials, acceptNewHostKey)
		if err != nil {
			return nil, fmt.Errorf("could not reach the jump host %s: %w", target.Jump.Host, err)
		}
		jumpClient = jumped.ssh
	}

	conn, err := dialConn(ctx, target, jumpClient)
	if err != nil {
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, err
	}

	sshConn, channels, requests, err := ssh.NewClientConn(conn, target.address(), config)
	if err != nil {
		conn.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, err
	}

	client := &Client{
		ssh:  ssh.NewClient(sshConn, channels, requests),
		jump: jumpClient,
		stop: make(chan struct{}),
	}
	go client.keepAlive()
	return client, nil
}

func dialConn(ctx context.Context, target *Target, through *ssh.Client) (net.Conn, error) {
	if through != nil {
		// DialContext rather than Dial: a jump host that accepts the
		// connection but never forwards it would otherwise hang here with no
		// timeout of its own.
		return through.DialContext(ctx, "tcp", target.address())
	}
	dialer := &net.Dialer{Timeout: DialTimeout}
	return dialer.DialContext(ctx, "tcp", target.address())
}

// HostKeyOf reports the fingerprint a server presents, without requiring it to
// be known already. Used by the connection test, so the user can be shown what
// they are being asked to trust.
func HostKeyOf(ctx context.Context, target *Target, creds *Credentials) (string, error) {
	probe := *target
	probe.KnownHostKey = ""

	var fingerprint string
	client, err := Dial(ctx, &probe, creds, func(seen string) bool {
		fingerprint = seen
		return true
	})
	if err != nil {
		if fingerprint != "" {
			// Authentication failed, but the key was still presented — which
			// is the answer this function was asked for.
			return fingerprint, err
		}
		return "", err
	}
	client.Close()
	return fingerprint, nil
}

// authMethods assembles the ways to prove who we are, in the order they are
// tried.
func authMethods(target *Target, creds *Credentials) ([]ssh.AuthMethod, error) {
	if creds == nil {
		creds = &Credentials{}
	}
	switch target.AuthMethod {
	case "password":
		if creds.Password == "" {
			return nil, fmt.Errorf("this server needs a password")
		}
		return []ssh.AuthMethod{ssh.Password(creds.Password)}, nil

	case "key":
		signer, err := loadKey(target.KeyPath, creds.Passphrase)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

	default: // agent
		signers, err := agentSigners()
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
	}
}

// PassphraseRequiredError says the key is encrypted and no passphrase was
// given, so the caller can ask for one rather than reporting a failure.
type PassphraseRequiredError struct {
	KeyPath string
}

func (e *PassphraseRequiredError) Error() string {
	return fmt.Sprintf("the key %s is protected by a passphrase", e.KeyPath)
}

func loadKey(path, passphrase string) (ssh.Signer, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("could not read the key file: %w", err)
	}

	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("the key could not be unlocked: %w", err)
		}
		return signer, nil
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		var needsPassphrase *ssh.PassphraseMissingError
		if errors.As(err, &needsPassphrase) {
			return nil, &PassphraseRequiredError{KeyPath: expanded}
		}
		return nil, fmt.Errorf("the key could not be read: %w", err)
	}
	return signer, nil
}

// agentSigners asks ssh-agent for the keys it holds.
func agentSigners() ([]ssh.Signer, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, fmt.Errorf("no ssh-agent is running (SSH_AUTH_SOCK is not set) — " +
			"choose a key file or a password instead")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("could not reach ssh-agent: %w", err)
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh-agent holds no usable keys: %w", err)
	}
	if len(signers) == 0 {
		conn.Close()
		return nil, fmt.Errorf("ssh-agent is running but holds no keys — add one with ssh-add")
	}
	return signers, nil
}

func expandHome(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if !strings.HasPrefix(trimmed, "~") {
		return trimmed, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + strings.TrimPrefix(trimmed, "~"), nil
}

// Run executes one command and returns its output.
//
// Each call opens its own channel on the shared connection, which is what
// makes this cheap: the handshake happened once, at Dial.
func (c *Client) Run(ctx context.Context, command string) ([]byte, error) {
	session, err := c.ssh.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := session.CombinedOutput(command)
		done <- result{out: out, err: err}
	}()

	select {
	case <-ctx.Done():
		// Closing the session unblocks the goroutine; without this a command
		// that never returns would hold it forever.
		session.Close()
		return nil, ctx.Err()
	case got := <-done:
		return got.out, got.err
	}
}

// keepAlive holds the connection open across idle periods.
func (c *Client) keepAlive() {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			// The reply is ignored; sending is what keeps the path alive. A
			// failure here means the connection is already gone, and the next
			// command will say so with a better message.
			_, _, _ = c.ssh.SendRequest("keepalive@openssh.com", true, nil)
		}
	}
}

// SSH exposes the underlying connection for the streams that need their own
// channels, such as an attached terminal.
func (c *Client) SSH() *ssh.Client {
	return c.ssh
}

// Close ends the connection, and the one it was tunnelled through.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.stop)
	c.mu.Unlock()

	err := c.ssh.Close()
	if c.jump != nil {
		c.jump.Close()
	}
	return err
}
