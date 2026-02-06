package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	RepoOwner           = "izll"
	RepoName            = "agent-session-manager"
	BinaryName          = "asmgr"
	CheckTimeout        = 5 * time.Second
	CheckInterval       = 24 * time.Hour // Check for updates once per day
	LastCheckFile       = "last_update_check"
	AvailableUpdateFile = "available_update"
)

type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

// getConfigDir returns the config directory path
func getConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".config", "agent-session-manager")
}

// ShouldCheckForUpdate returns true if enough time has passed since the last check
func ShouldCheckForUpdate() bool {
	configDir := getConfigDir()
	if configDir == "" {
		return true // If we can't determine config dir, check anyway
	}

	lastCheckPath := filepath.Join(configDir, LastCheckFile)
	data, err := os.ReadFile(lastCheckPath)
	if err != nil {
		return true // File doesn't exist or can't read, check anyway
	}

	lastCheck, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true // Invalid timestamp, check anyway
	}

	return time.Since(lastCheck) >= CheckInterval
}

// SaveLastCheckTime saves the current time as the last update check time
func SaveLastCheckTime() {
	configDir := getConfigDir()
	if configDir == "" {
		return
	}

	// Ensure config dir exists
	os.MkdirAll(configDir, 0755)

	lastCheckPath := filepath.Join(configDir, LastCheckFile)
	os.WriteFile(lastCheckPath, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// GetCachedAvailableUpdate returns the cached available update version (if any)
func GetCachedAvailableUpdate() string {
	configDir := getConfigDir()
	if configDir == "" {
		return ""
	}

	cachePath := filepath.Join(configDir, AvailableUpdateFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// SaveAvailableUpdate caches the available update version
func SaveAvailableUpdate(version string) {
	configDir := getConfigDir()
	if configDir == "" {
		return
	}

	os.MkdirAll(configDir, 0755)
	cachePath := filepath.Join(configDir, AvailableUpdateFile)

	if version == "" {
		// No update available - remove cache file
		os.Remove(cachePath)
	} else {
		os.WriteFile(cachePath, []byte(version), 0644)
	}
}

// ClearAvailableUpdate removes the cached update (call after successful update)
func ClearAvailableUpdate() {
	configDir := getConfigDir()
	if configDir == "" {
		return
	}
	cachePath := filepath.Join(configDir, AvailableUpdateFile)
	os.Remove(cachePath)
}

// IsPackageManaged checks if the binary was installed via a package manager
func IsPackageManaged() bool {
	// Check if installed via dpkg (Debian/Ubuntu)
	if _, err := os.Stat("/var/lib/dpkg/info/asmgr.list"); err == nil {
		return true
	}

	// Check if installed via rpm (RedHat/Fedora)
	// Check for asmgr specifically in rpm database
	if _, err := os.Stat("/var/lib/rpm"); err == nil {
		// Try to verify with rpm command if available
		execPath, _ := os.Executable()
		if strings.HasPrefix(execPath, "/usr/") {
			// Likely system-wide rpm install
			return true
		}
	}

	return false
}

// CheckForUpdate checks if a newer version is available
// Returns the new version string if available, empty string if up to date
func CheckForUpdate(currentVersion string) string {
	client := &http.Client{Timeout: CheckTimeout}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", RepoOwner, RepoName)

	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	currentVer := strings.TrimPrefix(currentVersion, "v")
	latestVer := strings.TrimPrefix(release.TagName, "v")

	if latestVer != currentVer && latestVer > currentVer {
		return release.TagName
	}

	return ""
}

// DownloadDeb downloads the .deb package to /tmp and returns the path
func DownloadDeb(version string) (string, error) {
	arch := runtime.GOARCH
	verNum := strings.TrimPrefix(version, "v")

	// Map Go arch to deb arch (GoReleaser uses x86_64/aarch64)
	debArch := arch
	if arch == "amd64" {
		debArch = "x86_64"
	} else if arch == "arm64" {
		debArch = "aarch64"
	}

	filename := fmt.Sprintf("%s_%s_linux_%s.deb", BinaryName, verNum, debArch)
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		RepoOwner, RepoName, version, filename)

	// Download to temp file
	tmpFile := fmt.Sprintf("/tmp/%s", filename)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Write to temp file
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	return tmpFile, nil
}

// DownloadAndInstallDeb downloads the .deb package and installs it via dpkg
// This is called from background goroutine
func DownloadAndInstallDeb(version string) error {
	tmpFile, err := DownloadDeb(version)
	if err != nil {
		return err
	}

	// Install with dpkg via sudo
	cmd := exec.Command("sudo", "dpkg", "-i", tmpFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("dpkg installation failed: %w", err)
	}

	// Clean up
	os.Remove(tmpFile)

	return nil
}

// DownloadRpm downloads the .rpm package to /tmp and returns the path
func DownloadRpm(version string) (string, error) {
	arch := runtime.GOARCH
	verNum := strings.TrimPrefix(version, "v")

	// Map Go arch to rpm arch
	rpmArch := arch
	if arch == "amd64" {
		rpmArch = "x86_64"
	} else if arch == "arm64" {
		rpmArch = "aarch64"
	}

	filename := fmt.Sprintf("%s_%s_linux_%s.rpm", BinaryName, verNum, rpmArch)
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		RepoOwner, RepoName, version, filename)

	// Download to temp file
	tmpFile := fmt.Sprintf("/tmp/%s", filename)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Write to temp file
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	return tmpFile, nil
}

// DownloadAndInstallRpm downloads the .rpm package and installs it via rpm
func DownloadAndInstallRpm(version string) error {
	tmpFile, err := DownloadRpm(version)
	if err != nil {
		return err
	}

	// Install with rpm via sudo (rpm -Uvh = upgrade with verbose and hash marks)
	cmd := exec.Command("sudo", "rpm", "-Uvh", tmpFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rpm installation failed: %w", err)
	}

	// Clean up
	os.Remove(tmpFile)

	return nil
}

// DownloadAndInstall downloads and installs the specified version
func DownloadAndInstall(version string) error {
	// Check if installed via package manager
	if IsPackageManaged() {
		// Check if dpkg - use deb update
		if _, err := os.Stat("/var/lib/dpkg/info/asmgr.list"); err == nil {
			return DownloadAndInstallDeb(version)
		}
		// Otherwise assume rpm
		return DownloadAndInstallRpm(version)
	}

	osName := runtime.GOOS
	arch := runtime.GOARCH

	verNum := strings.TrimPrefix(version, "v")
	filename := fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, verNum, osName, arch)
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		RepoOwner, RepoName, version, filename)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve symlinks: %w", err)
	}

	// Extract binary from tarball
	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	var binaryData []byte
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}

		if header.Name == BinaryName {
			binaryData, err = io.ReadAll(tarReader)
			if err != nil {
				return fmt.Errorf("failed to read binary: %w", err)
			}
			break
		}
	}

	if binaryData == nil {
		return fmt.Errorf("binary not found in archive")
	}

	// Check if we have write permission to the directory
	execDir := filepath.Dir(execPath)
	testFile := filepath.Join(execDir, ".asmgr_update_test")
	if err := os.WriteFile(testFile, []byte{}, 0644); err != nil {
		// No write permission - probably installed system-wide
		if strings.HasPrefix(execPath, "/usr/") {
			return fmt.Errorf("system-wide installation detected - please update with: curl -fsSL https://raw.githubusercontent.com/izll/agent-session-manager/main/install.sh | sudo bash")
		}
		return fmt.Errorf("no write permission to %s - try reinstalling with write permissions to your user directory", execDir)
	}
	os.Remove(testFile)

	// Write new binary
	tmpPath := execPath + ".new"
	if err := os.WriteFile(tmpPath, binaryData, 0755); err != nil {
		return fmt.Errorf("failed to write new binary: %w", err)
	}

	// Replace old binary
	oldPath := execPath + ".old"
	os.Remove(oldPath)

	if err := os.Rename(execPath, oldPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to backup old binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Rename(oldPath, execPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	os.Remove(oldPath)

	return nil
}
