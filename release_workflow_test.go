package main

import (
	"os"
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
	actions := strings.Count(workflow, "uses: softprops/action-gh-release@v2")
	flags := strings.Count(workflow, "prerelease: ${{ contains(steps.ver.outputs.tag, '-') }}")
	if actions != 3 {
		t.Fatalf("release action count = %d, want Linux, macOS and Windows", actions)
	}
	if flags != actions {
		t.Fatalf("prerelease flags = %d for %d release actions", flags, actions)
	}
}
