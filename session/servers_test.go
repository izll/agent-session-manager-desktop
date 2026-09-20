package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	return &Storage{
		configDir:  dir,
		configPath: filepath.Join(dir, "sessions.json"),
	}
}

// An empty list rather than an error: a first run has no file, and refusing to
// open the manager until one exists would leave no way to create the first
// entry.
func TestLoadServersWithNoFile(t *testing.T) {
	list, err := testStorage(t).LoadServers()
	if err != nil {
		t.Fatalf("loading an absent list failed: %v", err)
	}
	if len(list.Servers) != 0 {
		t.Errorf("got %d servers from nothing", len(list.Servers))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	storage := testStorage(t)
	saved := &ServerList{Servers: []Server{
		{ID: "a", Name: "Build box", Host: "10.0.0.5", User: "izll", AuthMethod: AuthAgent},
		{ID: "b", Name: "VPS", Host: "vps.example", Port: 2222, User: "root",
			AuthMethod: AuthKey, KeyPath: "/home/izll/.ssh/id_ed25519"},
	}}
	if err := storage.SaveServers(saved); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := storage.LoadServers()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Servers) != 2 {
		t.Fatalf("got %d servers back", len(loaded.Servers))
	}
	if loaded.Servers[1].Port != 2222 || loaded.Servers[1].KeyPath == "" {
		t.Errorf("entry came back changed: %+v", loaded.Servers[1])
	}
}

// The file names hosts and usernames. No other account on the machine has a
// reason to read it.
func TestServerFileIsNotWorldReadable(t *testing.T) {
	// Unix permission bits. Windows does not carry them — a file written with
	// 0600 reports 0666 — so the question has no answer there, and access is
	// controlled by an ACL this test could not read anyway.
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permission bits to check")
	}

	storage := testStorage(t)
	if err := storage.SaveServers(&ServerList{Servers: []Server{
		{ID: "a", Host: "h", User: "u", AuthMethod: AuthAgent},
	}}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(storage.serversPath())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("servers.json mode = %o; readable beyond the owner", mode)
	}
}

