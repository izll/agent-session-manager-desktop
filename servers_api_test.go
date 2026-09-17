package main

import (
	"os"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// The password is the one field that must not make the round trip to the UI.
// ServerInfo carries a flag instead, and the editor shows a placeholder — so
// a compromised or logged frontend payload holds no secret.
func TestServerInfoCarriesNoPassword(t *testing.T) {
	source, err := os.ReadFile("servers_api.go")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(source), "\r\n", "\n")

	start := strings.Index(body, "type ServerInfo struct {")
	if start < 0 {
		t.Fatal("ServerInfo is gone")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("ServerInfo is unbalanced")
	}
	decl := body[start : start+end]

	if strings.Contains(decl, "Password string") {
		t.Error("ServerInfo carries a password to the frontend; it should carry " +
			"HasPassword and leave the secret in the keyring")
	}
	if !strings.Contains(decl, "HasPassword") {
		t.Error("nothing tells the editor whether a password is already stored, " +
			"so it cannot show the difference between 'none' and 'saved'")
	}
}

// Changing a server's name must not wipe its password, and switching away from
// password authentication must not leave one behind.
func TestPasswordHandlingOnEdit(t *testing.T) {
	if !session.KeyringAvailable() {
		t.Skip("no system keyring on this machine")
	}
	app := &App{}
	const id = "test-edit-server"
	t.Cleanup(func() { session.ForgetServerPassword(id) })

	if err := session.StoreServerPassword(id, "eredeti"); err != nil {
		t.Fatal(err)
	}

	// An edit that types no new password leaves the stored one alone.
	if err := app.applyServerPassword(id, ServerSaveRequest{
		AuthMethod: session.AuthPassword,
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := session.LookupServerPassword(id); !ok || got != "eredeti" {
		t.Error("an unrelated edit discarded the stored password")
	}

	// A new password replaces it.
	if err := app.applyServerPassword(id, ServerSaveRequest{
		AuthMethod: session.AuthPassword,
		Password:   "uj",
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := session.LookupServerPassword(id); got != "uj" {
		t.Errorf("password after replacement = %q", got)
	}

	// Switching to key authentication clears it: nothing would ever use it
	// again, and nothing else would clean it up.
	if err := app.applyServerPassword(id, ServerSaveRequest{
		AuthMethod: session.AuthKey,
		KeyPath:    "/home/user/.ssh/id_ed25519",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := session.LookupServerPassword(id); ok {
		t.Error("a password survived the switch to key authentication")
	}
}

// Deleting a server whose sessions still exist would leave them pointing at
// nothing, with no way to say why they cannot start.
func TestDeleteRefusesWhileSessionsUseIt(t *testing.T) {
	source, err := os.ReadFile("servers_api.go")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(source), "\r\n", "\n")

	start := strings.Index(body, "func (a *App) DeleteServer(")
	if start < 0 {
		t.Fatal("DeleteServer is gone")
	}
	fn := body[start:]
	if end := strings.Index(fn, "\n}\n"); end > 0 {
		fn = fn[:end]
	}

	if !strings.Contains(fn, "inst.ServerID == id") {
		t.Error("DeleteServer does not check for sessions on the server, so " +
			"deleting one strands them")
	}
	if !strings.Contains(fn, "session.ForgetServerPassword(id)") {
		t.Error("the password outlives the server it belonged to")
	}
	if !strings.Contains(fn, "srv.JumpHostID == id") {
		t.Error("a server jumping through the deleted one keeps a dangling " +
			"reference, which fails validation on the next save")
	}
}
