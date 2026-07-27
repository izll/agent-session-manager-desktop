package session

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Read-only browsing of a session's working directory. Everything here is
// reachable from the frontend, so every path that arrives is treated as
// hostile: it is resolved through the symlinks it actually points at and then
// checked for containment inside the session root. Nothing in this file
// writes, renames or deletes.

const (
	// MaxBrowseFileBytes caps a single file read. Above this the file is
	// truncated rather than refused, because the first megabyte of a log is
	// still worth reading — but the caller is always told (BrowseFile.Truncated)
	// so the UI never presents a partial file as if it were whole.
	MaxBrowseFileBytes = 1 << 20 // 1 MiB

	// MaxBrowseEntries caps one directory listing. A directory with a hundred
	// thousand entries would otherwise cross the Wails bridge as a single JSON
	// blob and stall the webview; the UI reports the truncation instead.
	MaxBrowseEntries = 2000

	// binarySniffBytes is how much of a file's head is examined for a NUL byte.
	// Git uses 8000 for the same decision, and matching it keeps the file
	// browser's verdict consistent with the diff view's.
	binarySniffBytes = 8000
)

// browseSkipDirs are directory names never listed.
//
// .git is skipped because its contents are git's private database — browsing
// packfiles and refs is noise in a source browser and every entry in there is
// binary or machine-generated.
//
// node_modules is deliberately NOT skipped: unlike .git it holds ordinary
// source the user may legitimately want to read (checking what a dependency
// actually does is a routine reason to open a file browser), and it is only
// ever expanded when the user clicks into it. The entry cap already protects
// against its size, so hiding it would cost more than it saves.
var browseSkipDirs = map[string]bool{
	".git": true,
}

// BrowseEntry is one item in a directory listing.
type BrowseEntry struct {
	// Name is the entry's basename.
	Name string `json:"name"`
	// Path is the entry's path relative to the session root, slash-separated,
	// and is what the frontend sends back to list or read it.
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	// Size is the file size in bytes; meaningless for directories.
	Size int64 `json:"size"`
	// ModTime is RFC3339, formatted here so the frontend does not have to
	// reconcile Go's time encoding with the JS Date parser.
	ModTime string `json:"modTime"`
	// Unreadable marks an entry whose metadata could not be read (permissions,
	// or a file that vanished mid-listing). It is still listed — leaving a hole
	// in the tree is more confusing than showing a greyed-out row.
	Unreadable bool `json:"unreadable"`
}

// BrowseListing is one directory's contents.
type BrowseListing struct {
	// Path is the listed directory relative to the session root ("" for the
	// root itself).
	Path string `json:"path"`
	// AbsPath is the resolved absolute directory on disk, for the UI header.
	AbsPath string        `json:"absPath"`
	Entries []BrowseEntry `json:"entries"`
	// Truncated reports that MaxBrowseEntries was hit and entries were dropped.
	Truncated bool `json:"truncated"`
	// TotalEntries is how many entries the directory really has, so the UI can
	// say how many are hidden.
	TotalEntries int `json:"totalEntries"`
}

// BrowseFile is one file's contents, prepared for display.
type BrowseFile struct {
	Path    string `json:"path"`
	AbsPath string `json:"absPath"`
	// Content is empty when Binary is set — there is nothing displayable.
	Content string `json:"content"`
	Size    int64  `json:"size"`
	// Binary reports a NUL byte in the head of the file. The UI shows a notice
	// rather than mojibake.
	Binary bool `json:"binary"`
	// Truncated reports that only the first MaxBrowseFileBytes were read.
	Truncated bool `json:"truncated"`
}

// resolveBrowsePath validates a frontend-supplied relative path and returns the
// absolute location it denotes, together with the resolved session root the
// containment check used (callers that walk a directory need it to vet the
// symlinks they meet).
//
// The containment check runs on the SYMLINK-RESOLVED path, not on the cleaned
// string: `filepath.Clean` cannot see that `link/x` leaves the tree when `link`
// points at /etc, so a purely lexical check is bypassable by anyone who can
// create a symlink inside the working directory — which the agent running in
// that directory certainly can.
func (i *Instance) resolveBrowsePath(rel string) (abs string, root string, err error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", "", fmt.Errorf("path must be relative to the session directory")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == string(filepath.Separator) {
		clean = ""
	}
	// Reject the obvious escape before touching the filesystem, so a traversal
	// attempt never even triggers a stat outside the tree.
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path escapes the session directory")
	}

	// The root itself may be reached through a symlink (/tmp on macOS, a
	// symlinked project dir on Linux), so resolve it too — otherwise every
	// child would compare against a prefix that never matches.
	root, err = filepath.EvalSymlinks(i.Path)
	if err != nil {
		return "", "", fmt.Errorf("could not open the session directory: %w", err)
	}

	target := root
	if clean != "" {
		target = filepath.Join(root, clean)
	}

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		// A dangling symlink or a missing file: report it, but never fall back
		// to the unresolved path — that is exactly the check being bypassed.
		return "", "", fmt.Errorf("could not open %s: %w", rel, err)
	}
	if !isWithin(root, resolved) {
		return "", "", fmt.Errorf("path escapes the session directory")
	}
	return resolved, root, nil
}

