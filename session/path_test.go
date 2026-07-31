package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A GUI launch on macOS starts with an empty PATH, so the repair has to work
// from nothing as well as from an existing value — and must never displace what
// the user already has.
func TestEnsureToolPath(t *testing.T) {
	original := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", original) })

	t.Run("keeps the inherited PATH first", func(t *testing.T) {
		mine := t.TempDir()
		os.Setenv("PATH", mine)
		EnsureToolPath()

		dirs := filepath.SplitList(os.Getenv("PATH"))
		if len(dirs) == 0 || dirs[0] != mine {
			t.Fatalf("PATH = %q, want it to still begin with %q", os.Getenv("PATH"), mine)
		}
	})

	t.Run("adds nothing on a second call", func(t *testing.T) {
		// Counting duplicates outright would fail on a machine whose PATH
		// already contains one — several do. What matters is that calling this
		// twice changes nothing the second time.
		os.Setenv("PATH", original)
		EnsureToolPath()
		afterFirst := os.Getenv("PATH")
		EnsureToolPath()
		if got := os.Getenv("PATH"); got != afterFirst {
			t.Fatalf("second call changed PATH:\n first  %q\n second %q", afterFirst, got)
		}
	})

	t.Run("produces a usable PATH from an empty one", func(t *testing.T) {
		os.Setenv("PATH", "")
		EnsureToolPath()
		got := os.Getenv("PATH")
		if strings.HasPrefix(got, string(os.PathListSeparator)) {
			t.Fatalf("PATH = %q, which begins with an empty entry", got)
		}
	})
}
