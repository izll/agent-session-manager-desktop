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
	failClosedUploads := strings.Count(workflow, "fail_on_unmatched_files: true")
	if failClosedUploads != actions {
		t.Fatalf("fail-closed asset uploads = %d for %d release actions", failClosedUploads, actions)
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
		"find frontend/wailsjs -type f -exec chmod 0644 {} +",
		"git diff --exit-code -- frontend/wailsjs",
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
	diffAt := strings.Index(backendJob, "git diff --exit-code -- frontend/wailsjs")
	if diffAt < checkAt {
		t.Fatal("backend CI must fail on uncommitted generated bindings after regeneration and type-checking")
	}
	modeAt := strings.Index(backendJob, "find frontend/wailsjs -type f -exec chmod 0644 {} +")
	if modeAt < checkAt || modeAt > diffAt {
		t.Fatal("backend CI must normalize Wails' generated executable modes immediately before the binding diff")
	}
}

func TestReleaseFailsOnRegeneratedBindingDrift(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	linuxStart := strings.Index(workflow, "  release:\n")
	macStart := strings.Index(workflow, "  release-macos:\n")
	windowsStart := strings.Index(workflow, "  release-windows:\n")
	if linuxStart < 0 || macStart < 0 || windowsStart < 0 || linuxStart >= macStart || macStart >= windowsStart {
		t.Fatal("native release jobs are missing or out of order")
	}
	jobs := []struct {
		name  string
		body  string
		build string
	}{
		{name: "Linux", body: workflow[linuxStart:macStart], build: "wails build -tags webkit2_41 -clean"},
		{name: "macOS", body: workflow[macStart:windowsStart], build: "wails build -platform darwin/arm64 -clean"},
		{name: "Windows", body: workflow[windowsStart:], build: "wails build -platform windows/amd64 -clean"},
	}
	for _, job := range jobs {
		buildAt := strings.Index(job.body, job.build)
		checkAt := strings.LastIndex(job.body, "npm run check --prefix frontend")
		normalizeAt := strings.Index(job.body, `node -e 'const fs=require("fs"),p="frontend/wailsjs/go/models.ts"`)
		modeAt := strings.Index(job.body, "find frontend/wailsjs -type f -exec chmod 0644 {} +")
		diffAt := strings.Index(job.body, "git diff --exit-code -- frontend/wailsjs")
		if buildAt < 0 || checkAt < buildAt {
			t.Errorf("%s release must type-check after Wails regenerates the bindings", job.name)
		}
		if normalizeAt < checkAt || modeAt < normalizeAt || diffAt < modeAt {
			t.Errorf("%s release must normalize and fail on binding drift after regenerated-binding type-checking", job.name)
		}
	}
}

func TestNativeReleaseJobsExecutePlatformTestsBeforePackaging(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	macStart := strings.Index(workflow, "  release-macos:\n")
	windowsStart := strings.Index(workflow, "  release-windows:\n")
	publishStart := strings.Index(workflow, "  publish-release:\n")
	if macStart < 0 || windowsStart < 0 || publishStart < 0 || macStart >= windowsStart || windowsStart >= publishStart {
		t.Fatal("native release jobs are missing or out of order")
	}
	jobs := []struct {
		name        string
		body        string
		packageGate string
	}{
		{name: "macOS", body: workflow[macStart:windowsStart], packageGate: "- name: Package .tar.gz"},
		{name: "Windows", body: workflow[windowsStart:publishStart], packageGate: "- name: Bundle UCRT64 runtime DLLs"},
	}
	for _, job := range jobs {
		testAt := strings.Index(job.body, "go test ./...")
		vetAt := strings.Index(job.body, "go vet ./...")
		packageAt := strings.Index(job.body, job.packageGate)
		if testAt < 0 || vetAt < testAt || packageAt < vetAt {
			t.Errorf("%s release must run native Go test+vet before packaging", job.name)
		}
	}
}

