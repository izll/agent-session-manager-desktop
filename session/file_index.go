package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A flat list of every file under the session root, for the browser's
// quick-open. The tree itself loads one directory at a time (see
// file_browser.go), which is right for browsing but leaves nothing to search:
// a name the user half-remembers is usually in a directory they have never
// opened. This walk produces that list once, and the caller caches it.
//
// Every path the walk meets goes through the same containment rule the rest of
// the browser uses — resolved through its symlinks, then checked against the
// resolved root — because a walk visits paths nobody vetted by clicking on
// them.

const (
	// MaxIndexFiles caps how many files the index holds. 20k covers every
	// hand-written repository this is aimed at; past that the index stops and
	// says so (FileIndex.Truncated) rather than pretending to be complete.
	// Roughly 2 MB of JSON at the cap, which the Wails bridge carries without
	// stalling the webview.
	MaxIndexFiles = 20000

	// MaxIndexDepth bounds directory nesting. A cycle cannot occur — symlinked
	// directories are never descended into — but a pathologically deep tree
	// (generated code, an unpacked archive) still costs a stat per level, and
	// nothing a user quick-opens lives 40 directories down.
	MaxIndexDepth = 40
)

// indexSkipDirs are directory names the WALK does not descend into.
//
// This is deliberately larger than browseSkipDirs. Listing a directory is
// something the user asked for by clicking it, and reading a dependency's
// source is a legitimate reason to open a file browser — so the tree hides only
// .git. Walking is different: the whole tree is read whether or not the user
// cares, and in this very repository node_modules alone is the overwhelming
// majority of the files. Indexing it would spend most of the walk, and most of
// the result list, on files that then drown the user's own sources in every
// search.
//
// The skip is never silent: FileIndex.SkippedDirs names every directory that
// was passed over, so the UI can say which ones and offer to include them.
var indexSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".svn":         true,
	".hg":          true,
}

// IndexedFile is one file in the quick-open index.
type IndexedFile struct {
	// Path is relative to the session root, slash-separated — the same shape
	// ListDirectory and ReadFileForBrowse take back.
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// FileIndex is the whole walk's result.
type FileIndex struct {
	Files []IndexedFile `json:"files"`
	// Truncated reports that MaxIndexFiles or MaxIndexDepth stopped the walk, so
	// the UI can say the results are partial instead of implying "no match".
	Truncated bool `json:"truncated"`
	// SkippedDirs lists the paths of directories excluded by indexSkipDirs,
	// relative to the root. Reported so a missing hit is explainable rather
	// than a mystery.
	SkippedDirs []string `json:"skippedDirs"`
	// IncludedAll reports whether the walk was run with the skip list disabled.
	IncludedAll bool `json:"includedAll"`
}

// BuildFileIndex walks the session's working directory and returns every file
// under it.
//
// includeAll disables indexSkipDirs (never the containment check) for the user
// who genuinely wants to find something inside node_modules. It is a separate
// index, not a filter on this one, so the common case never pays for it.
func (i *Instance) BuildFileIndex(includeAll bool) (*FileIndex, error) {
	root, _, err := i.resolveBrowsePath("")
	if err != nil {
		return nil, err
	}

	index := &FileIndex{Files: []IndexedFile{}, SkippedDirs: []string{}, IncludedAll: includeAll}
	walkIndex(root, root, "", 0, includeAll, index)

	// Sorted here rather than in the frontend: the ranking is stable only if
	// equally-scored paths arrive in a fixed order, and doing it once on the Go
	// side beats re-sorting 20k entries in the webview on every keystroke.
	sort.Slice(index.Files, func(a, b int) bool {
		return index.Files[a].Path < index.Files[b].Path
	})
	sort.Strings(index.SkippedDirs)
	return index, nil
}

// walkIndex reads one directory and recurses. It appends to index.Files until
// the cap, at which point it sets Truncated and unwinds.
//
// root is the resolved session root, used for the containment check on every
// entry; dir is the resolved absolute directory being read; rel is dir's path
// relative to the root.
func walkIndex(root, dir, rel string, depth int, includeAll bool, index *FileIndex) {
	if len(index.Files) >= MaxIndexFiles {
		index.Truncated = true
		return
	}
	if depth > MaxIndexDepth {
		index.Truncated = true
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// One unreadable directory (permissions, or it vanished mid-walk) must
		// not cost the user the whole index — the rest of the tree is still
		// worth searching.
		return
	}

	for _, de := range entries {
		if len(index.Files) >= MaxIndexFiles {
			index.Truncated = true
			return
		}

		name := de.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}

		if de.IsDir() {
			if !includeAll && indexSkipDirs[name] {
				index.SkippedDirs = append(index.SkippedDirs, childRel)
				continue
			}
			walkIndex(root, filepath.Join(dir, name), childRel, depth+1, includeAll, index)
			continue
		}

		if de.Type()&os.ModeSymlink != 0 {
			// A symlink is resolved BEFORE anything else is done with it. If it
			// lands outside the tree it is dropped entirely — indexing it would
			// put a path in the picker that ReadFileForBrowse then refuses, and
			// even the name would disclose something outside the root. A
			// symlinked DIRECTORY inside the tree is not descended into either:
			// its contents are reachable through their real path, and following
			// it would list every file twice (or loop, for a link to an
			// ancestor).
			resolved, linkErr := filepath.EvalSymlinks(filepath.Join(dir, name))
			if linkErr != nil || !isWithin(root, resolved) {
				continue
			}
			info, statErr := os.Stat(resolved)
			if statErr != nil || info.IsDir() || !info.Mode().IsRegular() {
				continue
			}
			index.Files = append(index.Files, IndexedFile{Path: childRel, Name: name, Size: info.Size()})
			continue
		}

		info, infoErr := de.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			// Fifos, sockets and devices are not openable by the browser, so an
			// entry for them would only ever produce an error when picked.
			continue
		}
		index.Files = append(index.Files, IndexedFile{Path: childRel, Name: name, Size: info.Size()})
	}
}

