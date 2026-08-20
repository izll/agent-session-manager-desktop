package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		current, latest string
		want            int
	}{
		{"0.9.0", "0.10.0", 1},
		{"1.2.3", "1.2.3", 0},
		{"2.0.0", "1.99.99", -1},
		{"1.0.0-alpha", "1.0.0", 1},
		{"1.0.0-alpha.2", "1.0.0-alpha.10", 1},
		{"999999999999999999999.0.0", "1000000000000000000000.0.0", 1},
	}
	for _, tt := range tests {
		current, ok := parseSemver(tt.current)
		if !ok {
			t.Fatalf("parseSemver(%q) failed", tt.current)
		}
		latest, ok := parseSemver(tt.latest)
		if !ok {
			t.Fatalf("parseSemver(%q) failed", tt.latest)
		}
		got := compareSemver(latest, current)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != tt.want {
			t.Errorf("compare %s -> %s = %d, want %d", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestParseSemverRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "1", "1.2", "1.02.3", "1.2.3-01", "1.2.3+", "latest", "v1.2.3.4"} {
		if _, ok := parseSemver(value); ok {
			t.Errorf("parseSemver(%q) unexpectedly succeeded", value)
		}
	}
}

func TestCheckForUpdateUsesSemanticVersioning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.10.0"}`))
	}))
	defer server.Close()

	oldBase, oldClient := apiBaseURL, checkClient
	apiBaseURL = server.URL
	checkClient = server.Client()
	defer func() { apiBaseURL, checkClient = oldBase, oldClient }()

	if got := CheckForUpdate("0.9.0"); got != "v0.10.0" {
		t.Fatalf("CheckForUpdate returned %q", got)
	}
}

func TestDownloadVerifiedAsset(t *testing.T) {
	content := []byte("verified release artifact")
	sum := sha256.Sum256(content)
	filename := "asmgr-desktop_1.2.3_linux_x86_64.deb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case filename:
			_, _ = w.Write(content)
		case filename + ".sha256":
			_, _ = fmt.Fprintf(w, "%x  %s\n", sum, filename)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase, oldClient := downloadBaseURL, downloadClient
	downloadBaseURL = server.URL
	downloadClient = &http.Client{Timeout: time.Second}
	defer func() { downloadBaseURL, downloadClient = oldBase, oldClient }()

	path, err := downloadVerifiedAsset("v1.2.3", filename, "asmgr-test-*.deb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded %q, want %q", got, content)
	}
}

func TestDownloadVerifiedAssetRejectsChecksumMismatch(t *testing.T) {
	filename := "asset.tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == filename+".sha256" {
			_, _ = fmt.Fprintf(w, "%064x  %s\n", 0, filename)
			return
		}
		_, _ = w.Write([]byte("tampered"))
	}))
	defer server.Close()

	oldBase, oldClient := downloadBaseURL, downloadClient
	downloadBaseURL = server.URL
	downloadClient = server.Client()
	defer func() { downloadBaseURL, downloadClient = oldBase, oldClient }()

	if _, err := downloadVerifiedAsset("1.2.3", filename, "asmgr-test-*"); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

func TestExtractExecutableForEveryReleaseLayout(t *testing.T) {
	for _, expected := range []string{
		"asmgr-desktop",
		"asmgr-desktop.exe",
		"asmgr-desktop.app/Contents/MacOS/asmgr-desktop",
	} {
		t.Run(expected, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "release.tar.gz")
			writeTestArchive(t, archive, expected, []byte("executable"))
			staged, err := os.CreateTemp(t.TempDir(), "staged-*")
			if err != nil {
				t.Fatal(err)
			}
			destination := staged.Name()
			if err := extractExecutable(archive, expected, staged); err != nil {
				t.Fatal(err)
			}
			if err := staged.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "executable" {
				t.Fatalf("extracted %q", got)
			}
		})
	}
}

