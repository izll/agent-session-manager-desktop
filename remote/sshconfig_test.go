package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The shapes people actually have in this file: tabs, spaces, "Key=value",
// mixed case, comments. Measured against a real configuration with eight
// entries before this was written.
func TestReadsTheOrdinaryShapes(t *testing.T) {
	path := writeConfig(t, `# my servers
Host build
    HostName 10.0.0.5
    User izll
    Port 2222
    IdentityFile ~/.ssh/id_ed25519

Host vps
	hostname=vps.example.com
	USER root
`)

	hosts, err := readSSHConfigFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts: %+v", len(hosts), hosts)
	}

	if hosts[0].Alias != "build" || hosts[0].HostName != "10.0.0.5" ||
		hosts[0].User != "izll" || hosts[0].Port != 2222 {
		t.Errorf("first entry = %+v", hosts[0])
	}
	// Case and "=" are both allowed by the format and both appear in the wild.
	if hosts[1].HostName != "vps.example.com" || hosts[1].User != "root" {
		t.Errorf("second entry = %+v", hosts[1])
	}
}

// A short name with no HostName connects to itself — that is how "ssh web"
// works at all, and an entry left with an empty host would be unusable.
func TestAliasIsUsedWhenNoHostNameIsGiven(t *testing.T) {
	path := writeConfig(t, "Host web\n    User root\n")

	hosts, err := readSSHConfigFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].HostName != "web" {
		t.Fatalf("hosts = %+v", hosts)
	}
}

// "Host *" sets defaults for everything rather than naming a machine. Offering
// it would put an entry in the list that cannot be connected to.
func TestPatternsAreNotOfferedAsMachines(t *testing.T) {
	path := writeConfig(t, `Host *
    ServerAliveInterval 60

Host *.internal
    User admin

Host real
    HostName real.example
`)

	hosts, err := readSSHConfigFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Alias != "real" {
		t.Errorf("patterns were offered as machines: %+v", hosts)
	}
}

// One line can name several aliases; the first usable one is the name.
func TestSeveralAliasesOnOneLine(t *testing.T) {
	path := writeConfig(t, "Host web www web1\n    HostName 10.0.0.9\n")

	hosts, err := readSSHConfigFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Alias != "web" {
		t.Errorf("hosts = %+v", hosts)
	}
}

// An entry may list several keys as fallbacks. We have no way to choose
// between them, so the first is taken and the rest ignored.
func TestOnlyTheFirstKeyIsTaken(t *testing.T) {
	path := writeConfig(t, `Host multi
    HostName m.example
    IdentityFile ~/.ssh/first
    IdentityFile ~/.ssh/second
`)

	hosts, err := readSSHConfigFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].KeyPath != "~/.ssh/first" {
		t.Errorf("hosts = %+v", hosts)
	}
}

// Plenty of people have no SSH configuration at all, and that is not a fault.
func TestAMissingFileIsNotAnError(t *testing.T) {
	hosts, err := readSSHConfigFile(filepath.Join(t.TempDir(), "absent"), 0)
	if err != nil {
		t.Errorf("a missing config was reported as an error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("got %d hosts from nothing", len(hosts))
	}
}

// A file that includes itself would otherwise be read forever.
func TestIncludeDepthIsBounded(t *testing.T) {
	path := writeConfig(t, "Host a\n    HostName a.example\n")

	// Past the limit, nothing is read — which is the safe end of the trade:
	// a deeply nested configuration loses entries rather than hanging the app.
	hosts, err := readSSHConfigFile(path, maxIncludeDepth+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 0 {
		t.Errorf("reading continued past the include limit: %+v", hosts)
	}
}

func TestPortsAreValidated(t *testing.T) {
	cases := map[string]int{
		"22":     22,
		"2222":   2222,
		"70000":  0, // out of range
		"abc":    0,
		"22 # x": 0, // a trailing comment is not a port
	}
	for input, want := range cases {
		if got := parsePort(input); got != want {
			t.Errorf("parsePort(%q) = %d, want %d", input, got, want)
		}
	}
}