// A password must never reach the file. The struct has no field for one, and
// this test is what keeps it that way: adding one would make this fail.
func TestServerFileHoldsNoPassword(t *testing.T) {
	storage := testStorage(t)
	if err := storage.SaveServers(&ServerList{Servers: []Server{
		{ID: "a", Name: "n", Host: "h", User: "u", AuthMethod: AuthPassword},
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(storage.serversPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "password\":") {
		t.Errorf("the saved list contains a password field:\n%s", data)
	}
}

func TestServerValidation(t *testing.T) {
	cases := []struct {
		name   string
		server Server
		wantOK bool
	}{
		{"agent auth", Server{ID: "a", Host: "h", User: "u", AuthMethod: AuthAgent}, true},
		{"key with path", Server{ID: "a", Host: "h", User: "u", AuthMethod: AuthKey, KeyPath: "/k"}, true},
		{"key without path", Server{ID: "a", Host: "h", User: "u", AuthMethod: AuthKey}, false},
		{"no host", Server{ID: "a", User: "u", AuthMethod: AuthAgent}, false},
		{"no user", Server{ID: "a", Host: "h", AuthMethod: AuthAgent}, false},
		{"unknown method", Server{ID: "a", Host: "h", User: "u", AuthMethod: "magic"}, false},
		{"port out of range", Server{ID: "a", Host: "h", User: "u", AuthMethod: AuthAgent, Port: 70000}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.server.Validate()
			if testCase.wantOK && err != nil {
				t.Errorf("rejected a valid entry: %v", err)
			}
			if !testCase.wantOK && err == nil {
				t.Error("accepted an entry that cannot be connected to")
			}
		})
	}
}

// A duplicate id makes two entries indistinguishable to every session that
// names one — the wrong machine would answer.
func TestDuplicateIDsAreRejected(t *testing.T) {
	err := testStorage(t).SaveServers(&ServerList{Servers: []Server{
		{ID: "same", Host: "a", User: "u", AuthMethod: AuthAgent},
		{ID: "same", Host: "b", User: "u", AuthMethod: AuthAgent},
	}})
	if err == nil {
		t.Error("two servers saved under one id")
	}
}

// A jump chain that loops would hang the connection instead of failing, and a
// hang is the hardest thing to diagnose from the outside.
func TestJumpHostLoopsAreRejected(t *testing.T) {
	t.Run("self", func(t *testing.T) {
		err := testStorage(t).SaveServers(&ServerList{Servers: []Server{
			{ID: "a", Host: "a", User: "u", AuthMethod: AuthAgent, JumpHostID: "a"},
		}})
		if err == nil {
			t.Error("a server jumping through itself was accepted")
		}
	})

	t.Run("cycle", func(t *testing.T) {
		err := testStorage(t).SaveServers(&ServerList{Servers: []Server{
			{ID: "a", Host: "a", User: "u", AuthMethod: AuthAgent, JumpHostID: "b"},
			{ID: "b", Host: "b", User: "u", AuthMethod: AuthAgent, JumpHostID: "a"},
		}})
		if err == nil {
			t.Error("a two-server jump loop was accepted")
		}
	})

	t.Run("missing", func(t *testing.T) {
		err := testStorage(t).SaveServers(&ServerList{Servers: []Server{
			{ID: "a", Host: "a", User: "u", AuthMethod: AuthAgent, JumpHostID: "gone"},
		}})
		if err == nil {
			t.Error("a jump through a non-existent server was accepted")
		}
	})

	t.Run("valid chain", func(t *testing.T) {
		err := testStorage(t).SaveServers(&ServerList{Servers: []Server{
			{ID: "bastion", Host: "b", User: "u", AuthMethod: AuthAgent},
			{ID: "inner", Host: "i", User: "u", AuthMethod: AuthAgent, JumpHostID: "bastion"},
		}})
		if err != nil {
			t.Errorf("a legitimate jump host was rejected: %v", err)
		}
	})
}

// Two defaults would make "which server does a new session use" depend on
// iteration order.
func TestOnlyOneDefaultSurvives(t *testing.T) {
	storage := testStorage(t)
	if err := storage.SaveServers(&ServerList{Servers: []Server{
		{ID: "a", Name: "a", Host: "a", User: "u", AuthMethod: AuthAgent, IsDefault: true},
		{ID: "b", Name: "b", Host: "b", User: "u", AuthMethod: AuthAgent, IsDefault: true},
	}}); err != nil {
		t.Fatal(err)
	}

	list, err := storage.LoadServers()
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, srv := range list.Servers {
		if srv.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("%d servers marked default", defaults)
	}
}

// Two edits arriving together must not lose one. UpdateServers exists for
// exactly this, and callers reaching for Load+Save instead is the mistake it
// prevents.
func TestUpdateServersAppliesBothEdits(t *testing.T) {
	storage := testStorage(t)
	if err := storage.SaveServers(&ServerList{Servers: []Server{
		{ID: "a", Name: "first", Host: "a", User: "u", AuthMethod: AuthAgent},
	}}); err != nil {
		t.Fatal(err)
	}

	add := func(id, name string) error {
		return storage.UpdateServers(func(list *ServerList) error {
			list.Servers = append(list.Servers, Server{
				ID: id, Name: name, Host: id, User: "u", AuthMethod: AuthAgent,
			})
			return nil
		})
	}
	if err := add("b", "second"); err != nil {
		t.Fatal(err)
	}
	if err := add("c", "third"); err != nil {
		t.Fatal(err)
	}

	list, err := storage.LoadServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Servers) != 3 {
		t.Errorf("got %d servers; an edit was lost", len(list.Servers))
	}
}

func TestDisplayNameFallsBackToTheAddress(t *testing.T) {
	named := Server{Name: "Build box", Host: "10.0.0.5", User: "izll"}
	if got := named.DisplayName(); got != "Build box" {
		t.Errorf("display name = %q", got)
	}

	unnamed := Server{Host: "10.0.0.5", User: "izll"}
	if got := unnamed.DisplayName(); got != "izll@10.0.0.5" {
		t.Errorf("unnamed server shows as %q", got)
	}
}

func TestAddressFillsInTheDefaultPort(t *testing.T) {
	if got := (&Server{Host: "h"}).Address(); got != "h:22" {
		t.Errorf("address = %q", got)
	}
	if got := (&Server{Host: "h", Port: 2222}).Address(); got != "h:2222" {
		t.Errorf("address = %q", got)
	}
}
