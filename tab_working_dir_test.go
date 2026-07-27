package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveTabWorkingDirAcceptsExistingAbsolutePath(t *testing.T) {
	live := t.TempDir()
	configured := t.TempDir()

	if got := resolveTabWorkingDir(live+"\n", configured); got != live {
		t.Fatalf("expected the live path %q, got %q", live, got)
	}
}

func TestResolveTabWorkingDirRejectsRelativePath(t *testing.T) {
	configured := t.TempDir()

	if got := resolveTabWorkingDir("relative/dir", configured); got != configured {
		t.Fatalf("expected the configured path %q, got %q", configured, got)
	}
}

func TestResolveTabWorkingDirRejectsMissingPath(t *testing.T) {
	configured := t.TempDir()
	// A pane whose directory was deleted keeps reporting the old path.
	deleted := filepath.Join(t.TempDir(), "gone")

	if got := resolveTabWorkingDir(deleted, configured); got != configured {
		t.Fatalf("expected the configured path %q, got %q", configured, got)
	}
}

func TestResolveTabWorkingDirRejectsFile(t *testing.T) {
	configured := t.TempDir()
	file := filepath.Join(t.TempDir(), "file.txt")
	dashboardWriteFile(t, filepath.Dir(file), "file.txt", "content\n")

	if got := resolveTabWorkingDir(file, configured); got != configured {
		t.Fatalf("expected the configured path %q, got %q", configured, got)
	}
}

func TestResolveTabWorkingDirEmptyTmuxOutputFallsBack(t *testing.T) {
	configured := t.TempDir()

	for _, reported := range []string{"", "\n", "   \n"} {
		if got := resolveTabWorkingDir(reported, configured); got != configured {
			t.Fatalf("expected %q for reported %q, got %q", configured, reported, got)
		}
	}
}

func TestCachedPaneCurrentPathReusesAndExpires(t *testing.T) {
	const target = "asm_test_session:3"
	tabWorkingDirMu.Lock()
	delete(tabWorkingDirCache, target)
	tabWorkingDirMu.Unlock()

	calls := 0
	query := func(ctx context.Context, queried string) string {
		calls++
		if queried != target {
			t.Fatalf("expected target %q, got %q", target, queried)
		}
		if calls == 1 {
			return "/first\n"
		}
		return "/second\n"
	}

	app := &App{}
	if got := app.cachedPaneCurrentPath(target, query); got != "/first\n" {
		t.Fatalf("expected the first answer, got %q", got)
	}
	// The second call must be served from the cache, not from tmux.
	if got := app.cachedPaneCurrentPath(target, query); got != "/first\n" {
		t.Fatalf("expected the cached answer, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected one query, got %d", calls)
	}

	tabWorkingDirMu.Lock()
	entry := tabWorkingDirCache[target]
	entry.expiresAt = time.Now().Add(-time.Second)
	tabWorkingDirCache[target] = entry
	tabWorkingDirMu.Unlock()

	if got := app.cachedPaneCurrentPath(target, query); got != "/second\n" {
		t.Fatalf("expected the refreshed answer, got %q", got)
	}
	if calls != 2 {
		t.Fatalf("expected two queries, got %d", calls)
	}
}