func TestWindowsCrossBuildVerifiesDownloadedNativeDependency(t *testing.T) {
	raw, err := os.ReadFile("scripts/build-windows.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	shaDefinition := regexp.MustCompile(`(?m)^PA_SHA256="[0-9a-f]{64}"$`)
	if !shaDefinition.MatchString(script) {
		t.Fatal("Windows cross-build must pin PortAudio to an exact SHA-256")
	}
	for _, required := range []string{
		"curl --fail --location --silent --show-error",
		"sha256sum --check --strict",
		`PA_CACHE_KEY="${PA_PKG%.pkg.tar.zst}-$PA_SHA256"`,
		`PA_CACHE_MARKER="$CACHE/pa/$PA_CACHE_KEY/.verified"`,
		`[ ! -f "$PA_DIR/bin/libportaudio.dll" ]`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("Windows cross-build does not fail closed with %q", required)
		}
	}
	verifyAt := strings.Index(script, "sha256sum --check --strict")
	extractAt := strings.Index(script, "tar --zstd -xf")
	if verifyAt < 0 || extractAt < 0 || verifyAt > extractAt {
		t.Fatal("Windows cross-build must verify the native package before extraction")
	}
}

func TestWindowsReleaseDoesNotAdvertiseUnsafeInProcessUpdates(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	installer, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "- name: Package portable .tar.gz") {
		t.Fatal("Windows archive is not identified as a manual portable package")
	}
	for name, content := range map[string]string{
		"release workflow": string(workflow),
		"NSIS installer":   string(installer),
	} {
		for _, misleading := range []string{
			"name matches the in-app updater",
			"so it can update itself without administrator rights",
		} {
			if strings.Contains(content, misleading) {
				t.Errorf("%s still advertises the disabled flat Windows updater with %q", name, misleading)
			}
		}
	}
	if !strings.Contains(string(installer), "Automatic installation stays disabled until a stable launcher") {
		t.Fatal("NSIS packaging does not document the launcher prerequisite for automatic installation")
	}
}

func TestMacBundlesAllowTheLoopbackTerminalTransport(t *testing.T) {
	localATS := regexp.MustCompile(`(?s)<key>NSAppTransportSecurity</key>\s*<dict>.*?<key>NSAllowsLocalNetworking</key>\s*<true\s*/>.*?</dict>`)
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !localATS.Match(raw) {
			t.Errorf("%s does not allow the ws://127.0.0.1 terminal transport", path)
		}
	}
}

func TestMacBundlesDoNotAdvertiseAnUnsupportedGoRuntime(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	// Go 1.25 dropped support for macOS releases older than Monterey. Keep the
	// bundle's install/runtime gate aligned instead of letting Finder offer an
	// application whose Go runtime cannot start on the advertised OS.
	if !regexp.MustCompile(`(?m)^go 1\.25(?:\.\d+)?$`).Match(goMod) {
		t.Fatal("update this regression when changing the Go toolchain's macOS support floor")
	}
	minimum := regexp.MustCompile(`(?s)<key>LSMinimumSystemVersion</key>\s*<string>12\.0\.0</string>`)
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !minimum.Match(raw) {
			t.Errorf("%s advertises a macOS version unsupported by the Go 1.25 runtime", path)
		}
	}
}

func TestMacReleasePublishesTheActualNativeDeploymentFloor(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	required := []string{
		`macho_minimum()`,
		`LC_BUILD_VERSION`,
		`LC_VERSION_MIN_MACOSX`,
		`plutil -replace LSMinimumSystemVersion -string "$ARTIFACT_MIN"`,
		`MACOS_ARTIFACT_MIN_VERSION=$ARTIFACT_MIN`,
		`DECLARED_MIN=$(plutil -extract LSMinimumSystemVersion`,
		`[ "$DECLARED_MIN" != "$MACOS_ARTIFACT_MIN_VERSION" ]`,
	}
	for _, fragment := range required {
		if !strings.Contains(workflow, fragment) {
			t.Errorf("macOS release does not validate native deployment target with %q", fragment)
		}
	}
	adjustAt := strings.Index(workflow, `plutil -replace LSMinimumSystemVersion -string "$ARTIFACT_MIN"`)
	signAt := strings.Index(workflow, `- name: Sign with hardened runtime`)
	if adjustAt < 0 || signAt < adjustAt {
		t.Fatal("macOS deployment target must be finalized before code signing")
	}
}
