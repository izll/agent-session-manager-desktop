package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every file this package owns is written through writeFileAtomic.
//
// The helper fsyncs before the rename; a plain WriteFile+Rename does not. A
// rename is metadata the filesystem may journal ahead of the data blocks it
// points at, so without the sync a power cut can leave a file of the right name
// and the wrong length. That is survivable for a cache and not for these files:
// sessions.json is every session the user has, and the backups beside it exist
// precisely to survive the failure that would otherwise corrupt them.
//
// This was a real gap. The commits that added Sync() to the task files and the
// file editor left out the session store, the project catalogue and the backups
// — the three that matter most — so the check is mechanical rather than trusting
// the next writer to remember.
func TestDurableWritesGoThroughTheAtomicHelper(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	// A temp file renamed into place: the shape that needs the sync.
	renameCall := regexp.MustCompile(`os\.Rename\(\s*tmp`)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// file_edit.go defines the helper and stages writes for the editor's
		// conflict check, which renames a path it staged earlier.
		if name == "file_edit.go" {
			continue
		}

		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		// Checked per rename, not per file: a file can hold one durable write
		// and one that is not, which is exactly the state storage.go was in —
		// the tasks nearby were synced while the session store was not.
		source := string(data)
		for _, at := range renameCall.FindAllStringIndex(source, -1) {
			// The staging that precedes this rename. Far enough back to cover a
			// write with its error handling, short enough not to reach the
			// previous unrelated one.
			start := at[0] - 800
			if start < 0 {
				start = 0
			}
			if !strings.Contains(source[start:at[0]], ".Sync()") {
				t.Errorf("%s renames a temp file that was not fsynced (near offset %d); use writeFileAtomic",
					name, at[0])
			}
		}
	}
}

// And the helper itself keeps the sync. Removing it would leave every caller
// above looking correct while none of them were durable.
func TestAtomicHelperSyncsBeforeRename(t *testing.T) {
	data, err := os.ReadFile("file_edit.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)

	staging := source[strings.Index(source, "func stageFileAtomic("):]
	if end := strings.Index(staging, "\nfunc "); end > 0 {
		staging = staging[:end]
	}
	if !strings.Contains(staging, "tmp.Sync()") {
		t.Error("stageFileAtomic must fsync before the file is renamed into place")
	}
}

// The session store survives being written and read back, with its mode intact.
// The durability itself cannot be tested without cutting power, so this pins
// what can be checked: the file is complete, parseable and owner-only.
func TestSessionStoreWriteLeavesNoDebris(t *testing.T) {
	storage := newTestStorage(t)

	if err := storage.SaveAll([]*Instance{{ID: "a", Name: "API", Path: "/tmp/api"}}, nil, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	// No staging file outlives a successful save, under the old fixed name or a
	// CreateTemp one.
	entries, err := os.ReadDir(filepath.Dir(storage.configPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".asmgr-") {
			t.Errorf("staging file left behind: %s", name)
		}
	}

	info, err := os.Stat(storage.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("store permissions = %o, want 600 — it can hold an API key", perm)
	}

	loaded, err := storage.Load()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("store did not read back: %v, %d instances", err, len(loaded))
	}
}
