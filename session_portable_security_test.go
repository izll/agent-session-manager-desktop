package main

import (
	"os"
	"path/filepath"
	"testing"

	"asmgr-desktop/session"
)

func TestPortableImportUsesValidatedServerSnapshotToken(t *testing.T) {
	storage := guardedTestStorage(t)
	workDir := t.TempDir()
	bundle := &session.PortableBundle{
		Format:   session.PortableFormat,
		Version:  session.PortableVersion,
		Sessions: []session.PortableSession{{Name: "safe", Path: workDir}},
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.WritePortable(file, bundle); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage, projectLocked: true}
	info, err := app.readSessionFileAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Token == "" {
		t.Fatal("validated file did not receive an opaque import token")
	}
	if _, err := app.ImportSessionFile(info.Path, nil); err == nil {
		t.Fatal("frontend-supplied file path was accepted as an import token")
	}
	other, err := storage.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetActiveProject(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ImportSessionFile(info.Token, nil); err == nil {
		t.Fatal("stale import snapshot crossed into another project")
	}
	if err := storage.SetActiveProject(""); err != nil {
		t.Fatal(err)
	}
	count, err := app.ImportSessionFile(info.Token, nil)
	if err != nil || count != 1 {
		t.Fatalf("validated snapshot import = %d, %v", count, err)
	}
	if _, err := app.ImportSessionFile(info.Token, nil); err == nil {
		t.Fatal("successful one-shot import token was reusable")
	}
}
