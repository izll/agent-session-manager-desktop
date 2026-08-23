package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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

func TestAutomaticInstallCapabilityFailsClosedForFlatWindowsPayload(t *testing.T) {
	if automaticInstallSupportedFor("windows", "") {
		t.Fatal("flat Windows EXE+DLL layout must not advertise atomic automatic installation")
	}
	err := ensureAutomaticInstallSupported("windows", "")
	if err == nil || !strings.Contains(err.Error(), "Windows setup executable") {
		t.Fatalf("Windows manual-install guard = %v", err)
	}
	if !automaticInstallSupportedFor("linux", "") {
		t.Fatal("Linux atomic/package update paths must remain enabled")
	}
	if automaticInstallSupportedFor("darwin", "") {
		t.Fatal("unsigned Darwin build must not advertise automatic bundle replacement")
	}
	if !automaticInstallSupportedFor("darwin", "ABCDE12345") {
		t.Fatal("publisher-pinned Darwin release must remain enabled")
	}
	criticalCalled := false
	err = downloadAndInstallForPlatform(context.Background(), "v9.9.9", func(action func() error) error {
		criticalCalled = true
		return action()
	}, "windows")
	if err == nil || criticalCalled {
		t.Fatalf("Windows update reached mutation preparation: err=%v critical=%v", err, criticalCalled)
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

	got, err := CheckForUpdate("0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.10.0" {
		t.Fatalf("CheckForUpdate returned %q", got)
	}
}

func TestRefreshAvailableUpdatePreservesStateOnHTTPFailure(t *testing.T) {
	withTempHome(t)
	SaveAvailableUpdate("v2.0.0")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary outage", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	oldBase, oldClient := apiBaseURL, checkClient
	apiBaseURL = server.URL
	checkClient = server.Client()
	defer func() { apiBaseURL, checkClient = oldBase, oldClient }()

	if _, err := RefreshAvailableUpdate("1.0.0"); err == nil {
		t.Fatal("RefreshAvailableUpdate unexpectedly accepted an HTTP failure")
	}
	if got := CachedAvailableUpdate("1.0.0"); got != "v2.0.0" {
		t.Fatalf("cached update was changed to %q after a failed check", got)
	}
	if !ShouldCheckForUpdate() {
		t.Fatal("failed check wrote a throttle timestamp and suppressed an immediate retry")
	}
}

func TestCheckForUpdateContextCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	oldBase, oldClient := apiBaseURL, checkClient
	apiBaseURL = server.URL
	checkClient = server.Client()
	defer func() { apiBaseURL, checkClient = oldBase, oldClient }()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := CheckForUpdateContext(ctx, "1.0.0")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("update request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled update request returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("update request survived context cancellation")
	}
}

func TestDownloadVerifiedAssetContextCancelsInFlightRequest(t *testing.T) {
	filename := "asmgr-desktop_1.2.3_linux_amd64.tar.gz"
	assetStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%064x  %s\n", 0, filename)
			return
		}
		close(assetStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	oldBase, oldClient := downloadBaseURL, downloadClient
	downloadBaseURL = server.URL
	downloadClient = server.Client()
	defer func() { downloadBaseURL, downloadClient = oldBase, oldClient }()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := downloadVerifiedAssetContext(ctx, "v1.2.3", filename, "asmgr-test-*")
		result <- err
	}()
	select {
	case <-assetStarted:
	case <-time.After(time.Second):
		t.Fatal("asset download did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled asset download returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("asset download survived context cancellation")
	}
}

func TestRefreshAvailableUpdatePersistsSuccessfulNoUpdate(t *testing.T) {
	withTempHome(t)
	SaveAvailableUpdate("v2.0.0")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	oldBase, oldClient := apiBaseURL, checkClient
	apiBaseURL = server.URL
	checkClient = server.Client()
	defer func() { apiBaseURL, checkClient = oldBase, oldClient }()

	got, err := RefreshAvailableUpdate("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || CachedAvailableUpdate("1.0.0") != "" {
		t.Fatalf("successful no-update check left stale result %q", CachedAvailableUpdate("1.0.0"))
	}
	if ShouldCheckForUpdate() {
		t.Fatal("successful check did not write the throttle timestamp")
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

	path, trustedChecksum, err := downloadVerifiedAssetContextWithChecksum(context.Background(), "v1.2.3", filename, "asmgr-test-*.deb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if want := fmt.Sprintf("%x", sum); trustedChecksum != want {
		t.Fatalf("trusted checksum = %q, want %q", trustedChecksum, want)
	}
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

func TestVerifyStagedBundleRequiresSignatureAndGatekeeperAssessment(t *testing.T) {
	bundle := "/tmp/Agent Session Manager.app"
	teamID := "ABCDE12345"
	var calls [][]string
	err := verifyStagedBundleWith(bundle, teamID, func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "-dv" {
			return []byte("Executable=/staged\nTeamIdentifier=" + teamID + "\n"), nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/usr/bin/codesign", "--verify", "--deep", "--strict", bundle},
		{"/usr/bin/codesign", "-dv", "--verbose=4", bundle},
		{"/usr/sbin/spctl", "--assess", "--type", "execute", bundle},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("verification calls = %#v, want %#v", calls, want)
	}
}

func TestVerifyStagedBundleFailsClosed(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("check-%d", failAt), func(t *testing.T) {
			calls := 0
			err := verifyStagedBundleWith("staged.app", "ABCDE12345", func(_ string, args ...string) ([]byte, error) {
				current := calls
				calls++
				if current == failAt {
					return []byte("rejected staged bundle"), errors.New("exit status 1")
				}
				if len(args) > 0 && args[0] == "-dv" {
					return []byte("TeamIdentifier=ABCDE12345\n"), nil
				}
				return nil, nil
			})
			if err == nil || !strings.Contains(err.Error(), "rejected staged bundle") {
				t.Fatalf("verification error = %v", err)
			}
			if calls != failAt+1 {
				t.Fatalf("verification continued after failure: %d calls", calls)
			}
		})
	}
}

func TestVerifyStagedBundlePinsPublisherIdentity(t *testing.T) {
	run := func(_ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "-dv" {
			return []byte("Authority=Developer ID Application\nTeamIdentifier=OTHER12345\n"), nil
		}
		return nil, nil
	}
	if err := verifyStagedBundleWith("staged.app", "ABCDE12345", run); err == nil ||
		!strings.Contains(err.Error(), "publisher mismatch") {
		t.Fatalf("publisher mismatch error = %v", err)
	}
	if err := verifyStagedBundleWith("staged.app", "", run); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("missing publisher configuration error = %v", err)
	}
}

func TestValidateBundleDirectoryRejectsTopLevelSymlink(t *testing.T) {
	root := t.TempDir()
	realBundle := filepath.Join(root, "Real.app")
	if err := os.Mkdir(realBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateBundleDirectory(realBundle); err != nil {
		t.Fatalf("real bundle directory rejected: %v", err)
	}
	linkedBundle := filepath.Join(root, "Agent Session Manager.app")
	if err := os.Symlink(filepath.Base(realBundle), linkedBundle); err != nil {
		t.Skipf("symlinks are unavailable on this platform: %v", err)
	}
	if err := validateBundleDirectory(linkedBundle); err == nil {
		t.Fatal("top-level bundle symlink was accepted")
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
		"unrelated-project.old": true, // suffix alone never establishes ownership
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

func TestCleanStaleUpdateFilesKeepsUnrelatedOldDirectory(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "customer-data.old")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(victim, "keep.txt"), []byte("important"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanStaleUpdateFilesIn(dir)
	if got, err := os.ReadFile(filepath.Join(victim, "keep.txt")); err != nil || string(got) != "important" {
		t.Fatalf("unrelated .old directory was modified: got %q err=%v", got, err)
	}
}

func TestCleanMarkedUpdateStagesRequiresOwnershipMarker(t *testing.T) {
	dir := t.TempDir()
	owned, err := createUpdateStageDir(dir, "."+BinaryName+"-update-*")
	if err != nil {
		t.Fatal(err)
	}
	active, err := createUpdateStageDir(dir, "."+BinaryName+"-update-*")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-stageCleanupAge - time.Hour)
	if err := os.Chtimes(filepath.Join(owned, updateStageMarker), old, old); err != nil {
		t.Fatal(err)
	}
	unmarked, err := os.MkdirTemp(dir, "."+BinaryName+"-update-*")
	if err != nil {
		t.Fatal(err)
	}
	wrongMarker, err := os.MkdirTemp(dir, "."+BinaryName+"-dll-stage-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrongMarker, updateStageMarker), []byte("not ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated, err := createUpdateStageDir(dir, ".another-app-update-*")
	if err != nil {
		t.Fatal(err)
	}

	cleanMarkedUpdateStagesIn(dir)
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned staging directory was not removed: %v", err)
	}
	for _, path := range []string{active, unmarked, wrongMarker, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unowned directory %q was removed: %v", path, err)
		}
	}
}

func TestCleanStaleUpdateDownloadsKeepsFreshAndUnrelatedFiles(t *testing.T) {
	withTempHome(t)
	dir, err := secureUpdateDownloadDir()
	if err != nil {
		t.Fatal(err)
	}
	oldDownload := filepath.Join(dir, BinaryName+"-old.tar.gz")
	freshDownload := filepath.Join(dir, BinaryName+"-fresh.deb")
	unrelated := filepath.Join(dir, "important.tar.gz")
	for _, path := range []string{oldDownload, freshDownload, unrelated} {
		if err := os.WriteFile(path, []byte("asset"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-stageCleanupAge - time.Hour)
	for _, path := range []string{oldDownload, unrelated} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	cleanStaleUpdateDownloads()
	if _, err := os.Stat(oldDownload); !os.IsNotExist(err) {
		t.Fatalf("old updater download was not removed: %v", err)
	}
	for _, path := range []string{freshDownload, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fresh or unrelated file %q was removed: %v", path, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("download directory permissions = %v", info.Mode().Perm())
	}
}

func TestBundleCleanupChecksApplicationParentAndExecutableDirectory(t *testing.T) {
	withTempHome(t)
	root := t.TempDir()
	bundle := filepath.Join(root, "Agent Session Manager.app")
	execDir := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		t.Fatal(err)
	}
	execPath := filepath.Join(execDir, BinaryName)
	stages := make([]string, 0, 2)
	for _, parent := range []string{root, execDir} {
		stage, err := createUpdateStageDir(parent, "."+BinaryName+"-update-*")
		if err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-stageCleanupAge - time.Hour)
		if err := os.Chtimes(filepath.Join(stage, updateStageMarker), old, old); err != nil {
			t.Fatal(err)
		}
		stages = append(stages, stage)
	}

	cleanStaleUpdateFilesFor(execPath)
	for _, stage := range stages {
		if _, err := os.Stat(stage); !os.IsNotExist(err) {
			t.Fatalf("stale stage %q was not removed: %v", stage, err)
		}
	}
}

func TestStageSidecarDLLsRejectsCaseInsensitiveDuplicateBasenames(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"one/Foo.dll": "first",
		"two/foo.DLL": "second",
	})
	if _, cleanup, err := stageSidecarDLLs(archive, t.TempDir()); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("case-insensitive duplicate DLL basenames must be rejected")
	}
}

