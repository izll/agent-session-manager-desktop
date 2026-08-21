//go:build linux

package updater

import (
	"context"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func packageChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func TestPrivilegedPackageHelperArgsUseVerifiedHelperMode(t *testing.T) {
	got := privilegedPackageHelperArgs(
		"/usr/bin/asmgr-desktop",
		"/home/user/.config/asmgr/updates/update.deb",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"deb",
		"v1.2.3",
	)
	want := []string{
		"/usr/bin/asmgr-desktop",
		privilegedPackageInstallFlag,
		"/home/user/.config/asmgr/updates/update.deb",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"deb",
		"v1.2.3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("privileged helper argv = %#v, want %#v", got, want)
	}
	if got[0] == "dpkg" || got[0] == "rpm" {
		t.Fatalf("untrusted package path would be opened directly by %q", got[0])
	}
}

func TestPrivilegedPackageHelperPinsChecksumToOfficialReleaseAsset(t *testing.T) {
	official := packageChecksum([]byte("official package"))
	var gotURL, gotFilename string
	reader := func(_ context.Context, url, filename string) (string, error) {
		gotURL, gotFilename = url, filename
		return official, nil
	}
	if err := verifyOfficialPackageChecksum(context.Background(), "v1.2.3", "deb", official, reader); err != nil {
		t.Fatal(err)
	}
	wantFilename := fmt.Sprintf("%s_1.2.3_linux_%s.deb", BinaryName, packageArch())
	if gotFilename != wantFilename || gotURL != releaseURL("v1.2.3", wantFilename) {
		t.Fatalf("official checksum target = %q / %q, want %q / %q", gotURL, gotFilename, releaseURL("v1.2.3", wantFilename), wantFilename)
	}

	attackerChecksum := packageChecksum([]byte("attacker-controlled package"))
	if err := verifyOfficialPackageChecksum(context.Background(), "v1.2.3", "deb", attackerChecksum, reader); err == nil {
		t.Fatal("pkexec helper accepted a caller-selected checksum that differs from the official release")
	}
}

func TestPrivilegedPackageHelperRejectsUnconstrainedReleaseBeforeNetwork(t *testing.T) {
	readerCalled := false
	reader := func(context.Context, string, string) (string, error) {
		readerCalled = true
		return "", nil
	}
	if err := verifyOfficialPackageChecksum(context.Background(), "../../evil", "deb", packageChecksum(nil), reader); err == nil {
		t.Fatal("invalid release version was accepted")
	}
	if err := verifyOfficialPackageChecksum(context.Background(), "v1.2.3", "script", packageChecksum(nil), reader); err == nil {
		t.Fatal("invalid package kind was accepted")
	}
	if readerCalled {
		t.Fatal("invalid helper arguments reached the network checksum reader")
	}
}

func TestOfficialReleaseURLPolicyIsFailClosed(t *testing.T) {
	allowed := []string{
		"https://github.com/asmgr/releases/download/v1.2.3/checksum",
		"https://objects.githubusercontent.com/github-production-release-asset/checksum",
		"https://release-assets.githubusercontent.com/github-production-release-asset/checksum",
		"https://github-releases.githubusercontent.com/checksum",
		"https://github.com:443/asmgr/checksum",
	}
	for _, rawURL := range allowed {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateOfficialReleaseURL(u); err != nil {
			t.Errorf("validateOfficialReleaseURL(%q) = %v", rawURL, err)
		}
	}

	rejected := []string{
		"http://github.com/asmgr/checksum",
		"https://github.com.evil.example/asmgr/checksum",
		"https://raw.githubusercontent.com/attacker/checksum",
		"https://user:password@github.com/asmgr/checksum",
		"https://github.com:444/asmgr/checksum",
	}
	for _, rawURL := range rejected {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateOfficialReleaseURL(u); err == nil {
			t.Errorf("validateOfficialReleaseURL(%q) accepted an untrusted URL", rawURL)
		}
	}
}

func TestOfficialReleaseRedirectPolicyIsFailClosed(t *testing.T) {
	client, err := newOfficialReleaseClient()
	if err != nil {
		t.Fatal(err)
	}
	allowed, _ := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/asset", nil)
	if err := client.CheckRedirect(allowed, []*http.Request{{}}); err != nil {
		t.Fatalf("allowed release redirect was rejected: %v", err)
	}
	untrusted, _ := http.NewRequest(http.MethodGet, "https://example.com/asset", nil)
	if err := client.CheckRedirect(untrusted, []*http.Request{{}}); err == nil {
		t.Fatal("release redirect escaped the trusted host set")
	}
	tooMany := make([]*http.Request, 5)
	if err := client.CheckRedirect(allowed, tooMany); err == nil {
		t.Fatal("release client accepted too many redirects")
	}
}

func TestOfficialReleaseClientIgnoresCallerCAOverrides(t *testing.T) {
	attackerDir := t.TempDir()
	t.Setenv("SSL_CERT_FILE", filepath.Join(attackerDir, "attacker.pem"))
	t.Setenv("SSL_CERT_DIR", filepath.Join(attackerDir, "attacker-certs"))
	client, err := newOfficialReleaseClient()
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("official client does not use an explicit system CA pool: %#v", client.Transport)
	}
	if len(transport.TLSClientConfig.RootCAs.Subjects()) == 0 {
		t.Fatal("caller CA overrides replaced the official client's system CA pool")
	}
	if got := os.Getenv("SSL_CERT_FILE"); got != filepath.Join(attackerDir, "attacker.pem") {
		t.Fatalf("SSL_CERT_FILE was not restored: %q", got)
	}
	if got := os.Getenv("SSL_CERT_DIR"); got != filepath.Join(attackerDir, "attacker-certs") {
		t.Fatalf("SSL_CERT_DIR was not restored: %q", got)
	}
}

