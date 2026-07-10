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