// --- Content search --------------------------------------------------------

const (
	// MaxSearchMatches caps the whole result set. Past a few hundred hits the
	// answer is "refine the query", not more rows.
	MaxSearchMatches = 500
	// MaxSearchMatchesPerFile stops one generated file from filling the result
	// list and hiding every other file that matched.
	MaxSearchMatchesPerFile = 20
	// MaxSearchFileBytes skips files too large to be worth grepping inline. The
	// same reasoning as MaxBrowseFileBytes: the browser could not display the
	// result usefully anyway.
	MaxSearchFileBytes = 1 << 20 // 1 MiB
	// maxSearchLineRunes truncates the previewed line. A minified bundle is one
	// line of 200 KB, and shipping it to the UI to render 80 columns of it is
	// pure waste.
	maxSearchLineRunes = 400
)

// ContentMatch is one matching line.
type ContentMatch struct {
	Path string `json:"path"`
	// Line is 1-based, matching what the content pane's gutter shows.
	Line int    `json:"line"`
	Text string `json:"text"`
	// Col is the 0-based byte offset of the match within Text, so the UI can
	// highlight it without re-running the match.
	Col int `json:"col"`
	// Length is the match length in bytes within Text.
	Length int `json:"length"`
}

// ContentSearchResult is the whole grep's result.
type ContentSearchResult struct {
	Matches []ContentMatch `json:"matches"`
	// Truncated reports that MaxSearchMatches stopped the search early.
	Truncated bool `json:"truncated"`
	// FilesSearched counts the files actually read, for the UI's summary.
	FilesSearched int `json:"filesSearched"`
	// FilesSkipped counts files passed over as binary or oversized.
	FilesSkipped int `json:"filesSkipped"`
}

// SearchFileContents greps the indexed files for a literal substring.
//
// Literal, not a regular expression: the query comes from a search box that the
// user types into as they think, and a half-typed regex is either a syntax
// error or — worse — a pattern that quietly matches the wrong thing. A literal
// substring always means what it looks like.
//
// files is the already-built index, so the walk's skip and containment rules
// apply here without being restated. Each path is still re-resolved through
// resolveBrowsePath before it is opened: the index can be seconds old, and a
// path in it could have become a symlink out of the tree since.
func (i *Instance) SearchFileContents(query string, caseSensitive bool, files []IndexedFile) (*ContentSearchResult, error) {
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	if strings.TrimSpace(needle) == "" {
		return nil, fmt.Errorf("no search text given")
	}

	result := &ContentSearchResult{Matches: []ContentMatch{}}
	for _, file := range files {
		if len(result.Matches) >= MaxSearchMatches {
			result.Truncated = true
			break
		}
		if file.Size > MaxSearchFileBytes {
			result.FilesSkipped++
			continue
		}

		abs, _, err := i.resolveBrowsePath(file.Path)
		if err != nil {
			result.FilesSkipped++
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			result.FilesSkipped++
			continue
		}
		if IsBinaryContent(data) {
			result.FilesSkipped++
			continue
		}
		result.FilesSearched++

		perFile := 0
		for lineNo, line := range strings.Split(string(data), "\n") {
			if perFile >= MaxSearchMatchesPerFile {
				break
			}
			if len(result.Matches) >= MaxSearchMatches {
				result.Truncated = true
				break
			}
			hay := line
			if !caseSensitive {
				hay = strings.ToLower(line)
			}
			col := strings.Index(hay, needle)
			if col < 0 {
				continue
			}
			// A CR left by a CRLF file would otherwise render as a stray glyph
			// at the end of every previewed line.
			text := strings.TrimSuffix(line, "\r")
			text, col = trimMatchLine(text, col, len(needle))
			result.Matches = append(result.Matches, ContentMatch{
				Path:   file.Path,
				Line:   lineNo + 1,
				Text:   text,
				Col:    col,
				Length: len(needle),
			})
			perFile++
		}
	}
	return result, nil
}

// trimMatchLine shortens an over-long line to a window around the match and
// returns the adjusted column. Cutting on rune boundaries keeps the preview
// valid UTF-8; cutting on bytes would leave a replacement character next to the
// match, which is exactly where it would be noticed.
func trimMatchLine(line string, col, length int) (string, int) {
	if len(line) <= maxSearchLineRunes {
		return line, col
	}
	start := col - maxSearchLineRunes/4
	if start < 0 {
		start = 0
	}
	for start > 0 && !isRuneStart(line[start]) {
		start--
	}
	end := start + maxSearchLineRunes
	if end > len(line) {
		end = len(line)
	}
	for end < len(line) && !isRuneStart(line[end]) {
		end++
	}
	// A match that straddles the window would be highlighted at the wrong
	// place; keeping the whole of it inside is worth the extra bytes.
	if col+length > end {
		end = col + length
		if end > len(line) {
			end = len(line)
		}
	}
	return line[start:end], col - start
}

// isRuneStart reports whether b is not a UTF-8 continuation byte.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
