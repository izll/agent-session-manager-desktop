//go:build linux

package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const privilegedPackageInstallFlag = "--asmgr-verified-package-install"

const (
	privilegedDpkgPath = "/usr/bin/dpkg"
	privilegedRPMPath  = "/usr/bin/rpm"
	privilegedExecPath = "/usr/sbin:/usr/bin:/sbin:/bin"
)

const (
	privilegedPackageReady = "ASMGR_PACKAGE_STAGED_AND_VERIFIED_V1"
	privilegedPackageAck   = "ASMGR_BEGIN_PACKAGE_TRANSACTION_V1\n"
	preparedHelperExitWait = 2 * time.Second
)

// Do not inherit TMPDIR across the privilege boundary. pkexec normally
// sanitises the environment, but the updater's security must not depend on a
// particular PolicyKit configuration. /var/tmp is a system-owned sticky
// directory on supported Linux distributions; the actual staging directory is
// then made root-only below.
const privilegedPackageTempRoot = "/var/tmp"

const (
	privilegedPackageStageMarker = ".asmgr-package-stage"
	privilegedPackageStageOwner  = "asmgr-desktop privileged package stage v1\n"
)

// privilegedPackageHelperArgs deliberately invokes the package-owned asmgr
// executable, not dpkg/rpm directly. The helper crosses the privilege boundary
// first, copies the user-writable download to a root-owned file, and verifies
// the checksum there before a package manager is allowed to open it.
func privilegedPackageHelperArgs(executable, packagePath, trustedChecksum, packageKind, version string) []string {
	return []string{executable, privilegedPackageInstallFlag, packagePath, trustedChecksum, packageKind, version}
}

// HandlePrivilegedPackageInstall handles the narrow pkexec helper mode before
// Wails, logging, notifications, or GPU probing are initialized. It returns
// handled=false for every ordinary application invocation.
func HandlePrivilegedPackageInstall(args []string) (handled bool, exitCode int) {
	if len(args) < 2 || args[1] != privilegedPackageInstallFlag {
		return false, 0
	}
	if len(args) != 6 {
		fmt.Fprintln(os.Stderr, "invalid verified package installer arguments")
		return true, 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "verified package installer must run through pkexec")
		return true, 1
	}
	cleanStalePrivilegedPackageStages()

	// Do not trust the checksum merely because the unprivileged parent supplied
	// it. This helper is a public pkexec entry point: without an independent
	// fixed-origin check, any local process could invoke it directly and provide
	// the checksum of an arbitrary package of its own choosing. Re-fetch only
	// the tiny checksum sidecar as root and constrain its release/asset name from
	// compiled constants before opening the user-writable package path.
	verifyCtx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	err := verifyOfficialPackageChecksum(verifyCtx, args[5], args[4], args[3], readOfficialChecksumContext)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "package publisher verification failed: %v\n", err)
		return true, 1
	}

	stageDir, staged, err := stageVerifiedPackage(args[2], args[3], args[4])
	if stageDir != "" {
		defer os.RemoveAll(stageDir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "package verification failed: %v\n", err)
		return true, 1
	}
	// Authentication and verification are still safe to cancel. Do not let the
	// package manager mutate the system until the unprivileged parent has entered
	// its shutdown-blocking critical section and explicitly acknowledges it.
	if _, err := fmt.Fprintln(os.Stdout, privilegedPackageReady); err != nil {
		return true, 1
	}
	ack := make([]byte, len(privilegedPackageAck))
	if _, err := io.ReadFull(os.Stdin, ack); err != nil || string(ack) != privilegedPackageAck {
		fmt.Fprintln(os.Stderr, "package transaction was not authorised by the application")
		return true, 1
	}

	name, installArgs, err := privilegedPackageManagerCommand(args[4], staged)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unsupported package type")
		return true, 2
	}

	out, err := runPrivilegedPackageCommand(name, installArgs...)
	if len(out) != 0 {
		_, _ = os.Stdout.Write(out)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", name, err)
		return true, 1
	}
	return true, 0
}

