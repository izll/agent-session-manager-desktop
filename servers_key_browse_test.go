package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFileDialogOpensWhereTheKeyIs(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	keys := filepath.Join(home, "keys")
	for _, dir := range []string{ssh, keys} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for current, want := range map[string]string{
		"":                       ssh,
		"   ":                    ssh,
		"~/keys/id_rsa":          keys,
		filepath.Join(keys, "x"): keys,
		"~/gone/id_rsa":          ssh,
		"/no/such/dir/id_rsa":    ssh,
	} {
		if got := keyFileStartDir(current, home); got != want {
			t.Errorf("keyFileStartDir(%q) = %q, want %q", current, got, want)
		}
	}
}

func TestKeyFileDialogFallsBackToHomeWithoutSSHFolder(t *testing.T) {
	home := t.TempDir()
	if got := keyFileStartDir("", home); got != home {
		t.Fatalf("got %q, want %q", got, home)
	}
}

func TestAKeyUnderHomeIsWrittenTheWayTheFieldShows(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "me")
	for path, want := range map[string]string{
		filepath.Join(home, ".ssh", "id_rsa"):                      "~/.ssh/id_rsa",
		filepath.Join(string(filepath.Separator), "etc", "key"):    filepath.Join(string(filepath.Separator), "etc", "key"),
		filepath.Join(string(filepath.Separator), "home", "other"): filepath.Join(string(filepath.Separator), "home", "other"),
	} {
		if got := shortenHome(path, home); got != want {
			t.Errorf("shortenHome(%q) = %q, want %q", path, got, want)
		}
	}
}
