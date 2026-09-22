package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// The path from the UI is handed to an editor rather than read here, so the
// containment check has to happen before we let go of it: an editor given
// "../../etc/passwd" would open it perfectly happily.
func TestEditorPathCannotEscapeTheSessionTree(t *testing.T) {
	root := t.TempDir()
	inst := &session.Instance{Path: root}

	for _, rel := range []string{
		"../outside.txt",
		"../../etc/passwd",
		"sub/../../outside.txt",
	} {
		if _, err := inst.ResolveBrowsePathForEditor(rel); err == nil {
			t.Errorf("%q resolved instead of being rejected", rel)
		}
	}
}

func TestEditorPathRejectsAbsolutePaths(t *testing.T) {
	inst := &session.Instance{Path: t.TempDir()}
	if _, err := inst.ResolveBrowsePathForEditor("/etc/passwd"); err == nil {
		t.Fatal("an absolute path was accepted")
	}
}

func TestEditorPathResolvesAFileInTheTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{Path: root}
	abs, err := inst.ResolveBrowsePathForEditor("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(abs, "a.txt") || !filepath.IsAbs(abs) {
		t.Fatalf("resolved to %q, want an absolute path ending in a.txt", abs)
	}
}

// baseRef reaches git as an argument. It is checked the same way the diff
// itself checks it rather than trusted from the caller.
func TestDiffBaseRejectsAnInvalidRef(t *testing.T) {
	inst := &session.Instance{Path: t.TempDir()}
	for _, ref := range []string{
		"--upload-pack=touch /tmp/x",
		"HEAD; rm -rf /",
		"not-hex",
	} {
		if _, err := inst.WriteDiffBaseToTemp("a.txt", ref); err == nil {
			t.Errorf("ref %q was accepted", ref)
		}
	}
}

// A file added during the session has no "before" side. That is not a failure —
// the diff is the whole file — so an empty side is written rather than an error
// shown.
func TestDiffBaseOfAnAddedFileIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("added"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{Path: root}

	// Not a git repository at all, which is the same shape of failure as a file
	// git has never seen: git writes nothing and exits non-zero.
	target, err := inst.WriteDiffBaseToTemp("new.txt", "")
	if err != nil {
		t.Fatalf("an added file should produce an empty base, got %v", err)
	}
	defer os.RemoveAll(filepath.Dir(target))

	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatalf("base file holds %q, want empty", contents)
	}
	// The name is kept so the editor's tab title and highlighting still work.
	if filepath.Base(target) != "new.txt" {
		t.Fatalf("temp file named %q, want new.txt", filepath.Base(target))
	}
}

// The temporary directory is swept at startup, so it has to be recognisable.
func TestDiffTempDirIsPrefixedForTheSweep(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{Path: root}
	target, err := inst.WriteDiffBaseToTemp("f.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(target))

	if !strings.HasPrefix(filepath.Base(filepath.Dir(target)), session.DiffTempPrefix) {
		t.Fatalf("temp dir %q would not be swept", filepath.Dir(target))
	}
}

// Detection runs once and is cached; the point of the test is that the list is
// ordered and non-empty, so a machine with several editors gets a predictable
// one rather than whichever the map iteration happened to yield.
func TestKnownEditorsAreOrdered(t *testing.T) {
	if len(knownEditors) == 0 {
		t.Fatal("no editors are looked for")
	}
	if knownEditors[0] != "code" {
		t.Fatalf("first choice is %q, want code", knownEditors[0])
	}
}

// Opening a project replaces what the editor currently shows, so unlike the
// per-file jumps it must NOT reuse the window: the file buttons are a
// convenience, this one would be a loss.
func TestFolderOpenDoesNotReuseTheWindow(t *testing.T) {
	body := functionBody(t, readSourceFile(t, "external_editor.go"),
		"func (a *App) OpenFolderInEditor")
	// The call, not the whole body: the comment above it explains why the flag
	// is absent, and matching that made the test fail on its own rationale.
	call := body[strings.Index(body, "return startEditor("):]
	if strings.Contains(call, "--reuse-window") {
		t.Fatal("opening a folder reuses the window, replacing the user's open project")
	}
	if !strings.Contains(body, "resolvedRoot") {
		t.Fatal("the folder open does not use the validated root")
	}
}