func TestOfficialSystemCertPoolNeverConsultsCallerSelectedPaths(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsServer.Close()
	trusted := tlsServer.Certificate()
	trustedPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: trusted.Raw})

	attackerFile := filepath.Join(t.TempDir(), "attacker.pem")
	attackerDir := filepath.Join(t.TempDir(), "attacker-certs")
	t.Setenv("SSL_CERT_FILE", attackerFile)
	t.Setenv("SSL_CERT_DIR", attackerDir)
	consulted := make(map[string]bool)
	pool, err := loadOfficialSystemCertPool(func(path string) ([]byte, error) {
		consulted[path] = true
		if path == officialSystemCertFiles[2] {
			return trustedPEM, nil
		}
		return nil, os.ErrNotExist
	}, func(path string) ([]os.DirEntry, error) {
		consulted[path] = true
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	if consulted[attackerFile] || consulted[attackerDir] {
		t.Fatalf("caller-selected CA path was consulted: %#v", consulted)
	}
	if len(pool.Subjects()) != 1 || string(pool.Subjects()[0]) != string(trusted.RawSubject) {
		t.Fatal("fixed system CA bundle was not loaded into the official release pool")
	}
}

func TestOfficialSystemCertPoolLoadsDirectoryOnlyTrustStore(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsServer.Close()
	trusted := tlsServer.Certificate()
	trustedPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: trusted.Raw})

	fixtureDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixtureDir, "rotated.pem"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	wantedPath := filepath.Join(officialSystemCertDirs[0], "rotated.pem")
	pool, err := loadOfficialSystemCertPool(func(path string) ([]byte, error) {
		if path == wantedPath {
			return trustedPEM, nil
		}
		return nil, os.ErrNotExist
	}, func(path string) ([]os.DirEntry, error) {
		if path == officialSystemCertDirs[0] {
			return entries, nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Subjects()) != 1 || string(pool.Subjects()[0]) != string(trusted.RawSubject) {
		t.Fatal("directory-only system trust root was not loaded")
	}
}

func TestPrivilegedPackageCommandInheritsOuterProcessGroup(t *testing.T) {
	cmd := newPrivilegedPackageCommand(privilegedDpkgPath, "--version")
	if cmd.SysProcAttr != nil {
		t.Fatalf("privileged package command creates a detached process group: %#v", cmd.SysProcAttr)
	}
}

func TestPrivilegedPackageManagerIgnoresCallerPath(t *testing.T) {
	attackerBin := t.TempDir()
	for _, name := range []string{"dpkg", "rpm"} {
		if err := os.WriteFile(filepath.Join(attackerBin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", attackerBin)
	t.Setenv("RPM_CONFIGDIR", attackerBin)
	t.Setenv("DPKG_ADMINDIR", attackerBin)

	tests := []struct {
		kind string
		want string
	}{
		{kind: "deb", want: privilegedDpkgPath},
		{kind: "rpm", want: privilegedRPMPath},
	}
	for _, test := range tests {
		name, args, err := privilegedPackageManagerCommand(test.kind, "/var/tmp/update")
		if err != nil {
			t.Fatal(err)
		}
		cmd := newPrivilegedPackageCommand(name, args...)
		if cmd.Path != test.want {
			t.Fatalf("%s helper resolved package manager through caller PATH: got %q, want %q", test.kind, cmd.Path, test.want)
		}
		wantEnv := []string{"HOME=/root", "LANG=C", "LC_ALL=C", "PATH=" + privilegedExecPath}
		if !reflect.DeepEqual(cmd.Env, wantEnv) {
			t.Fatalf("%s helper inherited caller-controlled environment: %#v", test.kind, cmd.Env)
		}
	}
	if _, _, err := privilegedPackageManagerCommand("script", "/var/tmp/update"); err == nil {
		t.Fatal("unsupported package kind selected a privileged command")
	}
}

func TestPrivilegedPackageHelperIgnoresOrdinaryInvocation(t *testing.T) {
	if handled, code := HandlePrivilegedPackageInstall([]string{"asmgr-desktop"}); handled || code != 0 {
		t.Fatalf("ordinary invocation handled=%v code=%d", handled, code)
	}
}

func TestPrivilegedPackageProtocolProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || len(os.Args) <= separator+2 {
		return
	}
	mode, mutationPath := os.Args[separator+1], os.Args[separator+2]
	switch mode {
	case "never-ready":
		time.Sleep(10 * time.Second)
	case "ready":
		fmt.Fprintln(os.Stdout, privilegedPackageReady)
		ack := make([]byte, len(privilegedPackageAck))
		if _, err := io.ReadFull(os.Stdin, ack); err != nil || string(ack) != privilegedPackageAck {
			_ = os.WriteFile(mutationPath+".cleaned", []byte("cleaned"), 0o600)
			os.Exit(3)
		}
		if err := os.WriteFile(mutationPath, []byte("mutated"), 0o600); err != nil {
			os.Exit(4)
		}
	default:
		os.Exit(5)
	}
}

func privilegedProtocolTestArgs(mode, mutationPath string) []string {
	return []string{"-test.run=^TestPrivilegedPackageProtocolProcess$", "--", mode, mutationPath}
}

func TestPrivilegedPackagePromptIsCancellableBeforeReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mutationPath := filepath.Join(t.TempDir(), "mutation")
	criticalCalled := false
	result := make(chan error, 1)
	go func() {
		_, err := runPrivilegedPackageInstall(ctx, os.Args[0], privilegedProtocolTestArgs("never-ready", mutationPath), func(action func() error) error {
			criticalCalled = true
			return action()
		})
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled helper returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellable package prompt survived shutdown cancellation")
	}
	if criticalCalled {
		t.Fatal("package helper entered the critical mutation section before readiness")
	}
}

func TestPrivilegedPackageWaitsForCriticalAcknowledgement(t *testing.T) {
	mutationPath := filepath.Join(t.TempDir(), "mutation")
	criticalEntered := make(chan struct{})
	allowCritical := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := runPrivilegedPackageInstall(context.Background(), os.Args[0], privilegedProtocolTestArgs("ready", mutationPath), func(action func() error) error {
			close(criticalEntered)
			<-allowCritical
			return action()
		})
		result <- err
	}()
	select {
	case <-criticalEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("verified helper never reached the critical-section gate")
	}
	if _, err := os.Stat(mutationPath); !os.IsNotExist(err) {
		t.Fatalf("package mutation began before critical acknowledgement: %v", err)
	}
	close(allowCritical)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acknowledged package transaction did not finish")
	}
	if _, err := os.Stat(mutationPath); err != nil {
		t.Fatalf("acknowledged package mutation did not run: %v", err)
	}
}