func writeTestArchive(t *testing.T, path, name string, content []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// buildArchive writes a .tar.gz containing the named entries, for the sidecar
// tests below.
func buildArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "release.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// The libraries beside the executable have to be refreshed from the same
// archive, or a new binary runs against stale ones and may not start at all.
func TestUpdateSidecarDLLsReplacesEveryLibrary(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"asmgr-desktop.exe":  "new binary",
		"./libportaudio.dll": "new portaudio",
		"./libstdc++-6.dll":  "new stdc++",
	})

	dest := t.TempDir()
	for _, name := range []string{"libportaudio.dll", "libstdc++-6.dll"} {
		if err := os.WriteFile(filepath.Join(dest, name), []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := updateSidecarDLLs(archive, dest); err != nil {
		t.Fatalf("updateSidecarDLLs: %v", err)
	}

	for name, want := range map[string]string{
		"libportaudio.dll": "new portaudio",
		"libstdc++-6.dll":  "new stdc++",
	} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	// The executable is handled separately; this step must not drop it here.
	if _, err := os.Stat(filepath.Join(dest, "asmgr-desktop.exe")); !os.IsNotExist(err) {
		t.Fatal("the executable must not be written by the sidecar step")
	}
}

// A library present in the archive but not yet installed must still land, or a
// newly introduced dependency would be missing after an update.
func TestUpdateSidecarDLLsInstallsNewLibrary(t *testing.T) {
	archive := buildArchive(t, map[string]string{"./libnew.dll": "fresh"})
	dest := t.TempDir()

	if err := updateSidecarDLLs(archive, dest); err != nil {
		t.Fatalf("updateSidecarDLLs: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "libnew.dll"))
	if err != nil {
		t.Fatalf("new library was not installed: %v", err)
	}
	if string(got) != "fresh" {
		t.Fatalf("libnew.dll = %q, want %q", got, "fresh")
	}
}

// An archive entry must never escape the install directory, whatever its name.
func TestUpdateSidecarDLLsIgnoresPathTraversal(t *testing.T) {
	archive := buildArchive(t, map[string]string{"../../evil.dll": "pwned"})
	dest := t.TempDir()
	outside := filepath.Join(filepath.Dir(filepath.Dir(dest)), "evil.dll")
	_ = os.Remove(outside)

	if err := updateSidecarDLLs(archive, dest); err != nil {
		t.Fatalf("updateSidecarDLLs: %v", err)
	}
	if _, err := os.Stat(outside); err == nil {
		_ = os.Remove(outside)
		t.Fatal("archive entry escaped the destination directory")
	}
	// It should have been written under its base name instead.
	if _, err := os.Stat(filepath.Join(dest, "evil.dll")); err != nil {
		t.Fatalf("entry was not confined to the destination: %v", err)
	}
}

// An oversized entry is a malformed or hostile archive and must be refused
// rather than written to disk.
func TestUpdateSidecarDLLsRejectsOversizedEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	// Declare a size past the limit without writing the bytes.
	if err := tw.WriteHeader(&tar.Header{
		Name: "huge.dll", Mode: 0644, Size: BinaryLimit + 1, Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	if err := updateSidecarDLLs(p, t.TempDir()); err == nil {
		t.Fatal("an entry past BinaryLimit must be rejected")
	}
}

// Non-library entries must be left alone: the executable has its own path, and
// anything else in the archive is not ours to scatter into the install dir.
func TestUpdateSidecarDLLsIgnoresNonLibraries(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"README.md":     "docs",
		"asmgr-desktop": "linux binary",
		"./libgood.dll": "yes",
	})
	dest := t.TempDir()

	if err := updateSidecarDLLs(archive, dest); err != nil {
		t.Fatalf("updateSidecarDLLs: %v", err)
	}
	for _, name := range []string{"README.md", "asmgr-desktop"} {
		if _, err := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not have been written", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "libgood.dll")); err != nil {
		t.Fatalf("the library should have been written: %v", err)
	}
}

// The .old copies an update leaves behind must be cleared on the next start,
// and nothing else may be touched.
func TestCleanStaleUpdateFilesRemovesOnlyOldCopies(t *testing.T) {
	dir := t.TempDir()
	files := map[string]bool{ // name -> should survive
		"asmgr-desktop.exe":     true,
		"libportaudio.dll":      true,
		"asmgr-desktop.exe.old": false,
		"libportaudio.dll.old":  false,
		"notes.old.txt":         true, // .old must be the suffix, not a substring
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cleanStaleUpdateFilesIn(dir)

	for name, shouldSurvive := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if shouldSurvive && err != nil {
			t.Errorf("%s was removed but should have been kept", name)
		}
		if !shouldSurvive && err == nil {
			t.Errorf("%s survived but should have been removed", name)
		}
	}
}

// The Windows update must stay silent: no installer, no console window, no
// elevation prompt — the same in-place file swap Linux does. Package-manager
// handling is the only path that shells out, and it must never be taken here.
func TestPackageManagedIsLinuxOnly(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this asserts the behaviour on non-Linux platforms")
	}
	if IsPackageManaged() {
		t.Fatal("IsPackageManaged must be false off Linux, or the update would try to shell out to a package manager")
	}
}

// A bundle is found by walking up to the .app, not by assuming three levels:
// a binary run out of a build directory has no bundle and must not have
// unrelated parent directories mistaken for one.
func TestBundleRootFor(t *testing.T) {
	tests := []struct {
		name string
		exec string
		want string
	}{
		{
			name: "standard bundle layout",
			exec: "/Applications/asmgr-desktop.app/Contents/MacOS/asmgr-desktop",
			want: "/Applications/asmgr-desktop.app",
		},
		{
			name: "bundle in a user directory",
			exec: "/Users/x/Apps/Foo.app/Contents/MacOS/foo",
			want: "/Users/x/Apps/Foo.app",
		},
		{
			name: "loose binary has no bundle",
			exec: "/usr/local/bin/asmgr-desktop",
			want: "",
		},
		{
			name: "build directory is not a bundle",
			exec: "/home/x/project/build/bin/asmgr-desktop",
			want: "",
		},
		{
			// Deeper than the fixed layout: still not this app's bundle.
			name: "app far above the executable is not claimed",
			exec: "/Applications/Foo.app/Contents/Resources/a/b/c/tool",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bundleRootFor(tt.exec); got != tt.want {
				t.Fatalf("bundleRootFor(%q) = %q, want %q", tt.exec, got, tt.want)
			}
		})
	}
}

