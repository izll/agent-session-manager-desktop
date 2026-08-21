package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"asmgr-desktop/session"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Exporting sessions to a file and importing them back. The in-app "import"
// copies sessions between projects; this pair moves them between machines or
// keeps a snapshot.

// PortableSessionInfo is one session in an export file, as shown to the user
// before importing.
type PortableSessionInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Agent     string `json:"agent"`
	GroupName string `json:"groupName"`
	Tabs      int    `json:"tabs"`
	// PathExists is false when the directory isn't on this machine — the
	// session can still be imported, but it won't start until the path is
	// fixed.
	PathExists bool `json:"pathExists"`
}

// PortableFileInfo describes a parsed export file.
type PortableFileInfo struct {
	Path       string                `json:"path"`
	ExportedAt string                `json:"exportedAt"`
	AppVersion string                `json:"appVersion"`
	Sessions   []PortableSessionInfo `json:"sessions"`
}

// ExportSessions writes the chosen sessions to a file the user picks. An empty
// sessionIDs list exports every session in the current project.
func (a *App) ExportSessions(sessionIDs []string) (string, error) {
	instances, groups, err := a.storage.LoadAll()
	if err != nil {
		return "", err
	}

	selected := instances
	if len(sessionIDs) > 0 {
		wanted := make(map[string]bool, len(sessionIDs))
		for _, id := range sessionIDs {
			wanted[id] = true
		}
		selected = selected[:0:0]
		for _, inst := range instances {
			if wanted[inst.ID] {
				selected = append(selected, inst)
			}
		}
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("there are no sessions to export")
	}

	suggested := fmt.Sprintf("asmgr-sessions-%s.json", time.Now().Format("2006-01-02"))
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export sessions",
		DefaultFilename: suggested,
		Filters: []runtime.FileFilter{
			{DisplayName: "Session export (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // cancelled
	}
	if !strings.HasSuffix(strings.ToLower(path), ".json") {
		path += ".json"
	}

	bundle := session.ToPortable(selected, groups, Version)

	// Write to a temporary file first so a failure can't truncate a file the
	// user already had.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".asmgr-export-*")
	if err != nil {
		return "", fmt.Errorf("cannot write next to %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if werr := session.WritePortable(tmp, bundle); werr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", werr
	}
	if cerr := tmp.Close(); cerr != nil {
		os.Remove(tmpPath)
		return "", cerr
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("cannot save %s: %w", path, rerr)
	}

	log.Printf("[export] %d sessions -> %s", len(bundle.Sessions), path)
	return path, nil
}

// ReadSessionFile opens a file picker and returns what the chosen export
// contains, so the user can review it before importing.
func (a *App) ReadSessionFile() (*PortableFileInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import sessions from file",
		Filters: []runtime.FileFilter{
			{DisplayName: "Session export (*.json)", Pattern: "*.json"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil // cancelled
	}
	return a.readSessionFileAt(path)
}

func (a *App) readSessionFileAt(path string) (*PortableFileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	bundle, err := session.ReadPortable(f)
	if err != nil {
		return nil, err
	}

	info := &PortableFileInfo{
		Path:       path,
		AppVersion: bundle.AppVersion,
	}
	if !bundle.ExportedAt.IsZero() {
		info.ExportedAt = bundle.ExportedAt.Format("2006-01-02 15:04")
	}
	for _, s := range bundle.Sessions {
		info.Sessions = append(info.Sessions, PortableSessionInfo{
			Name:       s.Name,
			Path:       s.Path,
			Agent:      string(s.Agent),
			GroupName:  s.GroupName,
			Tabs:       len(s.Tabs),
			PathExists: session.PathExists(s.Path),
		})
	}
	return info, nil
}

// ImportSessionFile adds the named sessions from an export file to the current
// project. Names are used to identify them because an export has no IDs — they
// are re-generated on import so two machines can't collide.
func (a *App) ImportSessionFile(path string, names []string) (int, error) {
	done, err := a.beginProjectMutation()
	if err != nil {
		return 0, err
	}
	defer done()

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	bundle, err := session.ReadPortable(f)
	if err != nil {
		return 0, err
	}

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	selected := make([]session.PortableSession, 0, len(bundle.Sessions))
	for _, ps := range bundle.Sessions {
		if len(wanted) > 0 && !wanted[ps.Name] {
			continue
		}
		selected = append(selected, ps)
	}
	if len(selected) == 0 {
		return 0, fmt.Errorf("none of the selected sessions were found in the file")
	}
	added, err := a.storage.ImportPortableSessions(selected, bundle.Groups)
	if err != nil {
		return 0, err
	}
	log.Printf("[export] imported %d sessions from %s", added, path)
	return added, nil
}

// uniqueSessionName appends a counter until the name is free.
func uniqueSessionName(base string, taken map[string]bool) string {
	name := strings.TrimSpace(base)
	if name == "" {
		name = "Imported session"
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", name, n)
		if !taken[candidate] {
			return candidate
		}
	}
}
