// Package passwords lists and fetches passwords from where the user keeps them:
// the app's own list, a KeePassXC database, a Bitwarden vault, or any other tool the
// user describes with two commands.
//
// A password stays on the backend. Lists carry names only, and the one operation
// that yields a secret hands it to the code typing it into a terminal, so nothing
// here can leak it into the interface, a log or a settings file.
//
// The app's own list keeps names in a file and secrets in the system keyring, and
// refuses to store anything when there is no keyring: a password written to a file
// beside its name is not kept safe, only out of sight.
package passwords

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

// Source names.
const (
	SourceLocal     = "local"
	SourceKeePassXC = "keepassxc"
)

// Errors the interface translates.
var (
	ErrLocked           = errors.New("error.passwordSourceLocked")
	ErrWrongMaster      = errors.New("error.passwordWrongMaster")
	ErrNotFound         = errors.New("error.passwordNotFound")
	ErrNoKeyring        = errors.New("error.passwordNoKeyring")
	ErrToolFailed       = errors.New("error.passwordToolFailed")
	ErrNotLoggedIn      = errors.New("error.bitwardenNotLoggedIn")
	ErrNameRequired     = errors.New("error.passwordNameRequired")
	ErrPasswordRequired = errors.New("error.passwordRequired")
)

// autoLock forgets a master password this long after it was last used. It sits in
// memory only so that a run of insertions need not ask each time.
const autoLock = 15 * time.Minute

// vaultTimeout bounds asking a source this package talks to itself.
const vaultTimeout = 30 * time.Second

const keyringService = "asmgr-desktop"

func keyringAccount(id string) string { return "password:" + id }