func TestStageSidecarDLLsEnforcesCumulativeLimits(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"one.dll": "1234",
		"README":  "5678", // non-DLL regular entries count too
	})
	if _, cleanup, err := stageSidecarDLLsWithLimits(archive, t.TempDir(), archiveLimits{bytes: 7, entries: 10}); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("cumulative DLL bytes above the limit must be rejected")
	}
	if _, cleanup, err := stageSidecarDLLsWithLimits(archive, t.TempDir(), archiveLimits{bytes: 100, entries: 1}); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("archives above the entry limit must be rejected")
	}
}

func TestValidateRequiredSidecarDLLsRejectsArchiveWithoutPortAudio(t *testing.T) {
	dir := t.TempDir()
	files := []stagedInstall{{
		target: filepath.Join(dir, "libgcc_s_seh-1.dll"),
		staged: filepath.Join(dir, ".staged-libgcc"),
	}}
	if err := validateRequiredSidecarDLLs(files, requiredWindowsRuntimeDLLs); err == nil ||
		!strings.Contains(err.Error(), "libportaudio.dll") {
		t.Fatalf("missing PortAudio validation error = %v", err)
	}
	files = append(files, stagedInstall{
		target: filepath.Join(dir, "LIBPORTAUDIO.DLL"),
		staged: filepath.Join(dir, ".staged-portaudio"),
	})
	if err := validateRequiredSidecarDLLs(files, requiredWindowsRuntimeDLLs); err != nil {
		t.Fatalf("case-insensitive required DLL match failed: %v", err)
	}
}

