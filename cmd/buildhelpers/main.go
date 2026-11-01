// Command buildhelpers compiles the server helper for every architecture and
// leaves it where the application embeds it. Run it from the repository root
// before a release build:
//
//	go run ./cmd/buildhelpers
//
// Written in Go rather than as a script because all three release jobs need it,
// and one of them builds on Windows.
package main

import (
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"asmgr-desktop/remote/helperbin"
)

func main() {
	if err := run(filepath.Join("remote", "helperbin", helperbin.Dir), os.Getenv("VERSION")); err != nil {
		fmt.Fprintln(os.Stderr, "buildhelpers:", err)
		os.Exit(1)
	}
}

func run(directory, version string) error {
	// Nothing left over from another version travels with this one.
	stale, err := filepath.Glob(filepath.Join(directory, "*.gz"))
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	for _, arch := range helperbin.Architectures {
		if err := build(directory, arch, version); err != nil {
			return fmt.Errorf("linux/%s: %w", arch, err)
		}
	}
	return nil
}

func build(directory, arch, version string) error {
	temp, err := os.MkdirTemp("", "asmgrd-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "asmgrd")

	flags := "-s -w"
	if version != "" {
		flags += " -X main.Build=" + version
	}
	command := exec.Command("go", "build", "-trimpath", "-ldflags="+flags, "-o", binary, "./cmd/asmgrd")
	// Static, so it runs on a server whose C library is older than the build
	// machine's, which is the common case: servers are reinstalled less often.
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}

	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	output, err := os.Create(filepath.Join(directory, helperbin.FileName(arch)))
	if err != nil {
		return err
	}
	defer output.Close()
	writer, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return output.Close()
}
