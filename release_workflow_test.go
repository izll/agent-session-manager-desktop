package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// CheckForUpdate intentionally ignores prereleases. If a beta is published as
// an ordinary release, GitHub may return it from /releases/latest; the updater
// then discards it and never sees the stable release behind it.
func TestEveryPlatformMarksPrereleaseTagsInReleaseWorkflow(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	actions := strings.Count(workflow, "uses: softprops/action-gh-release@3bb12739c298aeb8a4eeaf626c5b8d85266b0e65")
	flags := strings.Count(workflow, "prerelease: ${{ contains(steps.ver.outputs.tag, '-') }}")
	if actions != 3 {
		t.Fatalf("release action count = %d, want Linux, macOS and Windows", actions)
	}
	if flags != actions {
		t.Fatalf("prerelease flags = %d for %d release actions", flags, actions)
	}
}

func TestWorkflowActionsArePinnedToImmutableCommits(t *testing.T) {
	mutable := regexp.MustCompile(`(?m)^\s*-?\s*uses:\s+[^\s@]+@([^\s#]+)`)
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range mutable.FindAllStringSubmatch(string(raw), -1) {
			if !sha.MatchString(match[1]) {
				t.Errorf("%s has mutable third-party action reference %q; pin its full commit SHA", path, match[1])
			}
		}
	}
}

func TestNativeCICompilesAndTestsTheFullApplication(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	macStart := strings.Index(workflow, "  go-test-macos:")
	windowsStart := strings.Index(workflow, "  go-test-windows:")
	if macStart < 0 || windowsStart < 0 || macStart >= windowsStart {
		t.Fatal("native CI jobs are missing or out of order")
	}
	macJob := workflow[macStart:windowsStart]
	windowsJob := workflow[windowsStart:]
	for name, job := range map[string]string{"macOS": macJob, "Windows": windowsJob} {
		for _, required := range []string{"npm run build --prefix frontend", "go test ./...", "go vet ./..."} {
			if !strings.Contains(job, required) {
				t.Errorf("%s CI does not contain %q", name, required)
			}
		}
	}
	for _, required := range []string{"msys2/setup-msys2@", "mingw-w64-ucrt-x86_64-portaudio", "CGO_ENABLED: '1'"} {
		if !strings.Contains(windowsJob, required) {
			t.Errorf("Windows full-app CI is missing %q", required)
		}
	}
}

func TestCIRegeneratesBindingsThroughPinnedWailsBuild(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	backendStart := strings.Index(workflow, "  backend:")
	macStart := strings.Index(workflow, "  go-test-macos:")
	if backendStart < 0 || macStart < 0 || backendStart >= macStart {
		t.Fatal("backend CI job is missing or out of order")
	}
	backendJob := workflow[backendStart:macStart]
	for _, required := range []string{
		"go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0",
		"wails\" build -tags webkit2_41 -clean",
		"npm run check --prefix frontend",
	} {
		if !strings.Contains(backendJob, required) {
			t.Errorf("backend CI does not validate generated Wails bindings with %q", required)
		}
	}
	buildAt := strings.Index(backendJob, "wails\" build -tags webkit2_41 -clean")
	checkAt := strings.LastIndex(backendJob, "npm run check --prefix frontend")
	if buildAt < 0 || checkAt < buildAt {
		t.Fatal("backend CI must type-check after Wails regenerates the bindings")
	}
}