// isWithin reports whether path is root or lives underneath it. Both arguments
// must already be symlink-resolved and absolute.
func isWithin(root, path string) bool {
	if path == root {
		return true
	}
	// The separator matters: without it "/tmp/rootkit" would count as inside
	// "/tmp/root".
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// ListDirectory returns the contents of one directory inside the session tree.
//
// rel is relative to the session's working directory; "" lists the root.
func (i *Instance) ListDirectory(rel string) (*BrowseListing, error) {
	abs, browseRoot, err := i.resolveBrowsePath(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", displayPath(rel), err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", displayPath(rel))
	}

	dirEntries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", displayPath(rel), err)
	}

	relPrefix := filepath.ToSlash(filepath.Clean(rel))
	if relPrefix == "." || rel == "" {
		relPrefix = ""
	}

	listing := &BrowseListing{Path: relPrefix, AbsPath: abs}
	entries := make([]BrowseEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if de.IsDir() && browseSkipDirs[name] {
			continue
		}

		entry := BrowseEntry{Name: name, IsDir: de.IsDir()}
		if relPrefix == "" {
			entry.Path = name
		} else {
			entry.Path = relPrefix + "/" + name
		}

		// A symlink's own Info() describes the link, not its target, so the
		// target has to be consulted to classify the row — but only AFTER the
		// link is resolved and confirmed to land inside the tree. Statting the
		// raw target would follow the link out and leak the size and mtime of a
		// file the user is not allowed to open; such a link is listed as
		// unreadable instead.
		if de.Type()&os.ModeSymlink != 0 {
			resolved, linkErr := filepath.EvalSymlinks(filepath.Join(abs, name))
			if linkErr != nil || !isWithin(browseRoot, resolved) {
				entry.Unreadable = true
			} else if target, statErr := os.Stat(resolved); statErr == nil {
				entry.IsDir = target.IsDir()
				entry.Size = target.Size()
				entry.ModTime = target.ModTime().Format(rfc3339)
			} else {
				entry.Unreadable = true
			}
		} else if fi, infoErr := de.Info(); infoErr == nil {
			entry.Size = fi.Size()
			entry.ModTime = fi.ModTime().Format(rfc3339)
		} else {
			// Permissions, or the entry disappeared between ReadDir and Info.
			// One bad entry must not cost the user the whole listing.
			entry.Unreadable = true
		}

		entries = append(entries, entry)
	}

	sortBrowseEntries(entries)

	listing.TotalEntries = len(entries)
	if len(entries) > MaxBrowseEntries {
		entries = entries[:MaxBrowseEntries]
		listing.Truncated = true
	}
	listing.Entries = entries
	return listing, nil
}

// rfc3339 is time.RFC3339 spelled out to avoid importing time for one constant.
const rfc3339 = "2006-01-02T15:04:05Z07:00"

// sortBrowseEntries puts directories first, then sorts by name. Case-insensitive
// so "README" and "readme" sit together the way a file manager shows them, with
// a case-sensitive tiebreak to keep the order stable.
func sortBrowseEntries(entries []BrowseEntry) {
	sort.SliceStable(entries, func(a, b int) bool {
		x, y := entries[a], entries[b]
		if x.IsDir != y.IsDir {
			return x.IsDir
		}
		lx, ly := strings.ToLower(x.Name), strings.ToLower(y.Name)
		if lx != ly {
			return lx < ly
		}
		return x.Name < y.Name
	})
}

// ReadFileForBrowse returns a file's contents for display.
//
// Large files are truncated rather than refused, and the result always says
// which happened; binary files come back with Binary set and no content, so the
// UI can never render bytes as text.
func (i *Instance) ReadFileForBrowse(rel string) (*BrowseFile, error) {
	if strings.TrimSpace(rel) == "" {
		return nil, fmt.Errorf("no file given")
	}
	abs, _, err := i.resolveBrowsePath(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", displayPath(rel), err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", displayPath(rel))
	}
	// A fifo or a device would block forever on Read, and a socket has nothing
	// to show; only regular files are displayable.
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", displayPath(rel))
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", displayPath(rel), err)
	}
	defer f.Close()

	result := &BrowseFile{
		Path:    filepath.ToSlash(filepath.Clean(rel)),
		AbsPath: abs,
		Size:    info.Size(),
	}

	// Read one byte past the cap so a file exactly at the limit is not reported
	// as truncated, and so the truncation flag never depends on the stat'd size
	// (which can be stale for a file being written).
	buf := make([]byte, 0, MaxBrowseFileBytes+1)
	data, err := readAtMost(f, buf, MaxBrowseFileBytes+1)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", displayPath(rel), err)
	}
	if len(data) > MaxBrowseFileBytes {
		data = data[:MaxBrowseFileBytes]
		result.Truncated = true
	}

	if IsBinaryContent(data) {
		result.Binary = true
		return result, nil
	}
	result.Content = string(data)
	return result, nil
}

// readAtMost fills buf from r with at most limit bytes.
func readAtMost(r io.Reader, buf []byte, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil {
		return nil, err
	}
	return append(buf, data...), nil
}

// IsBinaryContent reports whether data looks like a binary file. A NUL byte in
// the head is the same heuristic git uses; text files essentially never contain
// one, and every common binary format does within its first few KB.
func IsBinaryContent(data []byte) bool {
	head := data
	if len(head) > binarySniffBytes {
		head = head[:binarySniffBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// displayPath is what an error message calls the requested location. Errors are
// shown verbatim in the UI, so the root needs a name of its own.
func displayPath(rel string) string {
	if strings.TrimSpace(rel) == "" {
		return "the session directory"
	}
	return rel
}