func TestExtractExecutableCapsWholeArchiveNotOnlyBinary(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		BinaryName: "bin",
		"payload":  "oversized ignored content",
	})
	out, err := os.CreateTemp(t.TempDir(), "executable-*")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := extractExecutableWithLimits(archive, BinaryName, out, archiveLimits{bytes: 8, entries: 10}); err == nil {
		t.Fatal("non-executable regular entries bypassed the aggregate extraction limit")
	}
	if err := extractExecutableWithLimits(archive, BinaryName, out, archiveLimits{bytes: 100, entries: 1}); err == nil {
		t.Fatal("executable scan bypassed the archive entry limit")
	}
}

func TestInstallTransactionRollsBackEveryEarlierFile(t *testing.T) {
	dir := t.TempDir()
	targetA := filepath.Join(dir, "a.dll")
	targetB := filepath.Join(dir, "b.dll")
	stagedA := filepath.Join(dir, ".new-a")
	stagedB := filepath.Join(dir, ".new-b")
	for path, content := range map[string]string{
		targetA: "old-a", targetB: "old-b", stagedA: "new-a", stagedB: "new-b",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	failOnce := true
	rename := func(old, new string) error {
		if old == stagedB && failOnce {
			failOnce = false
			return fmt.Errorf("injected executable/DLL swap failure")
		}
		return os.Rename(old, new)
	}
	err := installTransactionWithRename([]stagedInstall{
		{target: targetA, staged: stagedA},
		{target: targetB, staged: stagedB},
	}, rename)
	if err == nil {
		t.Fatal("injected transaction failure was ignored")
	}
	for path, want := range map[string]string{targetA: "old-a", targetB: "old-b"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("rollback did not restore %s: got %q err=%v", filepath.Base(path), got, readErr)
		}
	}
}

