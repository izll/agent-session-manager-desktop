//go:build linux

package updater

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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
	)
	want := []string{
		"/usr/bin/asmgr-desktop",
		privilegedPackageInstallFlag,
		"/home/user/.config/asmgr/updates/update.deb",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"deb",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("privileged helper argv = %#v, want %#v", got, want)
	}
	if got[0] == "dpkg" || got[0] == "rpm" {
		t.Fatalf("untrusted package path would be opened directly by %q", got[0])
	}
}

func TestPrivilegedPackageCommandInheritsOuterProcessGroup(t *testing.T) {
	cmd := newPrivilegedPackageCommand("dpkg", "--version")
	if cmd.SysProcAttr != nil {
		t.Fatalf("privileged package command creates a detached process group: %#v", cmd.SysProcAttr)
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