type officialChecksumReader func(context.Context, string, string) (string, error)

func privilegedPackageManagerCommand(packageKind, staged string) (string, []string, error) {
	switch packageKind {
	case "deb":
		// Use a fixed system path across the privilege boundary. Relying on PATH
		// would let a customised PolicyKit environment select an unprivileged
		// caller's replacement binary. --force-confold keeps user-edited config.
		return privilegedDpkgPath, []string{"-i", "--force-confold", staged}, nil
	case "rpm":
		return privilegedRPMPath, []string{"-U", "--replacepkgs", staged}, nil
	default:
		return "", nil, fmt.Errorf("unsupported package type %q", packageKind)
	}
}

var officialReleaseHosts = map[string]bool{
	"github.com":                            true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
	"github-releases.githubusercontent.com": true,
}

// Keep these paths in sync with the Linux CA bundle locations supported by
// crypto/x509. Unlike x509.SystemCertPool, reading a fixed, root-owned bundle
// directly cannot be redirected through an unprivileged caller's
// SSL_CERT_FILE/SSL_CERT_DIR environment. SystemCertPool also caches its first
// result process-wide, so temporarily unsetting those variables here would be
// too late if another package had already initialized that cache.
var officialSystemCertFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian, Ubuntu, Alpine
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora, RHEL
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS, RHEL
	"/etc/ssl/cert.pem",                                 // fallback used by some distributions
}

// Some supported system trust layouts keep individual roots here rather than
// (or in addition to) a bundle. In particular this is the crypto/x509 fallback
// for SLES, and it also carries locally installed enterprise/rotation roots on
// distributions whose bundle regeneration is asynchronous. These paths are
// fixed for the same privilege-boundary reason as officialSystemCertFiles.
var officialSystemCertDirs = []string{
	"/etc/ssl/certs",
	"/etc/pki/tls/certs",
}

func validateOfficialReleaseURL(u *url.URL) error {
	if u == nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return fmt.Errorf("official release URL must use HTTPS without credentials")
	}
	host := strings.ToLower(u.Hostname())
	if !officialReleaseHosts[host] || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("untrusted official release host %q", u.Host)
	}
	return nil
}

func loadOfficialSystemCertPool(
	readFile func(string) ([]byte, error),
	readDir func(string) ([]os.DirEntry, error),
) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	var failures []string
	for _, path := range officialSystemCertFiles {
		bundle, err := readFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			}
			continue
		}
		if pool.AppendCertsFromPEM(bundle) {
			break
		}
		failures = append(failures, path+": no valid certificates")
	}
	for _, dir := range officialSystemCertDirs {
		entries, err := readDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			certificate, err := readFile(filepath.Join(dir, entry.Name()))
			if err == nil {
				pool.AppendCertsFromPEM(certificate)
			}
		}
	}
	if len(pool.Subjects()) != 0 {
		return pool, nil
	}
	if len(failures) != 0 {
		return nil, fmt.Errorf("cannot load a trusted system CA bundle (%s)", strings.Join(failures, "; "))
	}
	return nil, fmt.Errorf("no trusted system CA bundle found")
}

func officialSystemCertPool() (*x509.CertPool, error) {
	return loadOfficialSystemCertPool(os.ReadFile, os.ReadDir)
}

func newOfficialReleaseClient() (*http.Client, error) {
	roots, err := officialSystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("cannot load system TLS roots: %w", err)
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unsupported default HTTP transport %T", http.DefaultTransport)
	}
	transport := baseTransport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	return &http.Client{
		Transport: transport,
		Timeout:   CheckTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many official release redirects")
			}
			return validateOfficialReleaseURL(req.URL)
		},
	}, nil
}

func readOfficialChecksumContext(ctx context.Context, rawURL, filename string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if err := validateOfficialReleaseURL(u); err != nil {
		return "", err
	}
	client, err := newOfficialReleaseClient()
	if err != nil {
		return "", err
	}
	return readChecksumContextWithClient(ctx, client, rawURL, filename)
}

