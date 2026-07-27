package main

import (
	"sync"
	"time"

	"asmgr-desktop/session"
)

// The file browser's quick-open needs a flat list of every file under the
// session root, which the lazily-loaded tree cannot supply. The walk is one
// pass over the working directory, so it is done once and reused for a short
// while — the same shape as the git branch cache, including its rule that a
// failed lookup is never cached.

// fileIndexCacheTTL is short on purpose. An agent working in the directory
// creates and deletes files constantly, and a quick-open that offers a file
// deleted a minute ago is worse than one that costs an extra walk. Ten seconds
// covers a burst of keystrokes in the picker without outliving the tree.
const fileIndexCacheTTL = 10 * time.Second

type fileIndexCacheEntry struct {
	index     *session.FileIndex
	expiresAt time.Time
	// Populated while a walk is in flight so concurrent callers wait on the one
	// walk instead of each starting their own. A large repository takes long
	// enough that the picker opening twice in quick succession would otherwise
	// double the work.
	done chan struct{}
}

// fileIndexKey identifies one cached walk. includeAll produces a genuinely
// different list, so it is part of the key rather than a filter applied after.
type fileIndexKey struct {
	sessionID  string
	includeAll bool
}

var (
	fileIndexMu    sync.Mutex
	fileIndexCache = map[fileIndexKey]*fileIndexCacheEntry{}
)

// SearchSessionFileIndex returns the flat list of files under a session's
// working directory, for the browser's quick-open.
//
// includeAll disables the walk's skip list (node_modules and friends); it never
// disables the containment check, which lives in session.BuildFileIndex.
func (a *App) SearchSessionFileIndex(id string, includeAll bool) (*session.FileIndex, error) {
	return cachedFileIndex(fileIndexKey{sessionID: id, includeAll: includeAll}, func() (*session.FileIndex, error) {
		return a.buildFileIndex(id, includeAll)
	})
}

// cachedFileIndex is the cache itself, with the walk passed in so it can be
// exercised without a real Storage behind it.
func cachedFileIndex(key fileIndexKey, build func() (*session.FileIndex, error)) (*session.FileIndex, error) {
	for {
		fileIndexMu.Lock()
		entry, ok := fileIndexCache[key]
		if ok && entry.done == nil && time.Now().Before(entry.expiresAt) {
			fileIndexMu.Unlock()
			return entry.index, nil
		}
		if ok && entry.done != nil {
			// Another call is already walking this tree; wait for it rather
			// than walking it a second time.
			waiting := entry.done
			fileIndexMu.Unlock()
			<-waiting
			continue
		}

		inFlight := &fileIndexCacheEntry{done: make(chan struct{})}
		fileIndexCache[key] = inFlight
		fileIndexMu.Unlock()

		index, err := build()

		fileIndexMu.Lock()
		if err != nil {
			// A failed walk is not an answer. Caching it would keep quick-open
			// broken for the whole TTL after one transient permission error.
			delete(fileIndexCache, key)
		} else {
			fileIndexCache[key] = &fileIndexCacheEntry{
				index:     index,
				expiresAt: time.Now().Add(fileIndexCacheTTL),
			}
		}
		fileIndexMu.Unlock()
		close(inFlight.done)
		return index, err
	}
}

// buildFileIndex is the uncached walk.
func (a *App) buildFileIndex(id string, includeAll bool) (*session.FileIndex, error) {
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}
	return inst.BuildFileIndex(includeAll)
}

// InvalidateSessionFileIndex drops the cached walk for a session, so the next
// quick-open re-reads the tree. Called by the browser's refresh button, which
// is the user saying the tree has changed.
func (a *App) InvalidateSessionFileIndex(id string) {
	fileIndexMu.Lock()
	defer fileIndexMu.Unlock()
	// An in-flight walk is left alone: it will store its own result, which is
	// newer than whatever the caller wanted dropped anyway.
	for _, includeAll := range []bool{false, true} {
		key := fileIndexKey{sessionID: id, includeAll: includeAll}
		if entry, ok := fileIndexCache[key]; ok && entry.done == nil {
			delete(fileIndexCache, key)
		}
	}
}

// SearchSessionFileContents greps the session's indexed files for a literal
// substring. The index is taken from the same cache the quick-open uses, so a
// content search right after a filename search costs no extra walk.
func (a *App) SearchSessionFileContents(id, query string, caseSensitive, includeAll bool) (*session.ContentSearchResult, error) {
	index, err := a.SearchSessionFileIndex(id, includeAll)
	if err != nil {
		return nil, err
	}
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return nil, err
	}
	return inst.SearchFileContents(query, caseSensitive, index.Files)
}
