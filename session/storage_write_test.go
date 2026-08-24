package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProjectCatalogKeepsConcurrentEditsAcrossStorages(t *testing.T) {
	dir := t.TempDir()
	storages := []*Storage{{configDir: dir}, {configDir: dir}}
	const count = 40
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for n := 0; n < count; n++ {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := storages[n%len(storages)].AddProject(fmt.Sprintf("project-%02d", n))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	projects, err := storages[0].LoadProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != count {
		t.Fatalf("saved %d projects, want %d", len(projects.Projects), count)
	}
}

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
	// Skipped on Windows: Go reports 0666 for every regular file there, and the
	// protection that matters is the ACL, which Mode() does not describe.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}
}

func TestStorageHardensConfigDirectoryAndProjectCatalogPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "agent-session-manager-desktop")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	storage, err := NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("upgraded config directory mode = %o, want 0700", got)
	}
	if _, err := storage.AddProject("private client project"); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(configDir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("project catalog mode = %o, want 0600", got)
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
	legacyData, err := os.ReadFile(storage.legacyLockPath(""))
	if err != nil || strings.TrimSpace(string(legacyData)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("legacy-compatible lock was not published: data=%q err=%v", legacyData, err)
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

func TestOversizedSparseProjectLockIsReclaimedWithoutUnboundedRead(t *testing.T) {
	storage := newTestStorage(t)
	lockPath := storage.getLockPath("")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := storage.LockProject(""); err != nil {
		t.Fatalf("reclaim oversized corrupt lock: %v", err)
	}
	t.Cleanup(storage.UnlockProject)
	locked, pid := storage.IsProjectLocked("")
	if !locked || pid != os.Getpid() {
		t.Fatalf("reclaimed lock = locked %v pid %d, want this process", locked, pid)
	}
}

// Unlocking removes the file, or the next run inherits a lock nobody holds.
func TestUnlockRemovesTheLockFile(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.LockProject(""); err != nil {
		t.Fatal(err)
	}
	lockPath := storage.getLockPath("")
	legacyPath := storage.legacyLockPath("")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the lock file should exist while held: %v", err)
	}

	storage.UnlockProject()

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("the lock file outlived the unlock")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("the legacy compatibility lock outlived the unlock")
	}
}