func verifyOfficialPackageChecksum(ctx context.Context, version, packageKind, suppliedChecksum string, read officialChecksumReader) error {
	if err := validateReleaseVersion(version); err != nil {
		return err
	}
	// Refuse anything that is not newer than what is running.
	//
	// Re-fetching the checksum as root proves the package is an official
	// release; it does not prove it is a release anyone should install now. An
	// OLD official release has an official checksum too, so without this a local
	// process could invoke the helper directly — it is a public pkexec entry
	// point, guarded only by euid — and have root install a version with a known
	// vulnerability. The comparison is against the compiled-in version rather
	// than an argument, for the same reason.
	//
	// Empty means a development build with no version stamped in; there is
	// nothing to compare against, and blocking updates for it would be worse
	// than the risk this guards.
	if RunningVersion != "" && !isNewerVersion(version, RunningVersion) {
		return fmt.Errorf("refusing to install %s over the running %s: not an upgrade", version, RunningVersion)
	}
	if packageKind != "deb" && packageKind != "rpm" {
		return fmt.Errorf("unsupported package type %q", packageKind)
	}
	filename := fmt.Sprintf("%s_%s_linux_%s.%s", BinaryName, strings.TrimPrefix(version, "v"), packageArch(), packageKind)
	officialChecksum, err := read(ctx, releaseURL(version, filename), filename)
	if err != nil {
		return fmt.Errorf("cannot verify the official release checksum: %w", err)
	}
	supplied, err := hex.DecodeString(strings.ToLower(suppliedChecksum))
	if err != nil || len(supplied) != sha256.Size {
		return fmt.Errorf("invalid supplied checksum")
	}
	official, err := hex.DecodeString(strings.ToLower(officialChecksum))
	if err != nil || len(official) != sha256.Size {
		return fmt.Errorf("invalid official checksum")
	}
	if !equalChecksum(supplied, official) {
		return fmt.Errorf("package checksum is not the checksum published for %s", filename)
	}
	return nil
}

func stageVerifiedPackage(sourcePath, trustedChecksum, packageKind string) (stageDir, stagedPath string, err error) {
	checksumBytes, err := hex.DecodeString(strings.ToLower(trustedChecksum))
	if err != nil || len(checksumBytes) != sha256.Size {
		return "", "", fmt.Errorf("invalid trusted checksum")
	}
	if packageKind != "deb" && packageKind != "rpm" {
		return "", "", fmt.Errorf("unsupported package type %q", packageKind)
	}

	fd, err := unix.Open(sourcePath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", "", fmt.Errorf("cannot open downloaded package: %w", err)
	}
	source := os.NewFile(uintptr(fd), sourcePath)
	if source == nil {
		_ = unix.Close(fd)
		return "", "", fmt.Errorf("cannot access downloaded package")
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("downloaded package is not a regular file")
	}
	if info.Size() < 0 || info.Size() > DownloadLimit {
		return "", "", fmt.Errorf("downloaded package exceeds %d byte limit", DownloadLimit)
	}

	stageDir, err = os.MkdirTemp(privilegedPackageTempRoot, "asmgr-package-install-*")
	if err != nil {
		return "", "", fmt.Errorf("cannot create privileged package staging directory: %w", err)
	}
	cleanup := func(cause error) (string, string, error) {
		_ = os.RemoveAll(stageDir)
		return "", "", cause
	}
	if err := os.Chmod(stageDir, 0o700); err != nil {
		return cleanup(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, privilegedPackageStageMarker), []byte(privilegedPackageStageOwner), 0o600); err != nil {
		return cleanup(fmt.Errorf("cannot mark privileged package staging directory: %w", err))
	}
	stagedPath = filepath.Join(stageDir, BinaryName+"."+packageKind)
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return cleanup(fmt.Errorf("cannot create privileged package copy: %w", err))
	}

	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(staged, hash), io.LimitReader(source, DownloadLimit+1))
	if copyErr == nil && n > DownloadLimit {
		copyErr = fmt.Errorf("downloaded package exceeds %d byte limit", DownloadLimit)
	}
	if copyErr == nil {
		copyErr = staged.Sync()
	}
	if closeErr := staged.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return cleanup(fmt.Errorf("cannot stage downloaded package: %w", copyErr))
	}
	actual := hash.Sum(nil)
	if !equalChecksum(actual, checksumBytes) {
		return cleanup(fmt.Errorf("downloaded package changed after verification"))
	}
	return stageDir, stagedPath, nil
}

