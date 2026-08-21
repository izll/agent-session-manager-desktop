package mcp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMutateTaskMasterFileRejectsForeignRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	before := []byte(`{"master":{"tasks":[]}}`)
	foreign := []byte(`{"master":{"tasks":[{"id":"foreign"}]}}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	err := MutateTaskMasterFile(path, func(root map[string]interface{}) error {
		root["local"] = true
		return os.WriteFile(path, foreign, 0o600)
	})
	if !errors.Is(err, ErrTaskMasterConflict) {
		t.Fatalf("foreign revision error = %v, want conflict", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(foreign) {
		t.Fatalf("conflict overwrote foreign data: %s", got)
	}
}

func TestMutateTaskMasterFileSerializesAliasesInProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	alias := filepath.Join(t.TempDir(), "tasks-link.json")
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entered := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		done <- MutateTaskMasterFile(path, func(root map[string]interface{}) error {
			entered <- "first"
			<-release
			root["first"] = true
			return nil
		})
	}()
	if got := <-entered; got != "first" {
		t.Fatalf("first callback = %q", got)
	}
	go func() {
		done <- MutateTaskMasterFile(alias, func(root map[string]interface{}) error {
			entered <- "second"
			root["second"] = true
			return nil
		})
	}()
	select {
	case got := <-entered:
		t.Fatalf("alias transaction entered before first released: %q", got)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := <-entered; got != "second" {
		t.Fatalf("second callback = %q", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProviderWriterAndDirectMutationShareLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	providerEntered := make(chan struct{})
	releaseProvider := make(chan struct{})
	providerDone := make(chan error, 1)
	go func() {
		providerDone <- withTaskMasterWriterLock(path, func() error {
			close(providerEntered)
			<-releaseProvider
			return nil
		})
	}()
	<-providerEntered

	directEntered := make(chan struct{})
	directDone := make(chan error, 1)
	go func() {
		directDone <- MutateTaskMasterFile(path, func(root map[string]interface{}) error {
			close(directEntered)
			return nil
		})
	}()
	select {
	case <-directEntered:
		t.Fatal("direct mutation entered while provider writer held the common lock")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseProvider)
	if err := <-providerDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-directEntered:
	case <-time.After(time.Second):
		t.Fatal("direct mutation did not enter after provider writer released the lock")
	}
	if err := <-directDone; err != nil {
		t.Fatal(err)
	}
}

func TestWriterLockDoesNotCreateProviderTreeBeforeInitialize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".taskmaster", "tasks", "tasks.json")
	if err := withTaskMasterWriterLock(path, func() error {
		if _, err := os.Stat(filepath.Join(root, ".taskmaster")); !os.IsNotExist(err) {
			return errors.New("writer lock created provider-owned .taskmaster tree")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMutateTaskMasterFileSerializesProcesses(t *testing.T) {
	if role := os.Getenv("ASMGR_TASKMASTER_LOCK_HELPER"); role != "" {
		runTaskMasterLockHelper(t, role)
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(`{"master":{"tasks":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ready1 := filepath.Join(dir, "ready-first")
	ready2 := filepath.Join(dir, "ready-second")
	entered1 := filepath.Join(dir, "entered-first")
	entered2 := filepath.Join(dir, "entered-second")
	release := filepath.Join(dir, "release")

	start := func(role, ready, entered string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMutateTaskMasterFileSerializesProcesses$")
		cmd.Env = append(os.Environ(),
			"ASMGR_TASKMASTER_LOCK_HELPER="+role,
			"ASMGR_TASKMASTER_LOCK_PATH="+path,
			"ASMGR_TASKMASTER_LOCK_READY="+ready,
			"ASMGR_TASKMASTER_LOCK_ENTERED="+entered,
			"ASMGR_TASKMASTER_LOCK_RELEASE="+release,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start %s helper: %v", role, err)
		}
		return cmd
	}

	first := start("first", ready1, entered1)
	waitForTaskMasterMarker(t, entered1)
	second := start("second", ready2, entered2)
	waitForTaskMasterMarker(t, ready2)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(entered2); err == nil {
		t.Fatal("second process entered transaction while first held the OS lock")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first helper: %v", err)
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("second helper: %v", err)
	}
	waitForTaskMasterMarker(t, entered2)
}

func runTaskMasterLockHelper(t *testing.T, role string) {
	path := os.Getenv("ASMGR_TASKMASTER_LOCK_PATH")
	ready := os.Getenv("ASMGR_TASKMASTER_LOCK_READY")
	entered := os.Getenv("ASMGR_TASKMASTER_LOCK_ENTERED")
	release := os.Getenv("ASMGR_TASKMASTER_LOCK_RELEASE")
	if path == "" || ready == "" || entered == "" {
		t.Fatal("missing helper environment")
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MutateTaskMasterFile(path, func(root map[string]interface{}) error {
		if err := os.WriteFile(entered, nil, 0o600); err != nil {
			return err
		}
		if role == "first" {
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(release); err == nil {
					break
				} else if !os.IsNotExist(err) {
					return err
				}
				if time.Now().After(deadline) {
					return errors.New("timed out waiting for transaction release")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		root[role] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func waitForTaskMasterMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
