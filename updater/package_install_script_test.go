//go:build linux

package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const installScriptSource = "../build/linux/install-update"

func packageChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// runInstallScript runs the script as the test user. Every case here must be
// refused before dpkg/rpm would run, so nothing is ever installed even when
// the tests happen to run as root.
func runInstallScript(t *testing.T, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command("sh", append([]string{installScriptSource}, args...)...).CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

func installStages(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob("/var/tmp/asmgr-update.*")
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]bool{}
	for _, m := range matches {
		stages[m] = true
	}
	return stages
}

// expectRefused runs the script and checks that it failed with its own exit
// code (not pkexec's 126/127, which the updater reports as "not authorised")
// and left no staging directory behind.
func expectRefused(t *testing.T, want string, args ...string) {
	t.Helper()
	before := installStages(t)
	out, code := runInstallScript(t, args...)
	if code != 1 {
		t.Fatalf("exit code %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, want) {
		t.Fatalf("output does not mention %q:\n%s", want, out)
	}
	for stage := range installStages(t) {
		if !before[stage] {
			t.Fatalf("staging directory %s was left behind", stage)
		}
	}
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// buildDeb builds a minimal, unprivileged .deb with the given name and version.
func buildDeb(t *testing.T, name, version string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is not available")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "DEBIAN"), 0o755); err != nil {
		t.Fatal(err)
	}
	control := "Package: " + name + "\nVersion: " + version +
		"\nArchitecture: all\nMaintainer: test\nDescription: test package\n"
	writeFile(t, filepath.Join(root, "DEBIAN", "control"), []byte(control))
	deb := filepath.Join(t.TempDir(), "test.deb")
	if out, err := exec.Command("dpkg-deb", "--root-owner-group", "--build", root, deb).CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v\n%s", err, out)
	}
	data, err := os.ReadFile(deb)
	if err != nil {
		t.Fatal(err)
	}
	return deb, packageChecksum(data)
}

func TestInstallScriptIsShippedWhereTheUpdaterRunsIt(t *testing.T) {
	nfpm, err := os.ReadFile("../build/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	entry := "- src: ./build/linux/install-update\n    dst: " + packageInstallScript + "\n    file_info:\n      mode: 0755\n"
	if !strings.Contains(string(nfpm), entry) {
		t.Fatalf("build/nfpm.yaml does not install the script at %s with mode 0755", packageInstallScript)
	}
	info, err := os.Stat(installScriptSource)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("install-update is not executable in the repository")
	}
	if out, err := exec.Command("sh", "-n", installScriptSource).CombinedOutput(); err != nil {
		t.Fatalf("install-update has a syntax error: %v\n%s", err, out)
	}
}

func TestInstallScriptRejectsMalformedArguments(t *testing.T) {
	pkg := writeFile(t, filepath.Join(t.TempDir(), "p.deb"), []byte("x"))
	sum := packageChecksum([]byte("x"))
	expectRefused(t, "usage", "deb", pkg, sum)
	expectRefused(t, "unsupported package type", "script", pkg, sum, "v1.2.3")
	expectRefused(t, "invalid checksum", "deb", pkg, strings.ToUpper(sum), "v1.2.3")
	expectRefused(t, "invalid checksum", "deb", pkg, sum[:63], "v1.2.3")
	for _, version := range []string{"", "v", "1.2.3-evil", "1..2", ".1.2", "1.2.", "1.2.3;id"} {
		expectRefused(t, "invalid version", "deb", pkg, sum, version)
	}
}

func TestInstallScriptRejectsAPackageThatDoesNotMatchItsChecksum(t *testing.T) {
	pkg := writeFile(t, filepath.Join(t.TempDir(), "p.deb"), []byte("swapped after the download"))
	expectRefused(t, "checksum mismatch", "deb", pkg, packageChecksum([]byte("published")), "v1.2.3")
	expectRefused(t, "cannot copy", "deb", filepath.Join(t.TempDir(), "missing.deb"), packageChecksum(nil), "v1.2.3")
}

func TestInstallScriptRejectsAnotherPackage(t *testing.T) {
	deb, sum := buildDeb(t, "not-asmgr", "1.2.3")
	expectRefused(t, "not an asmgr-desktop package", "deb", deb, sum, "v1.2.3")
}

func TestInstallScriptRejectsAnotherVersionThanAskedFor(t *testing.T) {
	deb, sum := buildDeb(t, BinaryName, "1.2.3")
	expectRefused(t, "is not the expected 9.9.9", "deb", deb, sum, "v9.9.9")
}

func TestInstallScriptRejectsGarbageAsAPackage(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is not available")
	}
	pkg := writeFile(t, filepath.Join(t.TempDir(), "p.deb"), []byte("not a package"))
	expectRefused(t, "not a valid .deb package", "deb", pkg, packageChecksum([]byte("not a package")), "v1.2.3")
}

func TestPackageUpdateExplainsAMissingInstallScript(t *testing.T) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "pkexec"), []byte("#!/bin/sh\nexit 0\n"))
	if err := os.Chmod(filepath.Join(bin, "pkexec"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	previous := packageInstallScript
	t.Cleanup(func() { packageInstallScript = previous })
	packageInstallScript = filepath.Join(t.TempDir(), "install-update")

	err := installPackageUpdate(context.Background(), "v1.2.3", func(action func() error) error {
		t.Fatal("a missing installer reached the critical section")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "is missing") || !strings.Contains(err.Error(), "sudo ") {
		t.Fatalf("error = %v, want the missing installer and a manual command", err)
	}
}

func TestInstallScriptRejectsReinstallingTheInstalledVersion(t *testing.T) {
	installed, err := exec.Command("dpkg-query", "--show", "--showformat=${Version}", BinaryName).Output()
	if err != nil || len(installed) == 0 {
		t.Skip(BinaryName + " is not installed through dpkg here")
	}
	deb, sum := buildDeb(t, BinaryName, string(installed))
	expectRefused(t, "not an upgrade", "deb", deb, sum, "v"+string(installed))
}