func TestSingleFileInstallNeverMovesTheOldExecutableAwayFirst(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName)
	staged := filepath.Join(dir, ".new-"+BinaryName)
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	renameCalls := 0
	rename := func(old, new string) error {
		renameCalls++
		if old != staged || new != target {
			t.Fatalf("atomic install rename = %q -> %q, want staged directly over target", old, new)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("installed executable disappeared before replacement: %v", err)
		}
		return os.Rename(old, new)
	}
	if err := installSingleFileAtomically(stagedInstall{target: target, staged: staged}, rename); err != nil {
		t.Fatal(err)
	}
	if renameCalls != 1 {
		t.Fatalf("atomic install used %d renames, want one", renameCalls)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("installed executable = %q, %v", got, err)
	}
}

func TestSingleFileInstallPersistsParentAfterAtomicRename(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName)
	staged := filepath.Join(dir, ".new-"+BinaryName)
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	var events []string
	rename := func(old, new string) error {
		events = append(events, "rename")
		return os.Rename(old, new)
	}
	syncDir := func(path string) error {
		events = append(events, "sync")
		if path != dir {
			t.Fatalf("synced directory = %q, want %q", path, dir)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
			t.Fatalf("directory was synced before replacement publication: got=%q err=%v", got, err)
		}
		return nil
	}
	if err := installSingleFileAtomicallyWithOps(stagedInstall{target: target, staged: staged}, rename, syncDir); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"rename", "sync"}) {
		t.Fatalf("durable install order = %v, want rename then directory sync", events)
	}
}

func TestBundleRenamePublishesReplacementBeforeRetiringOldName(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Old.app")
	staged := filepath.Join(dir, ".stage", "New.app")
	target := filepath.Join(dir, "New.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	var calls [][2]string
	rename := func(old, new string) error {
		calls = append(calls, [2]string{old, new})
		if len(calls) == 1 {
			if old != staged || new != target {
				t.Fatalf("first bundle rename = %q -> %q, want replacement publication", old, new)
			}
			if _, err := os.Stat(bundle); err != nil {
				t.Fatalf("old bundle disappeared before replacement publication: %v", err)
			}
		}
		return os.Rename(old, new)
	}
	if err := swapBundleWithOps(bundle, staged, target, rename, os.RemoveAll); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("renamed bundle was not installed: %v", err)
	}
}

