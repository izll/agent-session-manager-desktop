//go:build !windows

package session

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Against tmux itself: a pane whose process has exited goes on reporting the old
// pid. Found on a real machine, where a dead tab of pbx-go was read through that
// pid after the system gave it to a process of WebERP's.
func TestADeadPaneHasNoLiveProcess(t *testing.T) {
	tmux := isolatedTmux(t)
	if _, err := tmux("new-session", "-d", "-s", "live", "-x", "80", "-y", "24", "sleep 60"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-window", "-d", "-t", "live:", "-n", "gone", "true"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	pid, ok := LivePanePIDContext(ctx, "live:0")
	if !ok || pid <= 0 {
		t.Fatalf("a running pane: %d, %v", pid, ok)
	}

	deadline := time.Now().Add(5 * time.Second)
	for paneFormat(t, tmux, "live:1", "#{pane_dead}") != "1" {
		if time.Now().After(deadline) {
			t.Fatal("the pane never died")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The trap itself: tmux still gives a pid for it.
	if reported, err := strconv.Atoi(paneFormat(t, tmux, "live:1", "#{pane_pid}")); err != nil || reported <= 0 {
		t.Fatalf("expected tmux to go on reporting a pid: %d, %v", reported, err)
	}
	if pid, ok := LivePanePIDContext(ctx, "live:1"); ok {
		t.Fatalf("a dead pane gave a process: %d", pid)
	}
}

// Every one of these goes on to read /proc for the pid, and one of them to signal
// it. A new one reading #{pane_pid} directly brings the same bug back.
func TestNothingReadsAPanePIDWithoutCheckingItIsAlive(t *testing.T) {
	direct := regexp.MustCompile(`"#\{pane_pid\}"`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if direct.Match(source) {
			t.Errorf("%s reads #{pane_pid} directly; use LivePanePIDContext", name)
		}
	}
	root, err := os.ReadFile("../app.go")
	if err != nil {
		t.Fatal(err)
	}
	if direct.Match(root) {
		t.Error("app.go reads #{pane_pid} directly; use session.LivePanePIDContext")
	}
}
