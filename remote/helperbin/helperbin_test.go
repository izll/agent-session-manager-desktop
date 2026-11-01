package helperbin

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"asmgr-desktop/remote/protocol"
)

func compress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestABinaryComesBackAsItWent(t *testing.T) {
	files := fstest.MapFS{Dir + "/" + FileName("amd64"): {Data: compress(t, []byte("helper"))}}
	got, err := readBinary(files, "amd64")
	if err != nil || string(got) != "helper" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAMissingArchitectureIsAnError(t *testing.T) {
	if _, err := readBinary(fstest.MapFS{}, "amd64"); err == nil {
		t.Fatal("no error for a helper that is not there")
	}
}

// Uploaded, a helper of another protocol would fail the version check on the
// server, and every connection to it with that.
func TestAHelperOfAnotherProtocolIsNotFound(t *testing.T) {
	stale := fmt.Sprintf("%s/asmgrd-linux-amd64-p%d.gz", Dir, protocol.Version-1)
	if _, err := readBinary(fstest.MapFS{stale: {Data: compress(t, []byte("old"))}}, "amd64"); err == nil {
		t.Fatal("a helper of another protocol was accepted")
	}
}

func TestDamagedDataIsAnError(t *testing.T) {
	files := fstest.MapFS{Dir + "/" + FileName("amd64"): {Data: []byte("not gzip")}}
	if _, err := readBinary(files, "amd64"); err == nil {
		t.Fatal("no error for damaged data")
	}
}

// The release runs this with ASMGR_REQUIRE_HELPERS set, after cmd/buildhelpers
// and before the real build: a release that cannot set up a server should fail
// on the day it is built, not on a user's first connection.
func TestReleaseCarriesEveryHelper(t *testing.T) {
	if os.Getenv("ASMGR_REQUIRE_HELPERS") == "" {
		t.Skip("only for a release build")
	}
	for _, arch := range Architectures {
		binary, err := Binary(arch)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(binary, []byte("\x7fELF")) {
			t.Fatalf("the helper for %s is not a Linux executable", arch)
		}
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	binary, err := Binary("amd64")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "asmgrd")
	if err := os.WriteFile(path, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("the helper does not run: %v: %s", err, output)
	}
	if want := fmt.Sprintf("protocol=%d", protocol.Version); !strings.Contains(string(output), want) {
		t.Fatalf("the helper says %q, want %s", output, want)
	}
}
