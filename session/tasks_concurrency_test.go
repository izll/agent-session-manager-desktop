package session

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskManagerRejectsNonCooperatingExternalWriter(t *testing.T) {
	project := t.TempDir()
	manager := NewTaskManager(project)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateTask("original", "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
	path := manager.getTaskFilePath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	external := bytes.Replace(before, []byte("original"), []byte("external"), 1)
	manager.mu.Lock()
	err = manager.mutateLocked(func() error {
		manager.store.Tasks[0].Title = "local overwrite"
		if err := os.WriteFile(path, external, 0o644); err != nil {
			return err
		}
		return manager.saveLocked()
	})
	manager.mu.Unlock()
	if !errors.Is(err, ErrTaskStoreConflict) {
		t.Fatalf("save error = %v, want ErrTaskStoreConflict", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, external) {
		t.Fatal("conflicting local save overwrote the external writer")
	}
	tasks := manager.GetTasks()
	if len(tasks) != 1 || tasks[0].Title != "external" {
		t.Fatalf("cache after conflict = %#v; want external store", tasks)
	}
}

func TestTaskManagerCanonicalAliasDoesNotLoseUpdates(t *testing.T) {
	realPath := t.TempDir()
	aliasRoot := t.TempDir()
	aliasPath := filepath.Join(aliasRoot, "repo-alias")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	one := NewTaskManager(realPath)
	two := NewTaskManager(aliasPath)
	if one.projectPath != two.projectPath {
		t.Fatalf("aliases received different canonical identities: %q != %q", one.projectPath, two.projectPath)
	}
	if err := one.Load(); err != nil {
		t.Fatal(err)
	}
	if err := two.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := one.CreateTask("one", "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := two.CreateTask("two", "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
	reloaded := NewTaskManager(realPath)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(reloaded.GetTasks()); got != 2 {
		t.Fatalf("alias RMW lost a task: got %d tasks, want 2", got)
	}
}

func TestTaskManagerCrossProcessRMW(t *testing.T) {
	if os.Getenv("ASMGR_TASK_RMW_HELPER") == "1" {
		runTaskRMWHelper(t)
		return
	}
	project := t.TempDir()
	coord := t.TempDir()
	commands := make([]*exec.Cmd, 2)
	outputs := make([]bytes.Buffer, 2)
	for i := range commands {
		marker := filepath.Join(coord, string(rune('a'+i))+".ready")
		cmd := exec.Command(os.Args[0], "-test.run=^TestTaskManagerCrossProcessRMW$")
		cmd.Env = append(os.Environ(),
			"ASMGR_TASK_RMW_HELPER=1",
			"ASMGR_TASK_RMW_PROJECT="+project,
			"ASMGR_TASK_RMW_MARKER="+marker,
			"ASMGR_TASK_RMW_START="+filepath.Join(coord, "start"),
		)
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[i] = cmd
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, aErr := os.Stat(filepath.Join(coord, "a.ready")); aErr == nil {
			if _, bErr := os.Stat(filepath.Join(coord, "b.ready")); bErr == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("task RMW helpers did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(coord, "start"), []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("task RMW helper failed: %v\n%s", err, outputs[i].String())
		}
	}
	manager := NewTaskManager(project)
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(manager.GetTasks()); got != 2 {
		t.Fatalf("cross-process RMW lost a task: got %d tasks, want 2", got)
	}
}

func runTaskRMWHelper(t *testing.T) {
	manager := NewTaskManager(os.Getenv("ASMGR_TASK_RMW_PROJECT"))
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ASMGR_TASK_RMW_MARKER"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("ASMGR_TASK_RMW_START")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for cross-process start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := manager.CreateTask(filepath.Base(os.Getenv("ASMGR_TASK_RMW_MARKER")), "", TaskPriorityMedium, nil); err != nil {
		t.Fatal(err)
	}
}
