package passwords

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

func newManager(t *testing.T, run Runner) *Manager {
	t.Helper()
	keyring.MockInit()
	manager, err := Open(filepath.Join(t.TempDir(), "passwords.json"), run)
	if err != nil {
		t.Fatal(err)
	}
	manager.findBitwarden = func(context.Context) []string { return []string{bitwardenTool} }
	return manager
}

// bitwardenSource adds an account and returns its source name.
func bitwardenSource(t *testing.T, manager *Manager) string {
	t.Helper()
	id, err := manager.SaveBitwardenAccount(context.Background(), "BW", "")
	if err != nil {
		t.Fatal(err)
	}
	return bitwardenSourcePrefix + id
}

func TestALocalPasswordIsKeptInTheKeyringNotTheFile(t *testing.T) {
	manager := newManager(t, nil)
	id, err := manager.SaveLocal("", " Prod DB ", " root ", "se:cret $1")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := manager.Secret(context.Background(), SourceLocal, id)
	if err != nil || secret != "se:cret $1" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
	data, err := os.ReadFile(manager.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "se:cret") {
		t.Fatalf("the password reached the file: %s", data)
	}
	info, _ := os.Stat(manager.path)
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("mode = %v", info.Mode())
	}
	entries, _ := manager.List(context.Background(), false)
	if len(entries) != 1 || entries[0].Name != "Prod DB" || entries[0].Username != "root" {
		t.Fatalf("entries = %+v", entries)
	}
	// The list survives a restart.
	again, err := Open(manager.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := again.List(context.Background(), false); len(entries) != 1 {
		t.Fatalf("after a restart: %+v", entries)
	}
}

func TestAnEntryDescriptionCarriesNoSecret(t *testing.T) {
	manager := newManager(t, nil)
	if _, err := manager.SaveLocal("", "n", "u", "se:cret"); err != nil {
		t.Fatal(err)
	}
	entries, _ := manager.List(context.Background(), false)
	data, _ := json.Marshal(entries)
	if strings.Contains(string(data), "se:cret") {
		t.Fatalf("listed: %s", data)
	}
}

func TestEditingWithoutAPasswordKeepsIt(t *testing.T) {
	manager := newManager(t, nil)
	ctx := context.Background()
	id, _ := manager.SaveLocal("", "a", "", "first")
	if _, err := manager.SaveLocal(id, "b", "", ""); err != nil {
		t.Fatal(err)
	}
	if secret, _ := manager.Secret(ctx, SourceLocal, id); secret != "first" {
		t.Fatalf("secret = %q", secret)
	}
	if _, err := manager.SaveLocal(id, "b", "", "second"); err != nil {
		t.Fatal(err)
	}
	if secret, _ := manager.Secret(ctx, SourceLocal, id); secret != "second" {
		t.Fatalf("secret = %q", secret)
	}
}