// cleanStalePrivilegedPackageStages recovers root-owned space after a forced
// timeout or crash, where deferred cleanup cannot run. Prefixes alone are not
// ownership proof in /var/tmp: require our exact regular marker, the current
// effective owner, and an age beyond any supported package transaction.
func cleanStalePrivilegedPackageStages() {
	entries, err := os.ReadDir(privilegedPackageTempRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "asmgr-package-install-") {
			continue
		}
		stageDir := filepath.Join(privilegedPackageTempRoot, entry.Name())
		dirInfo, err := os.Lstat(stageDir)
		if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || !ownedByEffectiveUser(dirInfo) {
			continue
		}
		markerPath := filepath.Join(stageDir, privilegedPackageStageMarker)
		markerInfo, err := os.Lstat(markerPath)
		if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 ||
			!ownedByEffectiveUser(markerInfo) || time.Since(markerInfo.ModTime()) < stageCleanupAge {
			continue
		}
		marker, err := os.Open(markerPath)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(marker, int64(len(privilegedPackageStageOwner)+1)))
		closeErr := marker.Close()
		if readErr != nil || closeErr != nil || string(data) != privilegedPackageStageOwner {
			continue
		}
		_ = os.RemoveAll(stageDir)
	}
}

func ownedByEffectiveUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// runPrivilegedPackageCommand deliberately keeps the package manager in the
// process group inherited from pkexec. The unprivileged parent owns the bounded
// 15-minute context and kills that entire group on timeout. Creating another
// group here would let dpkg/rpm and maintainer scripts survive after the parent
// reports a timeout or shuts down. Output remains capped on both sides of the
// privilege boundary.
func runPrivilegedPackageCommand(name string, args ...string) ([]byte, error) {
	cmd := newPrivilegedPackageCommand(name, args...)
	cmd.WaitDelay = packageCommandWait
	output := &boundedCommandOutput{limit: packageOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return output.Bytes(), err
}

func newPrivilegedPackageCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	// Package managers invoke maintainer scripts and those scripts invoke system
	// tools. An absolute dpkg/rpm path is therefore not enough: inheriting the
	// pkexec caller's PATH (or rpm/dpkg configuration variables) would still let
	// an unprivileged directory select a child executable across the root
	// boundary. Supply a small deterministic environment instead.
	cmd.Env = []string{
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"PATH=" + privilegedExecPath,
	}
	return cmd
}