// Entry is one password's description, never the password.
type Entry struct {
	Source   string `json:"source"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Group    string `json:"group"`
}

// SourceStatus says what can be done with a source right now.
type SourceStatus struct {
	Source string `json:"source"`
	// Available: its command-line tool is installed. Always so for the local list.
	Available bool `json:"available"`
	// Enabled: the user has set it up.
	Enabled bool `json:"enabled"`
	Locked  bool `json:"locked"`
	// Detail is what the user set: the database of a KeePassXC source.
	Detail  string `json:"detail"`
	KeyFile string `json:"keyFile"`
	Error   string `json:"error"`
	// A source the user defined: what it is called, and how it is asked.
	Name        string `json:"name"`
	Tool        string `json:"tool"`
	ListCommand string `json:"listCommand"`
	GetCommand  string `json:"getCommand"`
}

type localEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

type config struct {
	Local            []localEntry       `json:"local"`
	KeePassXCDB      string             `json:"keepassxc_db,omitempty"`
	KeePassXCKeyFile string             `json:"keepassxc_key_file,omitempty"`
	Bitwarden        []BitwardenAccount `json:"bitwarden_accounts,omitempty"`
	// Custom holds commands to run, so it comes from this file only, which is the
	// user's own: nothing imports it, and no other machine can write it.
	Custom []CustomSource `json:"custom,omitempty"`
}

// Manager holds the sources. Safe for concurrent use.
type Manager struct {
	path string

	mu      sync.Mutex
	cfg     config
	keepass *keePassXC
	run     Runner
	// bitwardens is the running state of each account, by id.
	bitwardens map[string]*bitwarden
	// findBitwarden stands in for looking bw up in tests.
	findBitwarden func(context.Context) []string
	timer         *time.Timer
	// cached lists: asking a vault takes seconds, and the picker opens often.
	cache map[string][]Entry
}

// Open loads the settings from path. A missing file is an empty list.
func Open(path string, run Runner) (*Manager, error) {
	if run == nil {
		run = runCommand
	}
	m := &Manager{path: path, keepass: &keePassXC{run: run}, run: run, bitwardens: map[string]*bitwarden{}, cache: map[string][]Entry{}}
	if err := m.reload(); err != nil {
		return nil, err
	}
	m.keepass.configure(m.cfg.KeePassXCDB, m.cfg.KeePassXCKeyFile)
	return m, nil
}

func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".passwords-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.path)
}

// keyringGet is replaced by tests with one that does not answer.
var keyringGet = keyring.Get

// change applies an edit to the settings as they are on disk now, holding the lock
// the other windows of the app also take.
func (m *Manager) change(edit func()) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return session.WithCrossProcessFileLock(m.path+".lock", func() error {
		if err := m.readSettings(); err != nil {
			return err
		}
		edit()
		return m.save()
	})
}

// reload picks up what another window of the app saved. The file is shared by all
// of them, and writing a copy read at startup would drop entries added since, leaving
// their secrets in the keyring with nothing naming them.
func (m *Manager) reload() error {
	// Under the lock even to read: on Windows a file open for reading cannot be
	// replaced, so another window's save would fail with access denied.
	return session.WithCrossProcessFileLock(m.path+".lock", m.readSettings)
}

func (m *Manager) readSettings() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var fresh config
	if err := json.Unmarshal(data, &fresh); err != nil {
		return err
	}
	m.cfg = fresh
	m.keepass.configure(m.cfg.KeePassXCDB, m.cfg.KeePassXCKeyFile)
	return nil
}

// armLock restarts the countdown to forgetting the master passwords.
func (m *Manager) armLock() {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(autoLock, m.Lock)
}

// Lock forgets every master password, at once.
func (m *Manager) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keepass.lock()
	delete(m.cache, SourceKeePassXC)
	for id, account := range m.bitwardens {
		account.lock()
		delete(m.cache, bitwardenSourcePrefix+id)
	}
	for _, custom := range m.cfg.Custom {
		delete(m.cache, custom.source())
	}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

// Statuses says where each source stands.
func (m *Manager) Statuses(ctx context.Context) []SourceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	installed := (&bitwarden{find: m.findBitwarden}).command(ctx) != nil
	statuses := []SourceStatus{
		{Source: SourceLocal, Available: true, Enabled: true},
		{
			Source: SourceKeePassXC, Available: toolInstalled(keePassXCTool),
			Enabled: m.cfg.KeePassXCDB != "", Locked: !m.keepass.unlocked(),
			Detail: m.cfg.KeePassXCDB, KeyFile: m.cfg.KeePassXCKeyFile,
		},
	}
	for _, account := range m.cfg.Bitwarden {
		statuses = append(statuses, SourceStatus{
			Source: account.source(), Available: installed, Enabled: true,
			Locked: !m.bitwardenFor(account.ID).unlocked(), Name: account.Name, Detail: account.ServerURL,
		})
	}
	for _, custom := range m.cfg.Custom {
		statuses = append(statuses, customStatus(custom))
	}
	return statuses
}

// Unlock opens a source with its master password, and checks it by listing.
func (m *Manager) Unlock(ctx context.Context, source, master string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var entries []Entry
	var err error
	switch source {
	case SourceKeePassXC:
		entries, err = m.keepass.unlock(ctx, master)
	default:
		if _, found := m.bitwardenAccount(source); !found {
			return ErrNotFound
		}
		entries, err = m.bitwardenFor(strings.TrimPrefix(source, bitwardenSourcePrefix)).unlock(ctx, master)
	}
	if err != nil {
		return err
	}
	m.cache[source] = entries
	m.armLock()
	return nil
}

// List returns every password of every source open now, by name. A source that
// fails is left out with its error returned beside the rest: one vault's trouble
// must not hide the others.
func (m *Manager) List(ctx context.Context, refresh bool) ([]Entry, map[string]string) {
	entries, failures, custom := m.listVaults(ctx, refresh)
	customEntries, customFailures := m.listCustom(ctx, custom, refresh)
	entries = append(entries, customEntries...)
	for source, failure := range customFailures {
		failures[source] = failure
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, failures
}

func (m *Manager) listVaults(ctx context.Context, refresh bool) ([]Entry, map[string]string, []CustomSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	failures := map[string]string{}
	_ = m.reload()
	entries := make([]Entry, 0, len(m.cfg.Local))
	for _, local := range m.cfg.Local {
		entries = append(entries, Entry{Source: SourceLocal, ID: local.ID, Name: local.Name, Username: local.Username})
	}
	open := []struct {
		source string
		active bool
		list   func(context.Context) ([]Entry, error)
	}{
		{SourceKeePassXC, m.cfg.KeePassXCDB != "" && m.keepass.unlocked(), m.keepass.list},
	}
	for _, account := range m.cfg.Bitwarden {
		state := m.bitwardenFor(account.ID)
		open = append(open, struct {
			source string
			active bool
			list   func(context.Context) ([]Entry, error)
		}{account.source(), state.unlocked(), state.list})
	}
	for _, each := range open {
		if !each.active {
			continue
		}
		cached, known := m.cache[each.source]
		if !known || refresh {
			fresh, err := each.list(ctx)
			if err != nil {
				failures[each.source] = err.Error()
				continue
			}
			cached = fresh
			m.cache[each.source] = fresh
		}
		entries = append(entries, cached...)
	}
	return entries, failures, append([]CustomSource(nil), m.cfg.Custom...)
}

// Secret fetches one password, for the code that types it. Only an entry the source
// has listed can be asked for, so an id from outside cannot reach further.
func (m *Manager) Secret(ctx context.Context, source, id string) (string, error) {
	if strings.HasPrefix(source, customSourcePrefix) {
		return m.customSecret(ctx, source, id)
	}
	ctx, cancel := context.WithTimeout(ctx, vaultTimeout)
	defer cancel()
	if source == SourceLocal {
		return m.localSecret(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, isBitwarden := m.bitwardenAccount(source)
	if source != SourceKeePassXC && !isBitwarden || !m.listed(source, id) {
		return "", ErrNotFound
	}
	m.armLock()
	if source == SourceKeePassXC {
		return m.keepass.secret(ctx, id)
	}
	return m.bitwardenFor(strings.TrimPrefix(source, bitwardenSourcePrefix)).secret(ctx, id)
}

// localSecret asks the keyring outside the lock and no longer than ctx allows: the
// keyring can sit waiting for someone to answer its own prompt, and neither locking
// the vaults nor the dialog waiting for the answer should wait with it.
func (m *Manager) localSecret(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	_ = m.reload()
	found := false
	for _, local := range m.cfg.Local {
		found = found || local.ID == id
	}
	m.mu.Unlock()
	if !found {
		return "", ErrNotFound
	}
	type answer struct {
		secret string
		err    error
	}
	answered := make(chan answer, 1)
	get := keyringGet
	go func() {
		secret, err := get(keyringService, keyringAccount(id))
		answered <- answer{secret, err}
	}()
	select {
	case <-ctx.Done():
		return "", ErrNoKeyring
	case got := <-answered:
		if errors.Is(got.err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		if got.err != nil {
			return "", ErrNoKeyring
		}
		return got.secret, nil
	}
}

func (m *Manager) listed(source, id string) bool {
	for _, entry := range m.cache[source] {
		if entry.ID == id {
			return true
		}
	}
	return false
}

// SaveLocal adds an entry, or changes one when id is set. On a change an empty
// password keeps the one stored: the form never holds it.
func (m *Manager) SaveLocal(id, name, username, password string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if id == "" && password == "" {
		return "", ErrPasswordRequired
	}
	if id != "" && !m.hasLocal(id) {
		return "", ErrNotFound
	}
	newEntry := id == ""
	if newEntry {
		id = uuid.NewString()
	}
	// Outside the lock, as in localSecret.
	if password != "" {
		if err := keyring.Set(keyringService, keyringAccount(id), password); err != nil {
			return "", ErrNoKeyring
		}
	}

	entry := localEntry{ID: id, Name: name, Username: strings.TrimSpace(username)}
	err := m.change(func() {
		for i, local := range m.cfg.Local {
			if local.ID == id {
				m.cfg.Local[i] = entry
				return
			}
		}
		m.cfg.Local = append(m.cfg.Local, entry)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (m *Manager) hasLocal(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.reload()
	for _, local := range m.cfg.Local {
		if local.ID == id {
			return true
		}
	}
	return false
}

// DeleteLocal removes an entry and its secret, the secret first: a failure then
// leaves a listed entry that can be tried again, not a secret nothing names.
func (m *Manager) DeleteLocal(id string) error {
	if !m.hasLocal(id) {
		return ErrNotFound
	}
	if err := keyring.Delete(keyringService, keyringAccount(id)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return ErrNoKeyring
	}
	return m.change(func() {
		for i, local := range m.cfg.Local {
			if local.ID == id {
				m.cfg.Local = append(m.cfg.Local[:i], m.cfg.Local[i+1:]...)
				return
			}
		}
	})
}

// SetKeePassXC points at a database, locked until opened; empty turns it off.
func (m *Manager) SetKeePassXC(database, keyFile string) error {
	return m.change(func() {
		m.cfg.KeePassXCDB = strings.TrimSpace(database)
		m.cfg.KeePassXCKeyFile = strings.TrimSpace(keyFile)
		m.keepass.configure(m.cfg.KeePassXCDB, m.cfg.KeePassXCKeyFile)
		delete(m.cache, SourceKeePassXC)
	})
}
