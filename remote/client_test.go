package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A server that answers with a different key than the one accepted before is
// the one case that must never be waved through. The honest explanations — a
// rebuilt machine, a reused address — look exactly like the dishonest one, so
// the decision belongs to the user, made knowingly.
func TestHostKeyMismatchIsItsOwnError(t *testing.T) {
	err := error(&HostKeyMismatchError{
		ServerID: "s1",
		Expected: "SHA256:aaa",
		Actual:   "SHA256:bbb",
	})

	var mismatch *HostKeyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatal("a changed host key cannot be told apart from an ordinary failure")
	}
	if !strings.Contains(err.Error(), "SHA256:aaa") ||
		!strings.Contains(err.Error(), "SHA256:bbb") {
		t.Errorf("the message hides one of the fingerprints: %s", err)
	}
}

// An unknown key is a question, not a failure — and the fingerprint has to
// travel with it, or the user has nothing to check against.
func TestUnknownHostKeyCarriesTheFingerprint(t *testing.T) {
	err := error(&UnknownHostKeyError{ServerID: "s1", Fingerprint: "SHA256:xyz"})

	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatal("a first connection is indistinguishable from a failure")
	}
	if unknown.Fingerprint != "SHA256:xyz" {
		t.Errorf("fingerprint = %q", unknown.Fingerprint)
	}
}

// ssh-agent with no keys and no agent at all are different problems with
// different fixes, and the message has to say which.
func TestAgentAuthExplainsWhatIsMissing(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	_, err := authMethods(&Target{AuthMethod: "agent"}, nil)
	if err == nil {
		t.Fatal("agent authentication succeeded with no agent")
	}
	if !strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Errorf("the message does not say what is missing: %v", err)
	}
}

func TestPasswordAuthNeedsAPassword(t *testing.T) {
	_, err := authMethods(&Target{AuthMethod: "password"}, &Credentials{})
	if err == nil {
		t.Error("password authentication was attempted with no password")
	}

	methods, err := authMethods(&Target{AuthMethod: "password"}, &Credentials{Password: "s3cret"})
	if err != nil {
		t.Fatalf("a password was refused: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("got %d auth methods", len(methods))
	}
}

// An encrypted key is not a broken key. Reported as its own type so the caller
// asks for the passphrase instead of telling the user their key is unreadable.
func TestEncryptedKeyAsksForAPassphrase(t *testing.T) {
	// An OpenSSH private key with a passphrase, generated for this test.
	const encrypted = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABAvvvvvvv
-----END OPENSSH PRIVATE KEY-----
`
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte(encrypted), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadKey(path, "")
	if err == nil {
		t.Fatal("a malformed or encrypted key loaded without complaint")
	}
	// Whichever it is, the message must name the file rather than leaving the
	// user to guess which of several keys was tried.
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("unhelpful message: %v", err)
	}
}

func TestMissingKeyFileSaysSo(t *testing.T) {
	_, err := loadKey(filepath.Join(t.TempDir(), "absent"), "")
	if err == nil {
		t.Fatal("a missing key file was accepted")
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Errorf("message = %v", err)
	}
}

// Key paths are typed by hand and almost always start with ~.
func TestHomeIsExpandedInKeyPaths(t *testing.T) {
	expanded, err := expandHome("~/.ssh/id_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(expanded, "~") {
		t.Errorf("the tilde survived: %q", expanded)
	}
	// The separator is not rewritten, and must not be: this path is typed for
	// the SERVER, which is always Unix. On Windows the home directory arrives
	// with backslashes and the rest keeps its forward slashes — the result is
	// mixed, and that is correct.
	if !strings.HasSuffix(expanded, "/.ssh/id_ed25519") {
		t.Errorf("expanded to %q", expanded)
	}

	// Anything without a leading ~ is left exactly as typed, whatever it looks
	// like on this machine: it describes a location on the server.
	if got, _ := expandHome("/etc/keys/id"); got != "/etc/keys/id" {
		t.Errorf("a server path was rewritten to %q", got)
	}
}

func TestAddressFillsInThePort(t *testing.T) {
	if got := (&Target{Host: "h"}).address(); got != "h:22" {
		t.Errorf("address = %q", got)
	}
	if got := (&Target{Host: "h", Port: 2222}).address(); got != "h:2222" {
		t.Errorf("address = %q", got)
	}
	// An IPv6 address has to keep its brackets, or the port cannot be told
	// from the address.
	if got := (&Target{Host: "::1", Port: 22}).address(); got != "[::1]:22" {
		t.Errorf("IPv6 address = %q", got)
	}
}

// The PATH addition is what makes an agent under ~/.local/bin visible to a
// non-interactive shell. A path with a space in it is ordinary on a server
// too, and must not split into two arguments.
func TestExtraPathIsQuoted(t *testing.T) {
	plain := withExtraPath(&Target{}, "tmux -V")
	if plain != "tmux -V" {
		t.Errorf("a server with no extra PATH had its command rewritten: %q", plain)
	}

	withPath := withExtraPath(&Target{ExtraPath: "/opt/my tools/bin"}, "tmux -V")
	if !strings.Contains(withPath, `'/opt/my tools/bin'`) {
		t.Errorf("the path was not quoted: %q", withPath)
	}
	if !strings.HasSuffix(withPath, "tmux -V") {
		t.Errorf("the command was lost: %q", withPath)
	}
}

// A single quote inside the value would otherwise end the quoting and hand the
// rest to the shell.
func TestShellQuoteClosesTheHole(t *testing.T) {
	quoted := shellQuote("it's here")
	if strings.Contains(quoted, "it's here") {
		t.Errorf("the quote was passed through unescaped: %s", quoted)
	}
	if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
		t.Errorf("value is not quoted: %s", quoted)
	}
}

func TestArchitectureMapping(t *testing.T) {
	cases := map[string]string{
		"x86_64":       "amd64",
		"amd64":        "amd64",
		"aarch64":      "arm64",
		"arm64":        "arm64",
		"  x86_64  \n": "amd64",
		"armv7l":       "",
		"riscv64":      "",
	}
	for input, want := range cases {
		if got := normaliseArch(input); got != want {
			t.Errorf("uname -m %q -> %q, want %q", input, got, want)
		}
	}
}