// runPrivilegedPackageInstall starts pkexec while the update is still in its
// cancellable preparation phase. The root helper verifies and privately stages
// the downloaded package, announces readiness, then blocks on stdin. Only the
// critical callback sends the acknowledgement that lets dpkg/rpm begin. This
// keeps a dismissed PolicyKit prompt from delaying app shutdown while still
// making an already-started package mutation fail-closed.
func runPrivilegedPackageInstall(
	ctx context.Context,
	pkexec string,
	args []string,
	critical func(func() error) error,
) ([]byte, error) {
	// Use a non-cancelling context here so os/exec permits the custom process-
	// group Cancel installed below. Preparation cancellation is coordinated by
	// the explicit watcher; it is disabled before the critical ACK is sent.
	cmd := exec.CommandContext(context.Background(), pkexec, args...)
	configurePackageCommand(cmd)
	cmd.WaitDelay = packageCommandWait
	output := &boundedCommandOutput{limit: packageOutputLimit}
	cmd.Stderr = output
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	var timedOut atomic.Bool
	timeout := time.AfterFunc(packageInstallTimeout, func() {
		timedOut.Store(true)
		_ = cmd.Cancel()
	})
	defer timeout.Stop()

	preparationDone := make(chan struct{})
	watcherDone := make(chan struct{})
	var helperReady atomic.Bool
	var stopPreparationOnce sync.Once
	stopPreparation := func() {
		stopPreparationOnce.Do(func() { close(preparationDone) })
		<-watcherDone
	}
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			if helperReady.Load() {
				// Once the helper reported readiness it is blocked before dpkg/rpm.
				// EOF lets it return through its deferred root-owned staging cleanup;
				// killing the process group here leaked that directory permanently.
				_ = stdin.Close()
			} else {
				_ = cmd.Cancel()
			}
		case <-preparationDone:
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	ready := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == privilegedPackageReady {
			ready = true
			// Publish the protocol state before yielding after the scanner loop.
			// A cancellation in the tiny gap between seeing READY and storing it
			// must close stdin for graceful cleanup, not SIGKILL the staged helper.
			helperReady.Store(true)
			break
		}
		_, _ = output.Write(append([]byte(line), '\n'))
	}
	if !ready {
		stopPreparation()
		_ = stdin.Close()
		// EOF without the exact bounded marker is never a state in which the
		// helper may continue. Cancel before Wait so malformed/noisy helper output
		// cannot leave us blocked on a pipe until the global timeout.
		_ = cmd.Cancel()
		waitErr := cmd.Wait()
		if timedOut.Load() {
			return output.Bytes(), fmt.Errorf("pkexec timed out after %s: %w", packageInstallTimeout, context.DeadlineExceeded)
		}
		if ctx.Err() != nil {
			return output.Bytes(), ctx.Err()
		}
		if scanErr := scanner.Err(); scanErr != nil {
			return output.Bytes(), fmt.Errorf("cannot read package helper readiness: %w", scanErr)
		}
		if waitErr == nil {
			waitErr = fmt.Errorf("package helper exited before transaction readiness")
		}
		return output.Bytes(), waitErr
	}
	// Drain package-manager output after consuming the private readiness line.
	// Without this goroutine a noisy maintainer script could fill the pipe and
	// deadlock the critical transaction.
	drainDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, stdout)
		close(drainDone)
	}()

	actionRan := false
	criticalErr := critical(func() error {
		actionRan = true
		// Stop and join the shutdown-cancellation watcher before acknowledging
		// the helper. After this point only the hard package timeout may kill the
		// transaction, and shutdown waits for this callback to return.
		stopPreparation()
		if _, err := io.WriteString(stdin, privilegedPackageAck); err != nil {
			waitErr := waitPreparedPackageHelper(cmd, stdin)
			<-drainDone
			if waitErr != nil {
				return waitErr
			}
			return err
		}
		_ = stdin.Close()
		waitErr := cmd.Wait()
		<-drainDone
		if timedOut.Load() {
			return fmt.Errorf("pkexec timed out after %s: %w", packageInstallTimeout, context.DeadlineExceeded)
		}
		return waitErr
	})
	if actionRan {
		return output.Bytes(), criticalErr
	}

	// The application closed its critical-section gate during the prompt. No
	// acknowledgement was sent, so the helper cannot have started dpkg/rpm.
	stopPreparation()
	_ = waitPreparedPackageHelper(cmd, stdin)
	<-drainDone
	return output.Bytes(), criticalErr
}

// waitPreparedPackageHelper stops a helper that has staged and verified the
// package but has not received the transaction acknowledgement. Closing stdin
// is the protocol's cancellation signal and lets the root process run deferred
// cleanup. A malformed helper still gets a bounded grace period before the
// complete process group is killed.
func waitPreparedPackageHelper(cmd *exec.Cmd, stdin io.Closer) error {
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(preparedHelperExitWait)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = cmd.Cancel()
		return <-done
	}
}

func equalChecksum(actual, expected []byte) bool {
	if len(actual) != len(expected) {
		return false
	}
	var different byte
	for i := range actual {
		different |= actual[i] ^ expected[i]
	}
	return different == 0
}
