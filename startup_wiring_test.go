package main

import (
	"os"
	"regexp"
	"testing"
)

// startup() must actually call the PATH repair.
//
// It once did not: the helper was written, unit-tested and released, but the
// edit that was supposed to wire it into startup silently matched nothing. The
// result shipped twice — a macOS build that still could not find the agents,
// with passing tests either side of the gap.
//
// Reading the source is crude, but it checks the one thing a unit test of
// EnsureToolPath cannot: that somebody calls it.
func TestStartupCallsEnsureToolPath(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("cannot read app.go: %v", err)
	}
	// Inside func (a *App) startup, before it returns.
	startup := regexp.MustCompile(`(?s)func \(a \*App\) startup\(ctx context\.Context\) \{.*?\n\}`)
	body := startup.Find(src)
	if body == nil {
		t.Fatal("could not find startup() in app.go")
	}
	if !regexp.MustCompile(`session\.EnsureToolPath\(\)`).Match(body) {
		t.Fatal("startup() does not call session.EnsureToolPath(); a GUI launch on macOS will not find tmux or the agents")
	}
}