func TestPrivilegedPackageCriticalRejectionSendsNoAcknowledgement(t *testing.T) {
	mutationPath := filepath.Join(t.TempDir(), "mutation")
	started := time.Now()
	_, err := runPrivilegedPackageInstall(context.Background(), os.Args[0], privilegedProtocolTestArgs("ready", mutationPath), func(func() error) error {
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("critical rejection returned %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("rejected critical section did not reap the blocked helper promptly")
	}
	if _, statErr := os.Stat(mutationPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected package transaction mutated the system: %v", statErr)
	}
	if _, statErr := os.Stat(mutationPath + ".cleaned"); statErr != nil {
		t.Fatalf("rejected prepared helper did not get a graceful cleanup opportunity: %v", statErr)
	}
}

func TestStageVerifiedPackageCopiesToPrivateRoot(t *testing.T) {
	content := []byte("verified package bytes")
	source := filepath.Join(t.TempDir(), "update.deb")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}

	stageDir, staged, err := stageVerifiedPackage(source, packageChecksum(content), "deb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stageDir)

	if filepath.Dir(stageDir) != privilegedPackageTempRoot {
		t.Fatalf("stage directory %q is not below fixed privileged root %q", stageDir, privilegedPackageTempRoot)
	}
	if staged == source {
		t.Fatal("package manager would receive the user-writable source path")
	}
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("staged content %q, want %q", got, content)
	}
	dirInfo, err := os.Stat(stageDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("stage directory mode %o, want 700", got)
	}
	fileInfo, err := os.Stat(staged)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("staged package mode %o, want 600", got)
	}
}

