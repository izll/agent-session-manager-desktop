package passwords

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bitwarden accounts. There can be several, because a company may run its own servers
// and bw talks to one server and one account per data directory.

const bitwardenSourcePrefix = "bitwarden:"

// bitwardenSetupTimeout bounds telling bw its server, or logging it out.
const bitwardenSetupTimeout = 30 * time.Second

// ErrBadServerURL refuses what bw could not connect to.
var ErrBadServerURL = errors.New("error.passwordBadServerUrl")

// BitwardenAccount is one server and account as saved. No server means bitwarden.com.
type BitwardenAccount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ServerURL string `json:"server_url,omitempty"`
}

func (a BitwardenAccount) source() string { return bitwardenSourcePrefix + a.ID }

func checkServerURL(address string) error {
	if address == "" {
		return nil
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ErrBadServerURL
	}
	return nil
}

// bitwardenFor returns the running state of an account. The session key lives there,
// so it is the same object for as long as the app runs.
func (m *Manager) bitwardenFor(id string) *bitwarden {
	if account, known := m.bitwardens[id]; known {
		return account
	}
	account := &bitwarden{run: m.run, id: id, source: bitwardenSourcePrefix + id, settingsPath: m.path, find: m.findBitwarden}
	m.bitwardens[id] = account
	return account
}

func (m *Manager) bitwardenAccount(source string) (BitwardenAccount, bool) {
	for _, account := range m.cfg.Bitwarden {
		if account.source() == source {
			return account, true
		}
	}
	return BitwardenAccount{}, false
}

// BitwardenInstalled says whether bw can be started here.
func (m *Manager) BitwardenInstalled(ctx context.Context) bool {
	return (&bitwarden{find: m.findBitwarden}).command(ctx) != nil
}

// SaveBitwardenAccount adds an account and tells bw its server, so it is ready to log
// in to. The address is fixed from then on: bw refuses to change it once logged in.
func (m *Manager) SaveBitwardenAccount(ctx context.Context, name, serverURL string) (string, error) {
	name = strings.TrimSpace(name)
	serverURL = strings.TrimSpace(serverURL)
	if name == "" {
		return "", ErrNameRequired
	}
	if err := checkServerURL(serverURL); err != nil {
		return "", err
	}
	account := BitwardenAccount{ID: uuid.NewString(), Name: name, ServerURL: serverURL}
	m.mu.Lock()
	state := m.bitwardenFor(account.ID)
	m.mu.Unlock()
	command := state.command(ctx)
	if command == nil {
		return "", ErrToolFailed
	}
	dataDir := bitwardenDataDir(m.path, account.ID, command)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	if serverURL != "" {
		setup, cancel := context.WithTimeout(ctx, bitwardenSetupTimeout)
		defer cancel()
		if _, _, err := state.call(setup, nil, "config", "server", serverURL, "--nointeraction"); err != nil {
			_ = os.RemoveAll(dataDir)
			return "", toolError(bitwardenTool, "config", err)
		}
	}
	err := m.change(func() {
		m.cfg.Bitwarden = append(m.cfg.Bitwarden, account)
	})
	if err != nil {
		return "", err
	}
	return account.ID, nil
}

// DeleteBitwardenAccount logs out and removes what bw kept for it on this computer. The
// vault on the server is not touched.
func (m *Manager) DeleteBitwardenAccount(ctx context.Context, id string) error {
	// Goes into a path that gets removed, so it has to be what this package made.
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	m.mu.Lock()
	state := m.bitwardenFor(id)
	state.lock()
	m.mu.Unlock()
	setup, cancel := context.WithTimeout(ctx, bitwardenSetupTimeout)
	defer cancel()
	// As far as it can: being logged out already, or offline, must not keep it listed.
	_, _, _ = state.call(setup, nil, "logout", "--nointeraction")
	err := m.change(func() {
		delete(m.cache, bitwardenSourcePrefix+id)
		delete(m.bitwardens, id)
		for i, account := range m.cfg.Bitwarden {
			if account.ID == id {
				m.cfg.Bitwarden = append(m.cfg.Bitwarden[:i], m.cfg.Bitwarden[i+1:]...)
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if command := state.command(ctx); command != nil {
		_ = os.RemoveAll(bitwardenDataDir(m.path, id, command))
	}
	return os.RemoveAll(filepath.Join(filepath.Dir(m.path), "bitwarden", id))
}

// BitwardenLoginLine says what to type at a shell here to log in to an account.
func (m *Manager) BitwardenLoginLine(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	_ = m.reload()
	_, found := m.bitwardenAccount(bitwardenSourcePrefix + id)
	state := m.bitwardenFor(id)
	m.mu.Unlock()
	if !found {
		return "", ErrNotFound
	}
	command := state.command(ctx)
	if command == nil {
		return "", ErrToolFailed
	}
	return bitwardenLoginLine(command, bitwardenDataDir(m.path, id, command), runtime.GOOS), nil
}
