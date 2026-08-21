//go:build darwin

package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundleSwapUsesAtomicDarwinExchange(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "Agent Session Manager.app")
	staged := filepath.Join(dir, ".asmgr-stage", "Agent Session Manager.app")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "version"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "version"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := swapBundle(installed, staged, installed); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(installed, "version")); err != nil || string(got) != "new" {
		t.Fatalf("installed bundle after exchange = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(staged, "version")); err != nil || string(got) != "old" {
		t.Fatalf("staged rollback bundle after exchange = %q, %v", got, err)
	}
}
