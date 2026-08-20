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

	instances, groups, err := a.storage.LoadAll()
	if err != nil {
		return 0, err
	}

	// Groups travel by name: match an existing one, otherwise create it.
	groupIDByName := make(map[string]string, len(groups))
	for _, g := range groups {
		groupIDByName[g.Name] = g.ID
	}
	for _, pg := range bundle.Groups {
		if _, ok := groupIDByName[pg.Name]; ok {
			continue
		}
		ng := &session.Group{
			ID:           fmt.Sprintf("grp_%d", time.Now().UnixNano()),
			Name:         pg.Name,
			Color:        pg.Color,
			BgColor:      pg.BgColor,
			FullRowColor: pg.FullRowColor,
		}
		groups = append(groups, ng)
		groupIDByName[pg.Name] = ng.ID
		// Nanosecond timestamps are the existing ID scheme; make sure two
		// groups created in the same loop can't share one.
		time.Sleep(time.Nanosecond)
	}

	// A name already in use would be confusing in the list, so imported
	// duplicates are suffixed rather than silently merged.
	taken := make(map[string]bool, len(instances))
	for _, inst := range instances {
		taken[inst.Name] = true
	}

	added := 0
	for _, ps := range bundle.Sessions {
		if len(wanted) > 0 && !wanted[ps.Name] {
			continue
		}
		ps.Name = uniqueSessionName(ps.Name, taken)
		taken[ps.Name] = true
		instances = append(instances, ps.FromPortable(groupIDByName[ps.GroupName]))
		added++
	}
	if added == 0 {
		return 0, fmt.Errorf("none of the selected sessions were found in the file")
	}

	if err := a.storage.SaveWithGroups(instances, groups); err != nil {
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
