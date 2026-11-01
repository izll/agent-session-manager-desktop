package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"asmgr-desktop/passwords"
	"asmgr-desktop/session"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Typing a password into a tab, picked from a list.
//
// Nothing here returns a password to the interface. It sends a name and gets a name
// back, and the secret travels only from the store to the tab.

// passwordFetchTimeout bounds asking a vault, whose tool can take seconds.
const passwordFetchTimeout = 30 * time.Second

// PasswordList is what the picker shows.
type PasswordList struct {
	Entries []passwords.Entry        `json:"entries"`
	Sources []passwords.SourceStatus `json:"sources"`
	// BitwardenInstalled: bw can be started here, so accounts can be added.
	BitwardenInstalled bool `json:"bitwardenInstalled"`
	// KeyringAvailable: without it the app's own list cannot store anything.
	KeyringAvailable bool `json:"keyringAvailable"`
}

func (a *App) passwordManager() (*passwords.Manager, error) {
	a.passwordsMu.Lock()
	defer a.passwordsMu.Unlock()
	if a.passwords == nil {
		opened, err := passwords.Open(a.storage.PasswordsPath(), nil)
		if err != nil {
			return nil, err
		}
		a.passwords = opened
	}
	return a.passwords, nil
}

func passwordContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, passwordFetchTimeout)
}

// GetPasswords lists what can be typed now. refresh asks the vaults again rather
// than using what they said last.
func (a *App) GetPasswords(refresh bool) (*PasswordList, error) {
	manager, err := a.passwordManager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := passwordContext(a.lifecycleContext())
	defer cancel()
	entries, failures := manager.List(ctx, refresh)
	sources := manager.Statuses(ctx)
	for index := range sources {
		sources[index].Error = failures[sources[index].Source]
	}
	return &PasswordList{
		Entries: entries, Sources: sources,
		BitwardenInstalled: manager.BitwardenInstalled(ctx),
		KeyringAvailable:   session.KeyringAvailable(),
	}, nil
}

// UnlockPasswordSource opens a vault. The master password stays in memory until
// the source is locked, or for fifteen minutes from its last use.
func (a *App) UnlockPasswordSource(source, master string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	ctx, cancel := passwordContext(a.lifecycleContext())
	defer cancel()
	return manager.Unlock(ctx, source, master)
}

// LockPasswordSources forgets every master password now.
func (a *App) LockPasswordSources() error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	manager.Lock()
	return nil
}

// SaveLocalPassword adds or changes an entry of the app's own list. An empty
// password on a change keeps the stored one.
func (a *App) SaveLocalPassword(id, name, username, password string) (string, error) {
	manager, err := a.passwordManager()
	if err != nil {
		return "", err
	}
	return manager.SaveLocal(id, name, username, password)
}

// DeleteLocalPassword removes an entry of the app's own list and its secret.
func (a *App) DeleteLocalPassword(id string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	return manager.DeleteLocal(id)
}

// SetKeePassXCDatabase points the KeePassXC source at a database; empty turns it off.
func (a *App) SetKeePassXCDatabase(database, keyFile string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	return manager.SetKeePassXC(database, keyFile)
}

// SaveBitwardenAccount adds a Bitwarden server and account; no address means bitwarden.com.
func (a *App) SaveBitwardenAccount(name, serverURL string) (string, error) {
	manager, err := a.passwordManager()
	if err != nil {
		return "", err
	}
	ctx, cancel := passwordContext(a.lifecycleContext())
	defer cancel()
	return manager.SaveBitwardenAccount(ctx, name, serverURL)
}

// DeleteBitwardenAccount removes an account from this computer.
func (a *App) DeleteBitwardenAccount(id string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	ctx, cancel := passwordContext(a.lifecycleContext())
	defer cancel()
	return manager.DeleteBitwardenAccount(ctx, id)
}

// SaveCustomPasswordSource adds or changes a source made of two commands.
func (a *App) SaveCustomPasswordSource(id, name, listCommand, getCommand string) (string, error) {
	manager, err := a.passwordManager()
	if err != nil {
		return "", err
	}
	return manager.SaveCustom(id, name, listCommand, getCommand)
}

// DeleteCustomPasswordSource removes such a source.
func (a *App) DeleteCustomPasswordSource(id string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	return manager.DeleteCustom(id)
}

// errBitwardenLoginNeedsLocalTab: the vault this app reads is on this machine, so a
// login on a server would not help, and would leave the user's account there.
var errBitwardenLoginNeedsLocalTab = errors.New("error.bitwardenLoginNeedsLocalTab")

func tabRunsOnThisMachine(inst *session.Instance, windowIdx int) bool {
	serverID, _ := inst.TabPlacement(windowIdx)
	return serverID == "" && inst.ServerID == ""
}

// OpenBitwardenLogin opens a terminal tab beside the given one with bw login running
// in it, and returns its index. What the login asks for is answered there, so no
// account password or second factor passes through the app. The tab closes itself
// once bw has ended without a failure.
func (a *App) OpenBitwardenLogin(accountID, sessionID string, fromWindow int, expectedProjectID string) (int, error) {
	manager, err := a.passwordManager()
	if err != nil {
		return -1, err
	}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return -1, err
	}
	if !tabRunsOnThisMachine(inst, fromWindow) {
		return -1, errBitwardenLoginNeedsLocalTab
	}
	ctx, cancel := passwordContext(a.lifecycleContext())
	defer cancel()
	line, err := manager.BitwardenLoginLine(ctx, accountID)
	if err != nil {
		return -1, err
	}
	return a.runCommandInNewTab(sessionID, fromWindow, "Bitwarden", line, session.TabCloseLogin, expectedProjectID)
}

// BrowsePasswordFile asks for a KeePassXC database or key file.
func (a *App) BrowsePasswordFile(current string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	start := home
	if current != "" {
		start = filepath.Dir(current)
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Select file",
		DefaultDirectory: start,
		ShowHiddenFiles:  true,
	})
}

// InsertPassword types a password into a tab, as if it had been typed there.
func (a *App) InsertPassword(source, id, sessionID string, windowIdx int, pressEnter bool, expectedProjectID string) error {
	manager, err := a.passwordManager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.lifecycleContext())
	defer cancel()
	a.passwordsMu.Lock()
	a.cancelPasswordFetch = cancel
	a.passwordsMu.Unlock()
	// Before taking the project lock: asking a vault can take seconds, and every
	// other change in the app would wait with it.
	secret, err := manager.Secret(ctx, source, id)
	// Called off, even if the answer has just arrived: nothing gets typed after that.
	if ctx.Err() != nil && a.lifecycleContext().Err() == nil {
		return nil
	}
	if err != nil {
		return err
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return err
	}
	// Its own deadline: a slow vault must not use up the time to type.
	pasteCtx, cancelPaste := context.WithTimeout(a.lifecycleContext(), session.TmuxCommandTimeout)
	defer cancelPaste()
	return inst.PasteSecretToWindow(pasteCtx, windowIdx, secret, pressEnter)
}

// CancelPasswordInsert calls off fetching a password, which can take as long as a tool
// waits for its own prompt to be answered. Once it is being typed it is too late.
func (a *App) CancelPasswordInsert() {
	a.passwordsMu.Lock()
	cancel := a.cancelPasswordFetch
	a.passwordsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// stopPasswords forgets master passwords at shutdown.
func (a *App) stopPasswords() {
	a.passwordsMu.Lock()
	manager := a.passwords
	a.passwordsMu.Unlock()
	if manager != nil {
		manager.Lock()
	}
}
