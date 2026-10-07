package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeDeleteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFileForBrowseHoldsAndRestores(t *testing.T) {
	root := t.TempDir()
	hold := filepath.Join(t.TempDir(), "held")
	writeDeleteTestFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(filepath.Join(root, "src", "main.go"), when, when)
	inst := &Instance{Path: root}

	deleted, err := inst.DeleteFileForBrowse("src/main.go", hold)
	if err != nil {
		t.Fatalf("DeleteFileForBrowse: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "src", "main.go")); !os.IsNotExist(err) {
		t.Fatalf("the file is still in the tree: %v", err)
	}
	if got, err := os.ReadFile(deleted.Held); err != nil || string(got) != "package main\n" {
		t.Fatalf("held file = %q, %v", got, err)
	}

	if err := RestoreDeletedFile(deleted); err != nil {
		t.Fatalf("RestoreDeletedFile: %v", err)
	}
	restored := filepath.Join(root, "src", "main.go")
	got, err := os.ReadFile(restored)
	if err != nil || string(got) != "package main\n" {
		t.Fatalf("restored file = %q, %v", got, err)
	}
	info, _ := os.Stat(restored)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %v, want 0640", info.Mode().Perm())
	}
	if !info.ModTime().Equal(when) {
		t.Fatalf("restored mtime = %v, want %v", info.ModTime(), when)
	}
	if _, err := os.Stat(filepath.Dir(deleted.Held)); !os.IsNotExist(err) {
		t.Fatalf("the hold slot was left behind: %v", err)
	}
}

func TestRestoreDeletedFileNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	writeDeleteTestFile(t, filepath.Join(root, "notes.txt"), "old")
	inst := &Instance{Path: root}
	deleted, err := inst.DeleteFileForBrowse("notes.txt", filepath.Join(t.TempDir(), "held"))
	if err != nil {
		t.Fatal(err)
	}
	writeDeleteTestFile(t, filepath.Join(root, "notes.txt"), "new")

	if err := RestoreDeletedFile(deleted); !errors.Is(err, ErrRestoreOccupied) {
		t.Fatalf("RestoreDeletedFile error = %v, want ErrRestoreOccupied", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "notes.txt")); string(got) != "new" {
		t.Fatalf("the newer file was overwritten: %q", got)
	}
	// Still held, so the undo can be retried once the new file is gone.
	if got, _ := os.ReadFile(deleted.Held); string(got) != "old" {
		t.Fatalf("held file = %q, want it kept", got)
	}
}

func TestDiscardDeletedFileRemovesIt(t *testing.T) {
	root := t.TempDir()
	writeDeleteTestFile(t, filepath.Join(root, "a.txt"), "a")
	deleted, err := (&Instance{Path: root}).DeleteFileForBrowse("a.txt", filepath.Join(t.TempDir(), "held"))
	if err != nil {
		t.Fatal(err)
	}
	if err := DiscardDeletedFile(deleted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(deleted.Held)); !os.IsNotExist(err) {
		t.Fatalf("held file survived the discard: %v", err)
	}
}

func TestDeleteFileForBrowseRefuses(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeDeleteTestFile(t, filepath.Join(outside, "secret.txt"), "secret")
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{Path: root}
	hold := filepath.Join(t.TempDir(), "held")

	for _, rel := range []string{"", ".", "..", "../secret.txt", "/etc/passwd", "dir", "missing.txt"} {
		if _, err := inst.DeleteFileForBrowse(rel, hold); err == nil {
			t.Errorf("DeleteFileForBrowse(%q) succeeded, want refusal", rel)
		}
	}
	if runtime.GOOS != "windows" {
		// A parent that is a symlink out of the tree does not let a delete out.
		if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
			t.Fatal(err)
		}
		if _, err := inst.DeleteFileForBrowse("out/secret.txt", hold); err == nil {
			t.Error("deleted through a symlinked parent outside the tree")
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatalf("a file outside the tree was touched: %v", err)
	}
}

func TestDeleteFileForBrowseTakesTheLinkNotItsTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	writeDeleteTestFile(t, filepath.Join(root, "real.txt"), "real")
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	deleted, err := (&Instance{Path: root}).DeleteFileForBrowse("link.txt", filepath.Join(t.TempDir(), "held"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "real.txt")); string(got) != "real" {
		t.Fatalf("the link's target was deleted: %q", got)
	}
	if err := RestoreDeletedFile(deleted); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(root, "link.txt")); err != nil || target != "real.txt" {
		t.Fatalf("restored link = %q, %v", target, err)
	}
}

func TestCopyFileNoClobberRefusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	writeDeleteTestFile(t, filepath.Join(dir, "src"), "src")
	writeDeleteTestFile(t, filepath.Join(dir, "dst"), "dst")
	if err := copyFileNoClobber(filepath.Join(dir, "src"), filepath.Join(dir, "dst")); !os.IsExist(err) {
		t.Fatalf("copyFileNoClobber error = %v, want exists", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "dst")); string(got) != "dst" {
		t.Fatalf("dst overwritten: %q", got)
	}
}

// The path taken when the holding folder is on another disk than the project.
func TestCopyFileNoClobberKeepsModeAndTime(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeDeleteTestFile(t, src, "content")
	when := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	os.Chtimes(src, when, when)
	dst := filepath.Join(dir, "dst")
	if err := copyFileNoClobber(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
	if !info.ModTime().Equal(when) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), when)
	}
	if got, _ := os.ReadFile(dst); string(got) != "content" {
		t.Fatalf("content = %q", got)
	}
}
