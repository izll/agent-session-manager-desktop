package whatsnew

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"asmgr-desktop/updater"
)

// SeenFile holds the version whose notes were last shown. It lives beside the
// app's other files rather than in the settings: settings are stored per
// project, and notes seen in one project would pop up again in the next.
const SeenFile = "whats_new_seen"

// seenLimit bounds the read: the file holds one version number.
const seenLimit = 256

// ConfigDir is the app's configuration directory — the same one the session
// store and the updater use.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agent-session-manager-desktop")
}

// priorInstallMarkers are files only an earlier run leaves behind: the
// session store of the default project, the project list, and the update
// check's timestamp. The directory itself proves nothing — this run's log
// is written into it before anything asks.
var priorInstallMarkers = []string{"sessions.json", "projects.json", "projects", "last_update_check"}

// HadPriorInstall reports whether an earlier run left its configuration in
// dir. It has to be asked before this run writes any of it.
func HadPriorInstall(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range priorInstallMarkers {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// LoadSeen returns the recorded version, "" when there is none. An unreadable
// file reads as none.
func LoadSeen(dir string) string {
	if dir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, SeenFile))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, seenLimit+1))
	if err != nil || len(data) > seenLimit {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// MarkSeen records version as seen. It never moves the record backwards: a
// downgrade leaves the newer version in place, so updating again does not show
// notes that were already read.
func MarkSeen(dir, version string) error {
	if dir == "" {
		return fmt.Errorf("no configuration directory")
	}
	if !updater.IsRelease(version) {
		return fmt.Errorf("not a release version: %q", version)
	}
	if cmp, ok := updater.CompareReleases(version, LoadSeen(dir)); ok && cmp <= 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Written beside the target and renamed over it, so a crash mid-write
	// cannot leave half a version behind.
	tmp, err := os.CreateTemp(dir, SeenFile+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := tmp.WriteString(version + "\n")
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmp.Name())
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, SeenFile)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