func TestBundleRenamePersistsReplacementBeforeRetiringOldName(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Old.app")
	staged := filepath.Join(dir, ".stage", "New.app")
	target := filepath.Join(dir, "New.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	var events []string
	rename := func(old, new string) error {
		switch {
		case old == staged && new == target:
			events = append(events, "publish")
		case old == bundle:
			events = append(events, "retire")
		default:
			t.Fatalf("unexpected rename %q -> %q", old, new)
		}
		return os.Rename(old, new)
	}
	syncDir := func(path string) error {
		if path == dir {
			events = append(events, "sync-parent")
		} else {
			events = append(events, "sync-backup")
		}
		return nil
	}
	if err := swapBundleWithDurabilityOps(bundle, staged, target, rename, os.RemoveAll, syncDir); err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"publish", "sync-parent", "retire", "sync-parent", "sync-backup"}
	if !reflect.DeepEqual(events, wantPrefix) {
		t.Fatalf("durable bundle migration order = %v, want %v", events, wantPrefix)
	}
}

func TestInstallTransactionPreservesBackupWhenRollbackFails(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	targetA := filepath.Join(dir, "a.dll")
	targetB := filepath.Join(dir, "b.dll")
	stagedA := filepath.Join(dir, ".new-a")
	stagedB := filepath.Join(dir, ".new-b")
	for path, content := range map[string]string{
		targetA: "old-a", targetB: "old-b", stagedA: "new-a", stagedB: "new-b",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rename := func(old, new string) error {
		if old == stagedB {
			return fmt.Errorf("injected install failure")
		}
		if strings.Contains(old, "."+BinaryName+"-update-rollback-") && new == targetB {
			return fmt.Errorf("injected restore failure")
		}
		return os.Rename(old, new)
	}
	err := installTransactionWithRename([]stagedInstall{
		{target: targetA, staged: stagedA},
		{target: targetB, staged: stagedB},
	}, rename)
	if err == nil || !strings.Contains(err.Error(), "backups preserved") {
		t.Fatalf("rollback failure did not report preserved backup: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "."+BinaryName+"-update-rollback-*", "*b.dll"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("failed rollback backup was deleted: backups=%v err=%v", backups, err)
	}
	if got, err := os.ReadFile(backups[0]); err != nil || string(got) != "old-b" {
		t.Fatalf("preserved rollback backup changed: got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(configDir(), failedUpdateFile)); err != nil {
		t.Fatalf("failed rollback path was not recorded: %v", err)
	}
}

func TestBundleRollbackPreservesOnlyBackupOnRestoreFailure(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Old.app")
	staged := filepath.Join(dir, "New.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "original"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	rename := func(old, new string) error {
		if old == staged {
			return fmt.Errorf("injected bundle install failure")
		}
		if strings.Contains(old, "."+BinaryName+"-update-rollback-") && new == bundle {
			return fmt.Errorf("injected bundle restore failure")
		}
		return os.Rename(old, new)
	}
	removeCalls := 0
	err := swapBundleWithOps(bundle, staged, bundle, rename, func(path string) error {
		removeCalls++
		return os.RemoveAll(path)
	})
	if err == nil || !strings.Contains(err.Error(), "backup preserved") {
		t.Fatalf("bundle rollback failure did not preserve/report backup: %v", err)
	}
	if removeCalls != 0 {
		t.Fatalf("bundle rollback deleted its only backup (%d RemoveAll calls)", removeCalls)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "."+BinaryName+"-update-rollback-*", "Old.app", "original"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("bundle backup missing after failed restore: %v err=%v", backups, err)
	}
}

func TestRecordedCleanupRefusesUnownedManifestPath(t *testing.T) {
	withTempHome(t)
	installDir := t.TempDir()
	execPath := filepath.Join(installDir, BinaryName)
	owned := filepath.Join(installDir, "."+BinaryName+"-update-rollback-owned")
	victim := filepath.Join(t.TempDir(), "documents")
	for _, dir := range []string{owned, victim} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(configDir(), staleUpdateFile)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{owned, victim})
	if err := os.WriteFile(manifest, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cleanRecordedUpdateFiles(execPath, "")
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned rollback directory survived cleanup: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("unowned manifest path was deleted: %v", err)
	}
}

func TestRecordedUpdateManifestAccumulatesPaths(t *testing.T) {
	withTempHome(t)
	first := filepath.Join(t.TempDir(), "."+BinaryName+"-update-rollback-first")
	second := filepath.Join(t.TempDir(), "."+BinaryName+"-update-rollback-second")
	recordStaleUpdatePath(first)
	recordStaleUpdatePath(second)

	raw, err := os.ReadFile(filepath.Join(configDir(), staleUpdateFile))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{first, second}) {
		t.Fatalf("manifest paths = %#v, want both recorded paths", paths)
	}
}

func TestUpdaterStateReadsAndPathManifestsAreBounded(t *testing.T) {
	dir := withTempHome(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, LastCheckFile)
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readUpdateStateFile(oversized, updateScalarLimit); err == nil {
		t.Fatal("sparse oversized updater state was accepted")
	}
	if !ShouldCheckForUpdate() {
		t.Fatal("oversized timestamp suppressed update checking")
	}
	cache := filepath.Join(dir, AvailableUpdateFile)
	if err := os.Rename(oversized, cache); err != nil {
		t.Fatal(err)
	}
	if got := CachedAvailableUpdate("1.0.0"); got != "" {
		t.Fatalf("oversized cached update was offered as %q", got)
	}

	tooMany := make([]string, updateManifestEntries+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("/tmp/rollback-%d", i)
	}
	raw, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeUpdatePaths(raw); err == nil {
		t.Fatal("path manifest entry limit was not enforced")
	}
	if _, err := decodeUpdatePaths([]byte(`["/tmp/rollback"] {}`)); err == nil {
		t.Fatal("trailing manifest document was accepted")
	}
}

func TestRecordedCleanupRejectsOversizedManifestWithoutDeleting(t *testing.T) {
	withTempHome(t)
	installDir := t.TempDir()
	execPath := filepath.Join(installDir, BinaryName)
	owned := filepath.Join(installDir, "."+BinaryName+"-update-rollback-owned")
	if err := os.MkdirAll(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(configDir(), staleUpdateFile)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(updateManifestLimit + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cleanRecordedUpdateFiles(execPath, "")
	if _, err := os.Stat(owned); err != nil {
		t.Fatalf("oversized manifest caused rollback deletion: %v", err)
	}
}

func TestRecordUpdatePathPreservesCorruptRecoveryManifest(t *testing.T) {
	withTempHome(t)
	manifest := filepath.Join(configDir(), failedUpdateFile)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`["/only/copy/of/old-executable"] trailing`)
	if err := os.WriteFile(manifest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	recordFailedUpdatePath("/new/rollback")
	got, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("corrupt recovery manifest was overwritten and older backup reference was lost")
	}
}

func TestInstallLockSerializesIndependentProcesses(t *testing.T) {
	if role := os.Getenv("ASMGR_UPDATE_LOCK_HELPER"); role != "" {
		err := withInstallLock(func() error {
			if role == "holder" {
				if err := os.WriteFile(os.Getenv("ASMGR_UPDATE_LOCK_READY"), []byte("ready"), 0o600); err != nil {
					return err
				}
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("ASMGR_UPDATE_LOCK_RELEASE")); err == nil {
						return nil
					}
					if time.Now().After(deadline) {
						return fmt.Errorf("timed out waiting for release")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			return os.WriteFile(os.Getenv("ASMGR_UPDATE_LOCK_ACQUIRED"), []byte("acquired"), 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	coord := t.TempDir()
	ready := filepath.Join(coord, "ready")
	release := filepath.Join(coord, "release")
	acquired := filepath.Join(coord, "acquired")
	baseEnv := os.Environ()
	holder := exec.Command(os.Args[0], "-test.run=^TestInstallLockSerializesIndependentProcesses$")
	holder.Env = append(baseEnv,
		"ASMGR_UPDATE_LOCK_HELPER=holder",
		"ASMGR_UPDATE_LOCK_READY="+ready,
		"ASMGR_UPDATE_LOCK_RELEASE="+release,
	)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install-lock holder did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	contender := exec.Command(os.Args[0], "-test.run=^TestInstallLockSerializesIndependentProcesses$")
	contender.Env = append(baseEnv,
		"ASMGR_UPDATE_LOCK_HELPER=contender",
		"ASMGR_UPDATE_LOCK_ACQUIRED="+acquired,
	)
	if err := contender.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(acquired); !os.IsNotExist(err) {
		t.Fatalf("contender entered while holder owned install lock: %v", err)
	}
	tryStarted := time.Now()
	tryActionRan := false
	locked, err := withInstallTryLock(func() error {
		tryActionRan = true
		return nil
	})
	if err != nil {
		t.Fatalf("nonblocking install lock returned an error under contention: %v", err)
	}
	if locked || tryActionRan {
		t.Fatal("nonblocking install lock entered while another process held it")
	}
	if elapsed := time.Since(tryStarted); elapsed > time.Second {
		t.Fatalf("nonblocking install lock took %v under contention", elapsed)
	}
	cleanupStarted := time.Now()
	CleanStaleUpdateFiles()
	if elapsed := time.Since(cleanupStarted); elapsed > time.Second {
		t.Fatalf("startup cleanup blocked for %v behind another process's install", elapsed)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := contender.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(acquired); err != nil {
		t.Fatalf("contender never acquired install lock: %v", err)
	}
}

func TestInstallLockContextIsCancellableWhileContended(t *testing.T) {
	installMu.Lock()
	defer installMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- withInstallLockContext(ctx, func() error {
			t.Error("contended cancellable lock unexpectedly ran the action")
			return nil
		})
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled lock wait returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled install lock wait did not return")
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

func TestPackageCommandHasTimeAndOutputBounds(t *testing.T) {
	if helper := os.Getenv("ASMGR_PACKAGE_COMMAND_HELPER"); helper != "" {
		switch helper {
		case "sleep":
			if runtime.GOOS == "linux" {
				_ = os.Setenv("ASMGR_PACKAGE_COMMAND_HELPER", "grandchild")
				child := exec.Command(os.Args[0], "-test.run=^TestPackageCommandHasTimeAndOutputBounds$")
				child.Stdout = os.Stdout
				child.Stderr = os.Stderr
				if err := child.Start(); err != nil {
					os.Exit(2)
				}
			}
			time.Sleep(5 * time.Second)
		case "grandchild":
			time.Sleep(5 * time.Second)
		case "spam":
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), packageOutputLimit+4096))
		}
		return
	}

	t.Setenv("ASMGR_PACKAGE_COMMAND_HELPER", "sleep")
	started := time.Now()
	_, err := runPackageCommand(50*time.Millisecond, os.Args[0], "-test.run=^TestPackageCommandHasTimeAndOutputBounds$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed command returned %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timed command took %v after its deadline", elapsed)
	}

	t.Setenv("ASMGR_PACKAGE_COMMAND_HELPER", "spam")
	out, err := runPackageCommand(10*time.Second, os.Args[0], "-test.run=^TestPackageCommandHasTimeAndOutputBounds$")
	if err != nil {
		t.Fatalf("bounded-output helper failed: %v", err)
	}
	marker := []byte("\n[output truncated]")
	if len(out) != packageOutputLimit+len(marker) {
		t.Fatalf("captured %d bytes, want the %d-byte limit plus marker", len(out), packageOutputLimit)
	}
	if !bytes.HasSuffix(out, marker) {
		t.Fatal("truncated command output did not include the truncation marker")
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

// A legacy name alone is not ownership proof. A user may have manually kept a
// full bundle backup under this natural name; recursive startup cleanup must
// not destroy it.
func TestCleanStaleUpdateFilesPreservesUnmarkedOldDirectories(t *testing.T) {
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

	if got, err := os.ReadFile(filepath.Join(stale, "Contents", "MacOS", "foo")); err != nil || string(got) != "x" {
		t.Fatalf("unmarked legacy bundle backup was modified: got %q err=%v", got, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("the current bundle must be left alone")
	}
}
