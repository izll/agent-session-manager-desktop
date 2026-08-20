package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The write path is what holds every session the user has. A regression here
// does not break a feature — it loses work that cannot be typed back in.
//
// These exercise Storage through its public surface against a throwaway HOME,
// so they run anywhere without a multiplexer.

// A save has to survive being read back. Obvious, and untested until now.
func TestSaveAllRoundTrips(t *testing.T) {
	storage := newTestStorage(t)

	instances := []*Instance{
		{ID: "a", Name: "API", Path: "/tmp/api", Agent: AgentClaude, Status: StatusStopped},
		{ID: "b", Name: "Web", Path: "/tmp/web", Agent: AgentCodex, Status: StatusStopped,
			FollowedWindows: []FollowedWindow{{Index: 2, Name: "Terminal", Agent: AgentTerminal, WorkDir: "/tmp/other"}}},
	}
	groups := []*Group{{ID: "g1", Name: "Backend"}}

	if err := storage.SaveAll(instances, groups, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	loaded, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("instances = %d, want 2", len(loaded))
	}

	// The per-tab working directory is the field three separate bugs turned on;
	// losing it in a round trip would put every one of them back.
	var web *Instance
	for _, inst := range loaded {
		if inst.ID == "b" {
			web = inst
		}
	}
	if web == nil {
		t.Fatal("the second session did not come back")
	}
	if len(web.FollowedWindows) != 1 {
		t.Fatalf("followed windows = %d, want 1", len(web.FollowedWindows))
	}
	if web.FollowedWindows[0].WorkDir != "/tmp/other" {
		t.Errorf("tab work dir = %q, want /tmp/other", web.FollowedWindows[0].WorkDir)
	}
}

// The store can hold an API key, so it must not be world-readable.
func TestStoreIsWrittenOwnerOnly(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "API", Path: "/tmp/api"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(storage.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}
}

// Written through a temp file and renamed, so an interrupted write cannot leave
// a half-file behind. The evidence available afterwards is that no temp file
// survives a successful save.
func TestSaveLeavesNoTempFile(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "API", Path: "/tmp/api"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(storage.configPath + ".tmp"); !os.IsNotExist(err) {
		t.Error("a temp file was left behind after a successful save")
	}
}

// A save must not quietly drop what it does not recognise... within reason:
// the point here is that the file it writes is valid JSON with the expected
// shape, since everything downstream parses it.
func TestSavedFileIsWellFormed(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "API", Path: "/tmp/api"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(storage.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("the store is not valid JSON: %v", err)
	}
	for _, key := range []string{"instances", "settings"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("the store is missing %q", key)
		}
	}
}

// Two saves in a row must both land. Trivial, except that the second one runs
// against a file that already exists, which is where a rename can fail.
func TestSecondSaveReplacesTheFirst(t *testing.T) {
	storage := newTestStorage(t)

	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "First", Path: "/tmp/a"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveAll([]*Instance{{ID: "b", Name: "Second", Path: "/tmp/b"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	loaded, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Second" {
		t.Fatalf("after the second save: %+v", loaded)
	}
}

// The project lock is what stops a second app instance from driving another
// one's terminals. It records the owning PID so a stale file can be told from a
// live holder.
func TestLockProjectRecordsThisProcess(t *testing.T) {
	storage := newTestStorage(t)

	if err := storage.LockProject(""); err != nil {
		t.Fatalf("locking failed: %v", err)
	}
	t.Cleanup(storage.UnlockProject)

	locked, pid := storage.IsProjectLocked("")
	if !locked {
		t.Fatal("the project should report as locked")
	}
	if pid != os.Getpid() {
		t.Errorf("lock pid = %d, want this process (%d)", pid, os.Getpid())
	}
}

// Locking twice from the same process is not a conflict — it is the same owner
// asking again, which happens when a project is reopened.
func TestLockProjectIsIdempotentForTheSameProcess(t *testing.T) {
	storage := newTestStorage(t)

	if err := storage.LockProject(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.UnlockProject)

	if err := storage.LockProject(""); err != nil {
		t.Errorf("re-locking from the same process should be allowed: %v", err)
	}
}

// A lock file naming a process that is gone must not keep a project hostage.
// This is the case that matters after a crash or a power cut.
func TestStaleLockFromADeadProcessIsNotHonoured(t *testing.T) {
	storage := newTestStorage(t)

	// A PID that cannot be running: the maximum is far below this on Linux,
	// and the check treats "no such process" as stale.
	lockPath := storage.getLockPath("")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(4194303)), 0644); err != nil {
		t.Fatal(err)
	}

	if err := storage.LockProject(""); err != nil {
		t.Fatalf("a stale lock should not block: %v", err)
	}
	t.Cleanup(storage.UnlockProject)

	_, pid := storage.IsProjectLocked("")
	if pid != os.Getpid() {
		t.Errorf("the lock should now name this process, got %d", pid)
	}
}

// Unlocking removes the file, or the next run inherits a lock nobody holds.
func TestUnlockRemovesTheLockFile(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.LockProject(""); err != nil {
		t.Fatal(err)
	}
	lockPath := storage.getLockPath("")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the lock file should exist while held: %v", err)
	}

	storage.UnlockProject()

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("the lock file outlived the unlock")
	}
}

// A store the app cannot parse must fail loudly rather than be treated as an
// empty one — that would look like every session vanishing, and the next save
// would make it true.
func TestCorruptStoreDoesNotReadAsEmpty(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "API", Path: "/tmp/api"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storage.configPath, []byte("{ this is not json"), 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := storage.Load()
	if err == nil && len(loaded) == 0 {
		t.Error("a corrupt store read back as an empty one, which invites overwriting it")
	}
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Logf("load reported: %v", err) // any error is acceptable; note the wording
	}
}