func TestLegacyLockPublishFailureRollsBackPrimaryLock(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("legacy rollback")
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := storage.legacyLockPath(project.ID)
	if err := os.MkdirAll(filepath.Join(legacyPath, "cannot-remove"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storage.LockProject(project.ID); err == nil {
		t.Fatal("unpublishable legacy lock unexpectedly succeeded")
	}
	if _, err := os.Stat(storage.getLockPath(project.ID)); !os.IsNotExist(err) {
		t.Fatalf("primary lock survived legacy publish failure: %v", err)
	}
	if storage.lockPath != "" || storage.legacyLockPathHeld != "" {
		t.Fatalf("failed acquisition was recorded as owned: primary=%q legacy=%q", storage.lockPath, storage.legacyLockPathHeld)
	}
}

func TestLockProjectHelperProcess(t *testing.T) {
	if os.Getenv("ASMGR_LOCK_HELPER") != "1" {
		return
	}
	start := os.Getenv("ASMGR_LOCK_START")
	for {
		if _, err := os.Stat(start); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	dir := os.Getenv("ASMGR_LOCK_DIR")
	storage := &Storage{configDir: dir, configPath: filepath.Join(dir, "sessions.json")}
	err := storage.LockProject("")
	result := "error"
	if err == nil {
		result = "acquired"
	} else {
		var locked *ErrProjectLocked
		if errors.As(err, &locked) {
			result = "locked"
		}
	}
	if writeErr := os.WriteFile(os.Getenv("ASMGR_LOCK_RESULT"), []byte(result), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if err == nil {
		time.Sleep(300 * time.Millisecond)
		storage.UnlockProject()
	}
}

// Two independent processes starting at the same instant must never both own
// the project. The lock is published as a fully-written inode via hard link,
// so there is no empty-file interval that the contender can reclaim as stale.
func TestLockProjectIsAtomicAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	results := []string{filepath.Join(dir, "result-a"), filepath.Join(dir, "result-b")}
	commands := make([]*exec.Cmd, 0, 2)
	for _, result := range results {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockProjectHelperProcess$")
		cmd.Env = append(os.Environ(),
			"ASMGR_LOCK_HELPER=1",
			"ASMGR_LOCK_DIR="+dir,
			"ASMGR_LOCK_START="+start,
			"ASMGR_LOCK_RESULT="+result,
		)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	if err := os.WriteFile(start, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("lock helper failed: %v", err)
		}
	}

	acquired := 0
	locked := 0
	for _, result := range results {
		raw, err := os.ReadFile(result)
		if err != nil {
			t.Fatal(err)
		}
		switch string(raw) {
		case "acquired":
			acquired++
		case "locked":
			locked++
		default:
			t.Fatalf("unexpected helper result %q", raw)
		}
	}
	if acquired != 1 || locked != 1 {
		t.Fatalf("acquired=%d locked=%d, want exactly one of each", acquired, locked)
	}
}

func TestRemoveProjectRejectsActiveProject(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("active")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetActiveProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := storage.RemoveProject(project.ID); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("active project deletion error = %v", err)
	}
}

func TestSetActiveProjectRejectsUnknownProjectWithoutCreatingDirectory(t *testing.T) {
	storage := newTestStorage(t)
	unknownID := "proj_unknown"

	if err := storage.SetActiveProject(unknownID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown project switch error = %v, want not found", err)
	}
	if got := storage.GetActiveProjectID(); got != "" {
		t.Fatalf("active project changed to %q after rejected switch", got)
	}
	if _, err := os.Stat(filepath.Join(storage.configDir, "projects", unknownID)); !os.IsNotExist(err) {
		t.Fatalf("unknown project directory was created: %v", err)
	}
}

func TestLoadAllForProjectRejectsUnknownProjectWithoutCreatingDirectory(t *testing.T) {
	storage := newTestStorage(t)
	unknownID := "missing-project"
	if _, _, err := storage.LoadAllForProject(unknownID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("LoadAllForProject error = %v, want not found", err)
	}
	if _, err := os.Stat(filepath.Join(storage.configDir, "projects", unknownID)); !os.IsNotExist(err) {
		t.Fatalf("unknown project read created a ghost directory: %v", err)
	}
}

func TestTemporaryProjectReaderBlocksConcurrentDeletion(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("reader handshake")
	if err != nil {
		t.Fatal(err)
	}
	readerEntered := make(chan struct{})
	releaseReader := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- storage.withTemporaryProjectReader(project.ID, func() error {
			close(readerEntered)
			<-releaseReader
			return nil
		})
	}()
	<-readerEntered

	deleter := &Storage{configDir: storage.configDir}
	finish, err := deleter.beginProjectDeletion(project.ID)
	if finish != nil {
		finish()
	}
	var locked *ErrProjectLocked
	if !errors.As(err, &locked) || locked.PID != os.Getpid() {
		close(releaseReader)
		<-done
		t.Fatalf("deletion entered while a temporary reader was active: %v", err)
	}
	close(releaseReader)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRemoveProjectRejectsProjectLockedByAnotherProcess(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("locked")
	if err != nil {
		t.Fatal(err)
	}
	holder := exec.Command("sleep", "30")
	if err := holder.Start(); err != nil {
		t.Skipf("cannot start lock-holder process: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})
	lockPath := storage.getLockPath(project.ID)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(holder.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	var locked *ErrProjectLocked
	if err := storage.RemoveProject(project.ID); !errors.As(err, &locked) {
		t.Fatalf("locked project deletion error = %v, want ErrProjectLocked", err)
	}
}

func TestRemoveProjectRejectsLiveReadOnlyViewer(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("viewed")
	if err != nil {
		t.Fatal(err)
	}
	viewer := &Storage{configDir: storage.configDir}
	if err := viewer.lockProjectReader(project.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(viewer.UnlockProject)

	var locked *ErrProjectLocked
	if err := storage.RemoveProject(project.ID); !errors.As(err, &locked) || locked.PID != os.Getpid() {
		t.Fatalf("viewed project deletion error = %v, want current viewer PID", err)
	}
	if _, err := storage.GetProject(project.ID); err != nil {
		t.Fatalf("viewed project disappeared after rejected deletion: %v", err)
	}

	viewer.UnlockProject()
	if err := storage.RemoveProject(project.ID); err != nil {
		t.Fatalf("project remained undeletable after viewer left: %v", err)
	}
}

func TestProjectOpenFailsWhileDeletionClaimIsPublished(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("deleting")
	if err != nil {
		t.Fatal(err)
	}
	finish, err := storage.beginProjectDeletion(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()

	viewer := &Storage{configDir: storage.configDir}
	var deleting *ErrProjectDeleting
	if err := viewer.LockProjectForUse(project.ID); !errors.As(err, &deleting) || deleting.PID != os.Getpid() {
		t.Fatalf("open during deletion error = %v, want ErrProjectDeleting", err)
	}
	if viewer.lockPath != "" || viewer.readLockPath != "" {
		t.Fatalf("failed open retained project claims: exclusive=%q reader=%q", viewer.lockPath, viewer.readLockPath)
	}
}

func TestFailedProjectSwitchReleasesPreviousLock(t *testing.T) {
	storage := newTestStorage(t)
	if err := storage.LockProject("one"); err != nil {
		t.Fatal(err)
	}
	oldPath := storage.getLockPath("one")
	holder := exec.Command("sleep", "30")
	if err := holder.Start(); err != nil {
		t.Skipf("cannot start lock-holder process: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})
	targetPath := storage.getLockPath("two")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(strconv.Itoa(holder.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	var locked *ErrProjectLocked
	if err := storage.LockProject("two"); !errors.As(err, &locked) {
		t.Fatalf("target lock error = %v, want ErrProjectLocked", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old project remained locked after failed switch: %v", err)
	}
	if storage.lockPath != "" {
		t.Fatalf("storage still records old lock path %q", storage.lockPath)
	}
}

func TestRemoveProjectRollsBackDataWhenCatalogSaveFails(t *testing.T) {
	storage := newTestStorage(t)
	project, err := storage.AddProject("rollback")
	if err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(storage.configDir, "projects", project.ID)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(projectDir, "sessions.json")
	if err := os.WriteFile(marker, []byte(`{"instances":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Make the catalog commit fail after the data has already moved to trash.
	//
	// A read-only config directory is what does it now: the write stages through
	// CreateTemp, so it cannot be blocked by planting a file at a predictable
	// temp name — which is what this test used to do, and which stopped working
	// the moment the write gained its fsync and a random staging name. Testing
	// the outcome rather than the mechanism keeps it honest either way.
	if err := os.Chmod(storage.configDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(storage.configDir, 0700) })
	if err := storage.RemoveProject(project.ID); err == nil {
		t.Fatal("deletion unexpectedly succeeded despite an unwritable catalog temp path")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("project data was not rolled back: %v", err)
	}
	if _, err := storage.GetProject(project.ID); err != nil {
		t.Fatalf("project disappeared from catalog after failed deletion: %v", err)
	}
}

func TestProjectDeleteRollbackAttemptsEveryMovedEntry(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	trashDir := filepath.Join(root, "trash")
	wantFirst := errors.New("first rollback failure")
	wantSecond := errors.New("second rollback failure")
	var attempted []string
	err := rollbackMovedProjectEntries(projectDir, trashDir, []string{"one", "two", "three"}, func(oldPath, newPath string) error {
		attempted = append(attempted, filepath.Base(oldPath))
		switch filepath.Base(oldPath) {
		case "three":
			return wantFirst
		case "one":
			return wantSecond
		default:
			return nil
		}
	})
	if !errors.Is(err, wantFirst) || !errors.Is(err, wantSecond) {
		t.Fatalf("rollback error = %v, want both failures", err)
	}
	if got := strings.Join(attempted, ","); got != "three,two,one" {
		t.Fatalf("rollback attempts = %s, want three,two,one", got)
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
