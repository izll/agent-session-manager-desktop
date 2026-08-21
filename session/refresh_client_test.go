package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refresh-client's -t takes a client (a tty path like /dev/pts/159), not a
// session name. Passing a session name fails with "can't find client", and
// because these calls discard their result the failure is invisible — the
// only symptom is a pane showing stale, mis-wrapped content until the user
// presses Refresh by hand. Every such call shipped that way and none of them
// ever ran.
//
// This reads the source because the alternative is a live multiplexer: what
// needs guarding is that no caller goes back to passing a session name.
func TestNoRefreshClientWithSessionName(t *testing.T) {
	roots := []string{"..", "."}
	seen := map[string]bool{}

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			abs, _ := filepath.Abs(path)
			if seen[abs] {
				return nil
			}
			seen[abs] = true

			// tmux.go holds the helper, which legitimately names the command.
			if filepath.Base(path) == "tmux.go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for i, line := range strings.Split(string(b), "\n") {
				if !strings.Contains(line, `"refresh-client"`) {
					continue
				}
				// A bare -t here means a session name was passed; the helper
				// resolves clients first and is the only correct form.
				if strings.Contains(line, `"-t"`) {
					t.Errorf("%s:%d passes a session name to refresh-client; "+
						"use session.RefreshSessionClients() instead:\n  %s",
						path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}

// The helper must resolve clients before refreshing them; refreshing by
// session name is exactly the bug it exists to prevent.
func TestRefreshSessionClientsResolvesClientsFirst(t *testing.T) {
	b, err := os.ReadFile("tmux.go")
	if err != nil {
		t.Fatalf("reading tmux.go: %v", err)
	}
	src := string(b)

	start := strings.Index(src, "func RefreshSessionClientsContext(")
	if start < 0 {
		t.Fatal("RefreshSessionClientsContext not found")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}

	if !strings.Contains(body, `"list-clients"`) {
		t.Error("RefreshSessionClients does not look up clients — refreshing by " +
			"session name fails with \"can't find client\"")
	}
	if !strings.Contains(body, "client_name") {
		t.Error("RefreshSessionClients does not read client names from list-clients")
	}
}