// The whole bundle has to come out of the archive: replacing only the binary
// would leave resources and the code signature describing the old version.
func TestExtractBundleRestoresTheTree(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"asmgr-desktop.app/Contents/MacOS/asmgr-desktop":     "binary",
		"asmgr-desktop.app/Contents/Info.plist":              "plist",
		"asmgr-desktop.app/Contents/Frameworks/libfoo.dylib": "dylib",
	})
	dest := t.TempDir()

	if err := extractBundle(archive, dest); err != nil {
		t.Fatalf("extractBundle: %v", err)
	}
	for rel, want := range map[string]string{
		"asmgr-desktop.app/Contents/MacOS/asmgr-desktop":     "binary",
		"asmgr-desktop.app/Contents/Info.plist":              "plist",
		"asmgr-desktop.app/Contents/Frameworks/libfoo.dylib": "dylib",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
}

// The executable bit has to survive, or the extracted bundle cannot be run.
func TestExtractBundleKeepsTheExecutableBit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "binary"
	if err := tw.WriteHeader(&tar.Header{
		Name: "Foo.app/Contents/MacOS/foo", Mode: 0755,
		Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	dest := t.TempDir()
	if err := extractBundle(p, dest); err != nil {
		t.Fatalf("extractBundle: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dest, "Foo.app", "Contents", "MacOS", "foo"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0100 == 0 {
		t.Fatalf("mode is %v; the executable bit was lost", fi.Mode().Perm())
	}
}

// An archive entry must never write outside the destination. The checksum
// proves the file matches the release, not that the release is well-formed.
func TestExtractBundleRefusesPathTraversal(t *testing.T) {
	archive := buildArchive(t, map[string]string{"../../evil": "pwned"})
	dest := t.TempDir()
	outside := filepath.Join(filepath.Dir(filepath.Dir(dest)), "evil")
	_ = os.Remove(outside)

	err := extractBundle(archive, dest)
	if _, statErr := os.Stat(outside); statErr == nil {
		_ = os.Remove(outside)
		t.Fatal("archive entry escaped the destination")
	}
	_ = err // confined either by refusal or by clamping; escaping is the failure
}

// A symlink target has to be validated exactly as os.Symlink will interpret
// it. Cleaning ../../../outside before validation but passing the raw value to
// os.Symlink lets the next regular entry write through the link and out of the
// staging directory.
func TestExtractBundleRefusesSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "stage")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "symlink-traversal.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "Foo.app/a/link", Linkname: "../../../outside",
		Mode: 0777, Typeflag: tar.TypeSymlink,
	}); err != nil {
		t.Fatal(err)
	}
	body := []byte("must stay in the archive")
	if err := tw.WriteHeader(&tar.Header{
		Name: "Foo.app/a/link/pwn", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractBundle(archive, dest); err == nil {
		t.Fatal("archive with an escaping symlink was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote through its symlink outside the destination: %v", err)
	}
}

// safeJoin is the guard the extraction relies on; check it directly too.
func TestSafeJoinConfinesEntries(t *testing.T) {
	dest := "/tmp/dest"
	for _, name := range []string{"../evil", "../../evil", "a/../../evil", "/etc/passwd"} {
		if got, err := safeJoin(dest, name); err == nil {
			t.Fatalf("safeJoin(%q, %q) = %q; traversal must be refused", dest, name, got)
		}
	}
}

// The old .app is left behind as a directory, so cleanup must remove trees and
// not just files.
func TestCleanStaleUpdateFilesRemovesOldDirectories(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "asmgr-desktop.app.old")
	if err := os.MkdirAll(filepath.Join(stale, "Contents", "MacOS"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "Contents", "MacOS", "foo"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "asmgr-desktop.app")
	if err := os.MkdirAll(keep, 0755); err != nil {
		t.Fatal(err)
	}

	cleanStaleUpdateFilesIn(dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the stale bundle was not removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("the current bundle must be left alone")
	}
}
