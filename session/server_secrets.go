package session

import (
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// Where a server's password lives.
//
// The system keyring, and nowhere else. The alternative — a file beside
// servers.json — would need its own encryption key stored on the same machine,
// which hides the password rather than protecting it, and looks safer than it
// is. When the keyring cannot be reached (a headless Linux box with no Secret
// Service, a locked session), the password is asked for and kept in memory for
// as long as the app runs.
//
// That fallback is deliberately not persisted. Someone who has to retype a
// password knows they are on a machine that cannot keep it; someone whose
// password silently landed in a file does not.

const keyringService = "asmgr-desktop"

// keyringAccount namespaces the entry so that two servers, and any other
// secret this app might keep later, cannot collide.
func keyringAccount(serverID string) string {
	return "server:" + serverID
}

// sessionPasswords holds passwords for this run only, for servers whose
// password could not be written to the keyring.
var sessionPasswords sync.Map // map[string]string, keyed by server id

// KeyringAvailable reports whether the system keyring answers at all.
//
// Probed by writing and deleting a marker rather than by reading: a read of a
// missing entry fails the same way on a working keyring as on an absent one,
// and telling the user "your password will not be remembered" wrongly is worse
// than the probe's cost.
func KeyringAvailable() bool {
	const probe = "availability-probe"
	if err := keyring.Set(keyringService, probe, "1"); err != nil {
		return false
	}
	_ = keyring.Delete(keyringService, probe)
	return true
}

// StoreServerPassword saves a password for later connections.
//
// A keyring failure is reported rather than swallowed: the caller decides
// whether to carry on with an in-memory password, and the user is told which
// of the two happened.
func StoreServerPassword(serverID, password string) error {
	if strings.TrimSpace(serverID) == "" {
		return fmt.Errorf("cannot store a password without a server id")
	}
	if err := keyring.Set(keyringService, keyringAccount(serverID), password); err != nil {
		return fmt.Errorf("the system keyring refused the password: %w", err)
	}
	sessionPasswords.Delete(serverID)
	return nil
}

// RememberServerPasswordForSession keeps a password for this run only, for the
// machines where the keyring is unavailable.
func RememberServerPasswordForSession(serverID, password string) {
	if strings.TrimSpace(serverID) == "" {
		return
	}
	sessionPasswords.Store(serverID, password)
}

// LookupServerPassword returns the stored password and whether one was found.
// The keyring is consulted first: a password saved there outlives the run, and
// an in-memory one only exists because the keyring could not hold it.
func LookupServerPassword(serverID string) (string, bool) {
	if strings.TrimSpace(serverID) == "" {
		return "", false
	}
	if secret, err := keyring.Get(keyringService, keyringAccount(serverID)); err == nil && secret != "" {
		return secret, true
	}
	if held, ok := sessionPasswords.Load(serverID); ok {
		if secret, isString := held.(string); isString && secret != "" {
			return secret, true
		}
	}
	return "", false
}

// ForgetServerPassword removes a password from both places.
//
// Called when a server is deleted, and when its authentication method changes
// away from a password — an entry left in the keyring for a server that no
// longer exists is a secret nobody will ever clean up.
func ForgetServerPassword(serverID string) {
	if strings.TrimSpace(serverID) == "" {
		return
	}
	sessionPasswords.Delete(serverID)
	// A missing entry is not an error worth reporting: the common case is a
	// server that never had a password at all.
	_ = keyring.Delete(keyringService, keyringAccount(serverID))
}
