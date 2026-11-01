// Package helperbin carries the helper for servers inside the application.
//
// A release has to be able to set a server up by itself: the machine it runs on
// has no Go toolchain and no copy of this source. Inside the binary there is no
// packaging format to get right per platform, and nothing beside the program
// that can go missing.
//
// The files are built by cmd/buildhelpers just before the release build and are
// not in version control. A development build has none, and compiles a helper
// from the checkout instead.
package helperbin

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"io/fs"

	"asmgr-desktop/remote/protocol"
)

// Architectures are the servers a release can set up.
var Architectures = []string{"amd64", "arm64"}

// Dir is where the files go, relative to this package.
const Dir = "bin"

//go:embed all:bin
var embedded embed.FS

// FileName names one helper. The protocol version is part of the name, so a
// stale file left in the directory is simply not found rather than uploaded to
// a server, where the version check would then refuse it.
func FileName(arch string) string {
	return fmt.Sprintf("asmgrd-linux-%s-p%d.gz", arch, protocol.Version)
}

// Binary returns the helper for an architecture, or an error if this build
// carries none.
func Binary(arch string) ([]byte, error) {
	return readBinary(embedded, arch)
}

func readBinary(files fs.FS, arch string) ([]byte, error) {
	compressed, err := fs.ReadFile(files, Dir+"/"+FileName(arch))
	if err != nil {
		return nil, fmt.Errorf("this build carries no helper for linux/%s: %w", arch, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("the helper for linux/%s is damaged: %w", arch, err)
	}
	defer reader.Close()
	binary, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("the helper for linux/%s is damaged: %w", arch, err)
	}
	return binary, nil
}
