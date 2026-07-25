package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Frontend-facing APIs for importing terminal colour schemes: from files the
// user picks, from schemes already installed locally (Konsole/kitty/…), and
// from the community iTerm2-Color-Schemes collection online.

// ImportSchemeFiles opens a file picker and parses whatever the user selects.
func (a *App) ImportSchemeFiles() ([]ImportedScheme, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import terminal colour schemes",
		Filters: []runtime.FileFilter{
			{DisplayName: "Colour schemes", Pattern: "*.colorscheme;*.itermcolors;*.conf;*.toml;*.json;*.yml;*.yaml;*.txt"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	var out []ImportedScheme
	for _, p := range paths {
		s, err := parseSchemeFile(p)
		if err != nil {
			continue // skip unparseable picks rather than failing the batch
		}
		out = append(out, *s)
	}
	if len(out) == 0 && len(paths) > 0 {
		return nil, fmt.Errorf("none of the selected files contained a readable palette")
	}
	return out, nil
}

// schemeSearchDirs lists where other terminals keep their schemes.
func schemeSearchDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".local/share/konsole"),
		"/usr/share/konsole",
		filepath.Join(home, ".config/kitty/themes"),
		filepath.Join(home, ".config/alacritty/themes/themes"),
		filepath.Join(home, ".config/wezterm/colors"),
		filepath.Join(home, ".config/ghostty/themes"),
	}
}

// DiscoverLocalSchemes finds colour schemes already installed on this machine
// (Konsole, kitty, Alacritty, WezTerm, Ghostty) so they can be imported
// without hunting for files.
func (a *App) DiscoverLocalSchemes() []ImportedScheme {
	var out []ImportedScheme
	seen := map[string]bool{}

	for _, dir := range schemeSearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			switch ext {
			case ".colorscheme", ".itermcolors", ".conf", ".toml", ".yml", ".yaml":
			default:
				continue
			}
			s, err := parseSchemeFile(filepath.Join(dir, e.Name()))
			if err != nil || s == nil {
				continue
			}
			key := strings.ToLower(s.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// --------------------------------------------------------------- online

// The community collection: ~400 schemes, mirrored in many formats. We pull
// the Konsole variants because that parser is the most reliable of the lot.
const (
	schemeRepoAPI = "https://api.github.com/repos/mbadolato/iTerm2-Color-Schemes/contents/konsole"
	schemeRepoRaw = "https://raw.githubusercontent.com/mbadolato/iTerm2-Color-Schemes/master/konsole/"
)

// OnlineSchemeInfo is one downloadable scheme in the online browser.
type OnlineSchemeInfo struct {
	Name string `json:"name"`
	File string `json:"file"`
}

var (
	onlineIndexCache []OnlineSchemeInfo
	onlineIndexAt    time.Time
)

// ListOnlineSchemes returns the names available in the online collection,
// cached for an hour so browsing doesn't hammer the API.
func (a *App) ListOnlineSchemes() ([]OnlineSchemeInfo, error) {
	if len(onlineIndexCache) > 0 && time.Since(onlineIndexAt) < time.Hour {
		return onlineIndexCache, nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, schemeRepoAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "asmgr-desktop/"+Version)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the scheme collection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scheme collection returned %d", resp.StatusCode)
	}

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}

	var out []OnlineSchemeInfo
	for _, e := range entries {
		if e.Type != "file" || !strings.HasSuffix(e.Name, ".colorscheme") {
			continue
		}
		out = append(out, OnlineSchemeInfo{
			Name: strings.TrimSuffix(e.Name, ".colorscheme"),
			File: e.Name,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	onlineIndexCache = out
	onlineIndexAt = time.Now()
	return out, nil
}

// FetchOnlineSchemes downloads and parses the named schemes.
func (a *App) FetchOnlineSchemes(files []string) ([]ImportedScheme, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	var out []ImportedScheme

	for _, f := range files {
		// Only ever fetch a plain file name from the known directory.
		if f == "" || strings.ContainsAny(f, "/\\") || !strings.HasSuffix(f, ".colorscheme") {
			continue
		}
		req, err := http.NewRequest(http.MethodGet, schemeRepoRaw+f, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "asmgr-desktop/"+Version)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		s, err := parseKonsole(resp.Body, strings.TrimSuffix(f, ".colorscheme"))
		resp.Body.Close()
		if err != nil || s == nil {
			continue
		}
		s.Source = "online"
		out = append(out, *s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("could not download any of the selected schemes")
	}
	return out, nil
}
