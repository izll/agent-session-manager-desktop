package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func deleteFileTestApp(t *testing.T) (*App, *session.Instance) {
	t.Helper()
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	inst := &session.Instance{ID: "files-session", Name: "files", Path: workDir, Status: session.StatusStopped}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	return &App{storage: storage, projectLocked: true}, inst
}

func TestDeleteSessionFileUndo(t *testing.T) {
	app, inst := deleteFileTestApp(t)
	path := filepath.Join(inst.Path, "docs", "readme.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	token, err := app.DeleteSessionFile(inst.ID, "docs/readme.md", -1, inst.Path, "")
	if err != nil {
		t.Fatalf("DeleteSessionFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still there after delete: %v", err)
	}
	if !strings.HasPrefix(app.deletedFiles.held[token].file.Held, app.storage.DeletedFilesDir()) {
		t.Fatalf("held outside the deleted files folder: %s", app.deletedFiles.held[token].file.Held)
	}

	if err := app.RestoreDeletedSessionFile(token); err != nil {
		t.Fatalf("RestoreDeletedSessionFile: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "hello" {
		t.Fatalf("restored = %q, %v", got, err)
	}
	if err := app.RestoreDeletedSessionFile(token); err == nil {
		t.Fatal("the same token restored twice")
	}
}

func TestDeleteSessionFileRefusesAnotherRoot(t *testing.T) {
	app, inst := deleteFileTestApp(t)
	path := filepath.Join(inst.Path, "a.txt")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DeleteSessionFile(inst.ID, "a.txt", -1, t.TempDir(), ""); err == nil {
		t.Fatal("deleted against a root the tab is not in")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file touched: %v", err)
	}
}

func TestRestoreDeletedSessionFileKeepsItAfterAFailure(t *testing.T) {
	app, inst := deleteFileTestApp(t)
	path := filepath.Join(inst.Path, "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := app.DeleteSessionFile(inst.ID, "a.txt", -1, inst.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedSessionFile(token); err == nil || !strings.Contains(err.Error(), "exists again") {
		t.Fatalf("restore over a new file: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedSessionFile(token); err != nil {
		t.Fatalf("retry after the new file went: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("restored = %q", got)
	}
}

func TestDiscardDeletedSessionFile(t *testing.T) {
	app, inst := deleteFileTestApp(t)
	if err := os.WriteFile(filepath.Join(inst.Path, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := app.DeleteSessionFile(inst.ID, "a.txt", -1, inst.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	held := app.deletedFiles.held[token].file.Held
	app.DiscardDeletedSessionFile(token)
	if _, err := os.Stat(held); !os.IsNotExist(err) {
		t.Fatalf("held file survived the discard: %v", err)
	}
	if err := app.RestoreDeletedSessionFile(token); err == nil {
		t.Fatal("restored a discarded file")
	}
}

func TestDeletedFilesDiscardedAtShutdown(t *testing.T) {
	app, inst := deleteFileTestApp(t)
	if err := os.WriteFile(filepath.Join(inst.Path, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := app.DeleteSessionFile(inst.ID, "a.txt", -1, inst.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	held := app.deletedFiles.held[token].file.Held
	app.deletedFiles.discardAll()
	if _, err := os.Stat(held); !os.IsNotExist(err) {
		t.Fatalf("held file survived shutdown: %v", err)
	}
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	shutdown := string(source)[strings.Index(string(source), "func (a *App) shutdown("):]
	if !strings.Contains(shutdown[:strings.Index(shutdown, "\n}\n")], "a.deletedFiles.discardAll()") {
		t.Fatal("shutdown does not discard the held files")
	}
}

func TestPurgeStaleDeletedFilesKeepsRecentOnes(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := filepath.Join(dir, "file-stale")
	fresh := filepath.Join(dir, "file-fresh")
	for _, p := range []string{stale, fresh} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-2 * staleDeletedFileAge)
	os.Chtimes(stale, old, old)

	purgeStaleDeletedFiles(dir, now)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale leftover kept: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("another instance's held file removed: %v", err)
	}
}
