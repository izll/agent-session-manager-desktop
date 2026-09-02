package session

import (
	"os"
	"path/filepath"
	"testing"
)

// The failure the retry is for: a handle to a .taskmaster that has since been
// replaced. Opening the lock through it fails with IsNotExist even though the
// directory exists — reproduced here rather than argued about.
func TestStaleTaskRootIsRecovered(t *testing.T) {
	project := t.TempDir()
	td := filepath.Join(project, ".taskmaster")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}

	stale, err := openProjectTaskRoot(project, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()

	// Another process replaces the directory under us.
	if err := os.RemoveAll(td); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, openErr := stale.OpenFile("tasks.json.lock", os.O_CREATE|os.O_RDWR, 0o600); openErr == nil {
		t.Skip("this platform tolerates the stale handle; nothing to recover from")
	} else if !os.IsNotExist(openErr) {
		t.Fatalf("unexpected error shape: %v", openErr)
	}

	// What mutateLocked does about it.
	fresh, err := openProjectTaskRoot(project, true)
	if err != nil {
		t.Fatalf("reopening failed: %v", err)
	}
	defer fresh.Close()
	f, err := fresh.OpenFile("tasks.json.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("the reopened handle still cannot lock: %v", err)
	}
	f.Close()
}

// And the whole path works when two managers mutate the same fresh project.
func TestConcurrentFirstMutationOnAFreshProject(t *testing.T) {
	project := t.TempDir()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			m := NewTaskManager(project)
			if err := m.Load(); err != nil {
				done <- err
				return
			}
			_, err := m.CreateTask("t", "", TaskPriorityMedium, nil)
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent first mutation failed: %v", err)
		}
	}
}