func TestANewEntryNeedsANameAndAPassword(t *testing.T) {
	manager := newManager(t, nil)
	if _, err := manager.SaveLocal("", " ", "", "x"); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := manager.SaveLocal("", "n", "", ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := manager.SaveLocal("nope", "n", "", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletingRemovesTheSecretToo(t *testing.T) {
	manager := newManager(t, nil)
	id, _ := manager.SaveLocal("", "a", "", "x")
	if err := manager.DeleteLocal(id); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(keyringService, keyringAccount(id)); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("secret left in the keyring: %v", err)
	}
	if err := manager.DeleteLocal(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// No keyring means nothing is stored, rather than stored somewhere worse.
func TestWithoutAKeyringNothingIsStored(t *testing.T) {
	manager := newManager(t, nil)
	keyring.MockInitWithError(errors.New("no keyring"))
	t.Cleanup(keyring.MockInit)
	if _, err := manager.SaveLocal("", "a", "", "x"); !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := manager.List(context.Background(), false); len(entries) != 0 {
		t.Fatalf("listed with no secret behind it: %+v", entries)
	}
	if _, err := os.Stat(manager.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file was written: %v", err)
	}
}

// --- KeePassXC, against the real tool ---

func keePassDatabase(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath(keePassXCTool); err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	database := filepath.Join(t.TempDir(), "test.kdbx")
	run := func(stdin string, args ...string) {
		t.Helper()
		command := exec.Command(keePassXCTool, args...)
		command.Stdin = strings.NewReader(stdin)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, output)
		}
	}
	run("mast\nmast\n", "db-create", "-q", "-p", database)
	run("mast\nse:cret $1 \"x\"\nse:cret $1 \"x\"\n", "add", "-q", "-p", database, "Top entry")
	run("mast\n", "mkdir", "-q", database, "Work")
	run("mast\nw2\nw2\n", "add", "-q", "-p", database, "Work/Deep entry")
	return database
}

func TestKeePassXCFromLockedToTyped(t *testing.T) {
	database := keePassDatabase(t)
	manager := newManager(t, nil)
	ctx := context.Background()
	if err := manager.SetKeePassXC(database, ""); err != nil {
		t.Fatal(err)
	}
	// Locked: named as a source, and nothing from it listed.
	if entries, _ := manager.List(ctx, false); len(entries) != 0 {
		t.Fatalf("listed while locked: %+v", entries)
	}
	if err := manager.Unlock(ctx, SourceKeePassXC, "wrong"); !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := manager.Unlock(ctx, SourceKeePassXC, "mast"); err != nil {
		t.Fatal(err)
	}
	entries, failures := manager.List(ctx, false)
	if len(failures) != 0 || len(entries) != 2 || entries[0].Name != "Deep entry" || entries[0].Group != "Work" || entries[1].Name != "Top entry" {
		t.Fatalf("entries = %+v, failures = %v", entries, failures)
	}
	if secret, err := manager.Secret(ctx, SourceKeePassXC, "Top entry"); err != nil || secret != `se:cret $1 "x"` {
		t.Fatalf("secret = %q, %v", secret, err)
	}
	if secret, err := manager.Secret(ctx, SourceKeePassXC, "Work/Deep entry"); err != nil || secret != "w2" {
		t.Fatalf("secret = %q, %v", secret, err)
	}

	manager.Lock()
	if entries, _ := manager.List(ctx, false); len(entries) != 0 {
		t.Fatalf("listed after locking: %+v", entries)
	}
	if _, err := manager.Secret(ctx, SourceKeePassXC, "Top entry"); err == nil {
		t.Fatal("a secret after locking")
	}
}

// Only what the source listed can be asked for.
func TestAnEntryNotListedIsNotFetched(t *testing.T) {
	calls := 0
	manager := newManager(t, func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		calls++
		return []byte("Top\n"), nil, nil
	})
	ctx := context.Background()
	if err := manager.SetKeePassXC("/x.kdbx", ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unlock(ctx, SourceKeePassXC, "m"); err != nil {
		t.Fatal(err)
	}
	before := calls
	for _, id := range []string{"-a", "--help", "Other", ""} {
		if _, err := manager.Secret(ctx, SourceKeePassXC, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", id, err)
		}
	}
	if calls != before {
		t.Fatal("the tool was run for an entry it never listed")
	}
}

// The master password travels on standard input, never on a command line.
func TestKeePassMasterIsNotAnArgument(t *testing.T) {
	var seen []string
	manager := newManager(t, func(_ context.Context, stdin string, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		seen = append(seen, strings.Join(args, " "))
		if stdin != "topsecret\n" {
			t.Errorf("stdin = %q", stdin)
		}
		return []byte("Top\n"), nil, nil
	})
	ctx := context.Background()
	if err := manager.SetKeePassXC("/x.kdbx", "/key.keyx"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unlock(ctx, SourceKeePassXC, "topsecret"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Secret(ctx, SourceKeePassXC, "Top"); err != nil {
		t.Fatal(err)
	}
	for _, args := range seen {
		if strings.Contains(args, "topsecret") {
			t.Fatalf("on the command line: %s", args)
		}
		if !strings.Contains(args, "-k /key.keyx") {
			t.Errorf("no key file in: %s", args)
		}
	}
}

func TestPointingAtAnotherDatabaseLocks(t *testing.T) {
	manager := newManager(t, func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		return []byte("Top\n"), nil, nil
	})
	ctx := context.Background()
	manager.SetKeePassXC("/one.kdbx", "")
	if err := manager.Unlock(ctx, SourceKeePassXC, "m"); err != nil {
		t.Fatal(err)
	}
	manager.SetKeePassXC("/two.kdbx", "")
	if manager.keepass.unlocked() || manager.keepass.master != "" {
		t.Fatal("still open, with the master of another database")
	}
}

func TestTheRecycleBinAndGroupsAreNotEntries(t *testing.T) {
	got := parseKeePassList("Top\nWork/\nWork/Deep\nRecycle Bin/\nRecycle Bin/Old\n\n")
	if len(got) != 2 || got[0].ID != "Top" || got[1].ID != "Work/Deep" {
		t.Fatalf("got = %+v", got)
	}
}

// A broken vault must not hide the rest.
func TestOneFailingSourceLeavesTheOthers(t *testing.T) {
	fail := false
	manager := newManager(t, func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		if fail {
			return nil, nil, fmt.Errorf("broken")
		}
		return []byte("Top\n"), nil, nil
	})
	ctx := context.Background()
	manager.SaveLocal("", "mine", "", "x")
	manager.SetKeePassXC(os.Args[0], "")
	if err := manager.Unlock(ctx, SourceKeePassXC, "m"); err != nil {
		t.Fatal(err)
	}
	fail = true
	entries, failures := manager.List(ctx, true)
	if len(entries) != 1 || entries[0].Name != "mine" || failures[SourceKeePassXC] == "" {
		t.Fatalf("entries = %+v, failures = %v", entries, failures)
	}
}

// --- Bitwarden, against a stand-in: the tool is not installed here ---

type fakeBW struct {
	status   string
	master   string
	unlocked []string
}

func (f *fakeBW) run(_ context.Context, _ string, env []string, name string, args ...string) ([]byte, []byte, error) {
	if name != bitwardenTool {
		return nil, nil, fmt.Errorf("unexpected %s", name)
	}
	f.unlocked = append(f.unlocked, strings.Join(args, " "))
	lookup := func(key string) string {
		for _, entry := range env {
			if value, found := strings.CutPrefix(entry, key+"="); found {
				return value
			}
		}
		return ""
	}
	switch args[0] {
	case "config", "logout":
		return nil, nil, nil
	case "status":
		return []byte(`{"status":"` + f.status + `"}`), nil, nil
	case "unlock":
		if lookup(args[len(args)-1]) != f.master {
			return nil, []byte("Invalid master password."), fmt.Errorf("exit 1")
		}
		return []byte("sess-key\n"), nil, nil
	case "list":
		if lookup(bitwardenSessionEnv) != "sess-key" {
			return nil, nil, fmt.Errorf("locked")
		}
		return []byte(`[{"id":"i1","name":"Prod","type":1,"login":{"username":"bob","password":"se:cret"}},
			{"id":"i2","name":"A note","type":2},{"id":"i3","name":"Card","type":3}]`), nil, nil
	case "get":
		if lookup(bitwardenSessionEnv) != "sess-key" {
			return nil, nil, fmt.Errorf("locked")
		}
		return []byte("se:cret\n"), nil, nil
	}
	return nil, nil, fmt.Errorf("unexpected %v", args)
}

func TestBitwardenFromLockedToTyped(t *testing.T) {
	fake := &fakeBW{status: "locked", master: "mast"}
	manager := newManager(t, fake.run)
	ctx := context.Background()
	source := bitwardenSource(t, manager)

	if err := manager.Unlock(ctx, source, "bad"); !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := manager.Unlock(ctx, source, "mast"); err != nil {
		t.Fatal(err)
	}
	entries, _ := manager.List(ctx, false)
	if len(entries) != 1 || entries[0].Name != "Prod" || entries[0].Username != "bob" {
		t.Fatalf("entries = %+v", entries)
	}
	if data, _ := json.Marshal(entries); strings.Contains(string(data), "se:cret") {
		t.Fatalf("the vault's password reached the list: %s", data)
	}
	if secret, err := manager.Secret(ctx, source, "i1"); err != nil || secret != "se:cret" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
	for _, args := range fake.unlocked {
		if strings.Contains(args, "mast") || strings.Contains(args, "sess-key") {
			t.Fatalf("a secret on the command line: %s", args)
		}
	}
	manager.Lock()
	if _, err := manager.Secret(ctx, source, "i1"); err == nil {
		t.Fatal("a secret after locking")
	}
}

// Logging in can take a second factor, and is the user's to do in a terminal.
func TestBitwardenNotLoggedInSaysSo(t *testing.T) {
	manager := newManager(t, (&fakeBW{status: "unauthenticated"}).run)
	if err := manager.Unlock(context.Background(), bitwardenSource(t, manager), "x"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}

// A timer forgets the master, so nothing lingers after an idle quarter of an hour.
func TestTheLockTimerIsArmedByUse(t *testing.T) {
	manager := newManager(t, (&fakeBW{status: "locked", master: "m"}).run)
	ctx := context.Background()
	source := bitwardenSource(t, manager)
	if err := manager.Unlock(ctx, source, "m"); err != nil {
		t.Fatal(err)
	}
	if manager.timer == nil || autoLock > 30*time.Minute {
		t.Fatalf("no timer, or too long a one: %v", autoLock)
	}
	manager.Lock()
	if manager.timer != nil {
		t.Fatal("the timer outlived the lock")
	}
}

func TestATypedHomePathReachesTheToolExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var seen string
	manager := newManager(t, func(_ context.Context, _ string, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		seen = strings.Join(args, " ")
		return []byte("Top\n"), nil, nil
	})
	manager.SetKeePassXC("~/vault.kdbx", "~/vault.key")
	if err := manager.Unlock(context.Background(), SourceKeePassXC, "m"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seen, "~") || !strings.Contains(seen, home+"/vault.kdbx") || !strings.Contains(seen, home+"/vault.key") {
		t.Fatalf("args = %s", seen)
	}
}

// Measured: without the separator an entry named like an option failed to open.
func TestAnEntryNamedLikeAnOptionIsAPath(t *testing.T) {
	database := keePassDatabase(t)
	command := exec.Command(keePassXCTool, "add", "-q", "-p", "--", database, "-aNotes")
	command.Stdin = strings.NewReader("mast\nzz\nzz\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("this version will not create such an entry: %v: %s", err, output)
	}
	manager := newManager(t, nil)
	ctx := context.Background()
	manager.SetKeePassXC(database, "")
	if err := manager.Unlock(ctx, SourceKeePassXC, "mast"); err != nil {
		t.Fatal(err)
	}
	if secret, err := manager.Secret(ctx, SourceKeePassXC, "-aNotes"); err != nil || secret != "zz" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
}

// Against the real tool, whatever the system's language.
func TestAnEmptyGroupIsNotAnEntry(t *testing.T) {
	database := keePassDatabase(t)
	command := exec.Command(keePassXCTool, "mkdir", "-q", database, "Nothing here")
	command.Stdin = strings.NewReader("mast\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	manager := newManager(t, nil)
	ctx := context.Background()
	manager.SetKeePassXC(database, "")
	if err := manager.Unlock(ctx, SourceKeePassXC, "mast"); err != nil {
		t.Fatal(err)
	}
	entries, _ := manager.List(ctx, false)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
}

// Another window of the app saved something; this one must not write over it.
func TestAnotherWindowsEntriesSurviveASave(t *testing.T) {
	first := newManager(t, nil)
	second, err := Open(first.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveLocal("", "from first", "", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.SaveLocal("", "from second", "", "y"); err != nil {
		t.Fatal(err)
	}
	entries, _ := first.List(context.Background(), false)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
}

// A keyring that will not delete leaves the entry, to be tried again.
func TestAFailedDeleteKeepsTheEntry(t *testing.T) {
	manager := newManager(t, nil)
	id, _ := manager.SaveLocal("", "a", "", "x")
	keyring.MockInitWithError(errors.New("no keyring"))
	t.Cleanup(keyring.MockInit)
	if err := manager.DeleteLocal(id); !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := manager.List(context.Background(), false); len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
}

// The database's password changed under an open source: it says so and locks.
func TestAMasterThatStoppedWorkingLocks(t *testing.T) {
	wrong := false
	manager := newManager(t, func(_ context.Context, _ string, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		if wrong {
			return nil, nil, fmt.Errorf("exit 1")
		}
		return []byte("Top\n"), nil, nil
	})
	ctx := context.Background()
	manager.SetKeePassXC(os.Args[0], "")
	if err := manager.Unlock(ctx, SourceKeePassXC, "m"); err != nil {
		t.Fatal(err)
	}
	wrong = true
	if _, err := manager.Secret(ctx, SourceKeePassXC, "Top"); !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("err = %v", err)
	}
	if manager.keepass.unlocked() || manager.keepass.master != "" {
		t.Fatal("still open")
	}
}

// A keyring prompt nobody answers must not hold the dialog or the vaults.
func TestAKeyringThatDoesNotAnswerGivesUpInTime(t *testing.T) {
	manager := newManager(t, nil)
	id, _ := manager.SaveLocal("", "a", "", "x")
	release := make(chan struct{})
	original := keyringGet
	keyringGet = func(string, string) (string, error) {
		<-release
		return "", keyring.ErrNotFound
	}
	t.Cleanup(func() {
		close(release)
		keyringGet = original
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := manager.Secret(ctx, SourceLocal, id)
		finished <- err
	}()
	manager.Lock()
	select {
	case err := <-finished:
		if !errors.Is(err, ErrNoKeyring) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting for the keyring")
	}
}

// Two windows saving at once: each reads the file, changes it and writes it back, so
// without a lock between them one window's change is written over.
func TestWindowsSavingAtOnceLoseNothing(t *testing.T) {
	first := newManager(t, nil)
	second, err := Open(first.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	const count = 30
	ids := make([]string, count)
	for n := range ids {
		if ids[n], err = first.SaveLocal("", "old", "", "x"); err != nil {
			t.Fatal(err)
		}
	}
	// Several rounds, as on Windows it takes a read and a save meeting at the wrong
	// moment, which one round of this catches only about every other time.
	for round := range 8 {
		name := fmt.Sprintf("name %d", round)
		var wg sync.WaitGroup
		for n, id := range ids {
			manager := first
			if n%2 == 1 {
				manager = second
			}
			wg.Add(1)
			go func(manager *Manager, id string) {
				defer wg.Done()
				if _, err := manager.SaveLocal(id, name, "", ""); err != nil {
					t.Error(err)
				}
			}(manager, id)
		}
		wg.Wait()
		entries, _ := first.List(context.Background(), false)
		if len(entries) != count {
			t.Fatalf("entries = %d", len(entries))
		}
		for _, entry := range entries {
			if entry.Name != name {
				t.Fatalf("a change was lost: %+v", entry)
			}
		}
	}
}

func TestTheFlatpakBitwardenIsUsedThroughFlatpak(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seen [][]string
	var envs [][]string
	manager := newManager(t, func(_ context.Context, _ string, env []string, name string, args ...string) ([]byte, []byte, error) {
		seen = append(seen, append([]string{name}, args...))
		envs = append(envs, env)
		return []byte(`{"status":"unauthenticated"}`), nil, nil
	})
	manager.findBitwarden = func(context.Context) []string {
		return []string{"flatpak", "run", "--command=bw", bitwardenFlatpak}
	}
	ctx := context.Background()
	id, err := manager.SaveBitwardenAccount(ctx, "Work", "https://vault.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen[0], " "); got != "flatpak run --command=bw com.bitwarden.desktop config server https://vault.example.com --nointeraction" {
		t.Fatalf("ran %q", got)
	}
	// A Flatpak's bw can only write inside its own folder.
	home, _ := os.UserHomeDir()
	want := bitwardenDataDirEnv + "=" + filepath.Join(home, ".var/app", bitwardenFlatpak, "config/asmgr-accounts", id)
	if len(envs[0]) != 1 || envs[0][0] != want {
		t.Fatalf("env = %v, want %s", envs[0], want)
	}
	info, err := os.Stat(strings.TrimPrefix(want, bitwardenDataDirEnv+"="))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no such bits to read: there the folder is under the user's profile.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("data dir mode = %v", info.Mode())
	}
	if err := manager.Unlock(ctx, bitwardenSourcePrefix+id, "m"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}

// Two servers, as a company running its own has. Measured with the real bw: one data
// directory holds one server and one account.
func TestTwoAccountsKeepApartEverything(t *testing.T) {
	sessions := map[string]string{}
	lookup := func(env []string, key string) string {
		for _, entry := range env {
			if value, found := strings.CutPrefix(entry, key+"="); found {
				return value
			}
		}
		return ""
	}
	manager := newManager(t, func(_ context.Context, _ string, env []string, _ string, args ...string) ([]byte, []byte, error) {
		dir := lookup(env, bitwardenDataDirEnv)
		if dir == "" {
			return nil, nil, fmt.Errorf("no data dir")
		}
		switch args[0] {
		case "status":
			return []byte(`{"status":"locked"}`), nil, nil
		case "unlock":
			sessions[dir] = "key-" + filepath.Base(dir)
			return []byte(sessions[dir]), nil, nil
		case "list":
			if lookup(env, bitwardenSessionEnv) != sessions[dir] {
				return nil, nil, fmt.Errorf("wrong session for this directory")
			}
			return []byte(`[{"id":"i1","name":"Item ` + filepath.Base(dir) + `","type":1,"login":{}}]`), nil, nil
		case "get":
			if lookup(env, bitwardenSessionEnv) != sessions[dir] {
				return nil, nil, fmt.Errorf("wrong session for this directory")
			}
			return []byte("pw-" + filepath.Base(dir)), nil, nil
		}
		return nil, nil, nil
	})
	ctx := context.Background()
	first, err := manager.SaveBitwardenAccount(ctx, "A", "https://a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.SaveBitwardenAccount(ctx, "B", "https://b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Unlock(ctx, bitwardenSourcePrefix+first, "m"); err != nil {
		t.Fatal(err)
	}
	statuses := manager.Statuses(ctx)
	if len(statuses) != 4 || statuses[2].Locked || !statuses[3].Locked || statuses[3].Detail != "https://b.example.com" || statuses[3].Name != "B" {
		t.Fatalf("statuses = %+v", statuses)
	}
	if err := manager.Unlock(ctx, bitwardenSourcePrefix+second, "m"); err != nil {
		t.Fatal(err)
	}
	entries, failures := manager.List(ctx, true)
	if len(entries) != 2 || len(failures) != 0 {
		t.Fatalf("entries = %+v, failures = %v", entries, failures)
	}
	// What the picker asks for next is by source, so each entry must name its own.
	for _, entry := range entries {
		if entry.Source != bitwardenSourcePrefix+strings.TrimPrefix(entry.Name, "Item ") {
			t.Fatalf("entry = %+v", entry)
		}
	}
	// The same id on two servers: the source says which.
	for _, id := range []string{first, second} {
		secret, err := manager.Secret(ctx, bitwardenSourcePrefix+id, "i1")
		if err != nil || secret != "pw-"+id {
			t.Fatalf("secret = %q, %v", secret, err)
		}
	}
	manager.Lock()
	if _, err := manager.Secret(ctx, bitwardenSourcePrefix+first, "i1"); err == nil {
		t.Fatal("a secret after locking")
	}
}

func TestABadServerAddressIsRefused(t *testing.T) {
	manager := newManager(t, (&fakeBW{}).run)
	for _, address := range []string{"vault.example.com", "ftp://x", "https://", "https://me:pw@x.example.com", "-x"} {
		if _, err := manager.SaveBitwardenAccount(context.Background(), "A", address); !errors.Is(err, ErrBadServerURL) {
			t.Errorf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"", "https://vault.example.com", "http://10.0.0.5:8080"} {
		if _, err := manager.SaveBitwardenAccount(context.Background(), "A", address); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
}

// A server bw would not accept leaves nothing behind that looks set up.
func TestAFailedServerSetupSavesNothing(t *testing.T) {
	manager := newManager(t, func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		return nil, nil, fmt.Errorf("exit 1")
	})
	if _, err := manager.SaveBitwardenAccount(context.Background(), "A", "https://x.example.com"); !errors.Is(err, ErrToolFailed) {
		t.Fatalf("err = %v", err)
	}
	if len(manager.cfg.Bitwarden) != 0 {
		t.Fatalf("saved: %+v", manager.cfg.Bitwarden)
	}
	if entries, _ := os.ReadDir(filepath.Join(filepath.Dir(manager.path), "bitwarden")); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestDeletingAnAccountLogsOutAndRemovesItsFolder(t *testing.T) {
	var ran []string
	manager := newManager(t, func(_ context.Context, _ string, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		ran = append(ran, args[0])
		return nil, nil, fmt.Errorf("offline")
	})
	id := uuid.NewString()
	dir := bitwardenDataDir(manager.path, id, []string{"bw"})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.change(func() { manager.cfg.Bitwarden = append(manager.cfg.Bitwarden, BitwardenAccount{ID: id, Name: "A"}) }); err != nil {
		t.Fatal(err)
	}
	// Offline, so the logout fails, and that must not keep the account.
	if err := manager.DeleteBitwardenAccount(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "logout" {
		t.Fatalf("ran = %v", ran)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the folder is still there")
	}
	if len(manager.cfg.Bitwarden) != 0 {
		t.Fatalf("still listed: %+v", manager.cfg.Bitwarden)
	}
}

// bw may have been installed another way since: neither folder stays behind.
func TestDeletingAnAccountRemovesBothPossibleFolders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := newManager(t, (&fakeBW{}).run)
	flatpak := []string{"flatpak", "run", "--command=bw", bitwardenFlatpak}
	manager.findBitwarden = func(context.Context) []string { return flatpak }
	id := uuid.NewString()
	folders := []string{bitwardenDataDir(manager.path, id, flatpak), bitwardenDataDir(manager.path, id, []string{"bw"})}
	for _, folder := range folders {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.DeleteBitwardenAccount(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	for _, folder := range folders {
		if _, err := os.Stat(folder); err == nil {
			t.Fatalf("%s is still there", folder)
		}
	}
}

// The id goes into a path that is removed.
func TestAnIDThatIsNotOursRemovesNothing(t *testing.T) {
	manager := newManager(t, (&fakeBW{}).run)
	keep := filepath.Join(filepath.Dir(manager.path), "keep")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteBitwardenAccount(context.Background(), "../keep"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
}

func TestNoBitwardenAnywhereIsNotInstalled(t *testing.T) {
	manager := newManager(t, nil)
	manager.findBitwarden = func(context.Context) []string { return nil }
	if manager.BitwardenInstalled(context.Background()) {
		t.Fatal("installed without a tool")
	}
	if _, err := manager.SaveBitwardenAccount(context.Background(), "A", ""); !errors.Is(err, ErrToolFailed) {
		t.Fatalf("err = %v", err)
	}
}

// Measured on the Flatpak's first run, which announces the file it creates.
func TestAFirstRunsNoteDoesNotHideTheStatus(t *testing.T) {
	manager := newManager(t, func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		return []byte("Could not find data file; creating it instead.\n{\"status\":\"unauthenticated\"}\n"), nil, nil
	})
	if err := manager.Unlock(context.Background(), bitwardenSource(t, manager), "m"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommandsThatWouldMisbehaveAreRefused(t *testing.T) {
	for name, pair := range map[string][2]string{
		"empty list":          {"", "tool {name}"},
		"empty get":           {"tool", ""},
		"open quote":          {"tool 'x", "tool {name}"},
		"name in list":        {"tool {name}", "tool {name}"},
		"name inside a shell": {"tool", "sh -c 'show {name}'"},
		"name inside a word":  {"tool", "tool --entry={name}"},
		"name as the program": {"tool", "{name} show"},
		// Passing the whole-word rule once must not excuse the shell code beside it.
		"name in both places": {"tool", "sh -c 'show {name}' {name}"},
		"no name at all":      {"tool", "tool show"},
	} {
		if err := checkCustomCommands(pair[0], pair[1]); !errors.Is(err, ErrBadCommand) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, pair := range map[string][2]string{
		"plain":       {"gopass ls --flat", "gopass show -o {name}"},
		"quoted path": {`"/opt/my tool/x" ls`, `"/opt/my tool/x" get {name}`},
		"through env": {"tool", `sh -c 'show "$ASMGR_SECRET_NAME"'`},
	} {
		if err := checkCustomCommands(pair[0], pair[1]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestACommandIsReadIntoWords(t *testing.T) {
	got, err := splitCommand(`  "C:\\Program Files\\t.exe"  -a 'b c' d""e "" `)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\\Program Files\\t.exe`, "-a", "b c", "de", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestABatchFileIsRecognised(t *testing.T) {
	for program, want := range map[string]bool{`C:\t\x.BAT`: true, "x.cmd": true, "x.exe": false, "x": false} {
		if goesThroughCmd(program) != want {
			t.Errorf("%s", program)
		}
	}
}

func TestSavingACustomSourceKeepsWhatOtherWindowsSaved(t *testing.T) {
	first := newManager(t, nil)
	second, err := Open(first.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.SaveCustom("", "A", "t ls", "t get {name}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.SaveCustom("", "B", "t ls", "t get {name}"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveCustom(a, "A2", "t ls", "t get {name}"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, status := range first.Statuses(context.Background()) {
		if status.Name != "" {
			names = append(names, status.Name)
		}
	}
	if strings.Join(names, ",") != "A2,B" {
		t.Fatalf("names = %v", names)
	}
	if err := first.DeleteCustom(a); err != nil {
		t.Fatal(err)
	}
	if len(first.cfg.Custom) != 1 || first.cfg.Custom[0].Name != "B" {
		t.Fatalf("custom = %+v", first.cfg.Custom)
	}
}

func TestACustomSourcesToolIsCheckedAndNamed(t *testing.T) {
	manager := newManager(t, nil)
	if _, err := manager.SaveCustom("", "Gone", "no-such-tool-here ls", "no-such-tool-here get {name}"); err != nil {
		t.Fatal(err)
	}
	statuses := manager.Statuses(context.Background())
	last := statuses[len(statuses)-1]
	if last.Available || last.Tool != "no-such-tool-here" || last.GetCommand == "" || !last.Enabled || last.Locked {
		t.Fatalf("status = %+v", last)
	}
}

// The key bw prints on success must not stay in the tab's scrollback, its questions and
// errors must still reach the person answering them, and only success ends the shell.
// Measured with the real bw.
func TestTheLoginLineKeepsTheKeyOffTheScreen(t *testing.T) {
	flatpak := []string{"flatpak", "run", "--command=bw", bitwardenFlatpak}
	got := bitwardenLoginLine(flatpak, "/home/a b/it's", "linux")
	want := `env BITWARDENCLI_APPDATA_DIR='/home/a b/it'\''s' flatpak run --command=bw com.bitwarden.desktop login > /dev/null && exit`
	if got != want {
		t.Fatalf("line = %s", got)
	}
	got = bitwardenLoginLine([]string{"bw"}, `C:\u\d`, "windows")
	if got != `cmd /c "set "BITWARDENCLI_APPDATA_DIR=C:\u\d"&& bw login --quiet"` {
		t.Fatalf("line = %s", got)
	}
	if strings.Join(flatpak, " ") != "flatpak run --command=bw "+bitwardenFlatpak {
		t.Fatalf("command changed: %q", flatpak)
	}
}

func TestALoginLineIsForAnAccountThatExists(t *testing.T) {
	manager := newManager(t, (&fakeBW{}).run)
	if _, err := manager.BitwardenLoginLine(context.Background(), uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	id, err := manager.SaveBitwardenAccount(context.Background(), "A", "")
	if err != nil {
		t.Fatal(err)
	}
	line, err := manager.BitwardenLoginLine(context.Background(), id)
	if err != nil || !strings.Contains(line, filepath.Join("bitwarden", id)) {
		t.Fatalf("line = %q, %v", line, err)
	}
}
