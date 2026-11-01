package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// Compiling the helper for a development build, which carries none: see
// remote/helperbin for what a release does. This is what makes it possible to
// work on the helper without a release cycle, and it fails loudly rather than
// silently sending a stale binary.

// helperBuildOnce keeps a development build from being repeated for every
// server connected in one run.
var helperBuildOnce sync.Map // map[string]*helperBuild

type helperBuild struct {
	once sync.Once
	path string
	err  error
}

// buildHelper compiles one from source, for development builds.
func buildHelper(arch string) (string, error) {
	value, _ := helperBuildOnce.LoadOrStore(arch, &helperBuild{})
	build := value.(*helperBuild)

	build.once.Do(func() {
		root, err := repositoryRoot()
		if err != nil {
			build.err = err
			return
		}

		output := filepath.Join(os.TempDir(), fmt.Sprintf("asmgrd-linux-%s-dev", arch))
		command := exec.Command("go", "build",
			"-ldflags=-s -w",
			"-o", output,
			"./cmd/asmgrd")
		command.Dir = root
		// Static, so it runs on a server whose C library is older than this
		// machine's — which is the common case, since servers are not
		// reinstalled as often as laptops.
		command.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS=linux",
			"GOARCH="+arch,
		)

		if out, err := command.CombinedOutput(); err != nil {
			build.err = fmt.Errorf("could not build the helper for linux/%s: %w\n%s",
				arch, err, out)
			return
		}
		build.path = output
	})

	return build.path, build.err
}

// repositoryRoot finds the source tree, for a development build.
func repositoryRoot() (string, error) {
	// The working directory during development is the repository.
	working, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for directory := working; ; {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return "", fmt.Errorf("this build carries no helper, and this does not look like " +
		"a development checkout to build one from")
}

// LocalGOARCH is exposed for diagnostics, so a log can say what the app itself
// runs on when a helper mismatch is being investigated.
func LocalGOARCH() string { return runtime.GOARCH }