func TestPrivilegedPackageCleanupRequiresExactOldOwnedMarker(t *testing.T) {
	content := []byte("verified package bytes")
	source := filepath.Join(t.TempDir(), "update.deb")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	owned, _, err := stageVerifiedPackage(source, packageChecksum(content), "deb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(owned)
	old := time.Now().Add(-stageCleanupAge - time.Hour)
	if err := os.Chtimes(filepath.Join(owned, privilegedPackageStageMarker), old, old); err != nil {
		t.Fatal(err)
	}

	unowned, err := os.MkdirTemp(privilegedPackageTempRoot, "asmgr-package-install-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(unowned)
	if err := os.WriteFile(filepath.Join(unowned, privilegedPackageStageMarker), []byte("not ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(unowned, privilegedPackageStageMarker), old, old); err != nil {
		t.Fatal(err)
	}

	cleanStalePrivilegedPackageStages()
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("old owned privileged stage survived cleanup: %v", err)
	}
	if _, err := os.Stat(unowned); err != nil {
		t.Fatalf("unowned privileged stage was removed: %v", err)
	}
}

func TestStageVerifiedPackageRejectsReplacementAfterDownload(t *testing.T) {
	original := []byte("verified release")
	source := filepath.Join(t.TempDir(), "update.rpm")
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	trusted := packageChecksum(original)
	if err := os.WriteFile(source, []byte("attacker replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	stageDir, staged, err := stageVerifiedPackage(source, trusted, "rpm")
	if err == nil {
		if stageDir != "" {
			_ = os.RemoveAll(stageDir)
		}
		t.Fatalf("replacement was staged at %q", staged)
	}
	if stageDir != "" || staged != "" {
		t.Fatalf("failed verification leaked staging paths dir=%q file=%q", stageDir, staged)
	}
}

func TestStageVerifiedPackageRejectsSymlinkSource(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.deb")
	content := []byte("verified release")
	if err := os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "update.deb")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if stageDir, staged, err := stageVerifiedPackage(link, packageChecksum(content), "deb"); err == nil {
		if stageDir != "" {
			_ = os.RemoveAll(stageDir)
		}
		t.Fatalf("symlink source was staged at %q", staged)
	}
}

func TestStageVerifiedPackageRejectsOversizeSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "update.deb")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(DownloadLimit + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if stageDir, staged, err := stageVerifiedPackage(source, packageChecksum(nil), "deb"); err == nil {
		if stageDir != "" {
			_ = os.RemoveAll(stageDir)
		}
		t.Fatalf("oversize source was staged at %q", staged)
	}
}
