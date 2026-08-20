package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// clearFileIndexCache drops every entry so one test cannot see another's.
func clearFileIndexCache(t *testing.T) {
	t.Helper()
	fileIndexMu.Lock()
	fileIndexCache = map[fileIndexKey]*fileIndexCacheEntry{}
	fileIndexMu.Unlock()
}

func TestFileIndexCacheReusesTheWalk(t *testing.T) {
	clearFileIndexCache(t)
	key := fileIndexKey{sessionID: "s1"}

	var walks int32
	build := func() (*session.FileIndex, error) {
		atomic.AddInt32(&walks, 1)
		return &session.FileIndex{Files: []session.IndexedFile{{Path: "a.go", Name: "a.go"}}}, nil
	}

	for n := 0; n < 3; n++ {
		index, err := cachedFileIndex(key, build)
		if err != nil {
			t.Fatalf("cachedFileIndex: %v", err)
		}
		if len(index.Files) != 1 {
			t.Fatalf("index = %+v, want the one walked file", index.Files)
		}
	}
	if got := atomic.LoadInt32(&walks); got != 1 {
		t.Errorf("walked %d times, want the result reused after the first", got)
	}

	// Past the TTL the tree is walked again — an agent working in the directory
	// creates and deletes files constantly.
	fileIndexMu.Lock()
	fileIndexCache[key].expiresAt = time.Now().Add(-time.Second)
	fileIndexMu.Unlock()

	if _, err := cachedFileIndex(key, build); err != nil {
		t.Fatalf("cachedFileIndex after expiry: %v", err)
	}
	if got := atomic.LoadInt32(&walks); got != 2 {
		t.Errorf("walked %d times, want a second walk once the entry expired", got)
	}
}

// includeAll produces a genuinely different list, so it must not share a cache
// slot with the default walk.
func TestFileIndexCacheKeyedByIncludeAll(t *testing.T) {
	clearFileIndexCache(t)

	var walks int32
	build := func() (*session.FileIndex, error) {
		atomic.AddInt32(&walks, 1)
		return &session.FileIndex{}, nil
	}

	if _, err := cachedFileIndex(fileIndexKey{sessionID: "s1", includeAll: false}, build); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedFileIndex(fileIndexKey{sessionID: "s1", includeAll: true}, build); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&walks); got != 2 {
		t.Errorf("walked %d times, want one walk per includeAll setting", got)
	}
}

// A failed walk is not an answer: caching it would keep quick-open broken for
// the whole TTL after one transient error. This mirrors the git branch cache's
// rule about timed-out lookups.
func TestFileIndexCacheDoesNotCacheFailures(t *testing.T) {
	clearFileIndexCache(t)
	key := fileIndexKey{sessionID: "s1"}

	var walks int32
	failing := func() (*session.FileIndex, error) {
		atomic.AddInt32(&walks, 1)
		return nil, errors.New("permission denied")
	}

	for n := 0; n < 3; n++ {
		if _, err := cachedFileIndex(key, failing); err == nil {
			t.Fatal("a failing walk must surface its error")
		}
	}
	if got := atomic.LoadInt32(&walks); got != 3 {
		t.Errorf("walked %d times, want every call to retry after a failure", got)
	}

	fileIndexMu.Lock()
	_, cached := fileIndexCache[key]
	fileIndexMu.Unlock()
	if cached {
		t.Error("a failed walk must leave nothing in the cache")
	}
}

// Two callers arriving at once must share one walk, not start two. A large
// repository takes long enough that the picker opening twice in quick
// succession would otherwise double the work.
func TestFileIndexCacheCollapsesConcurrentWalks(t *testing.T) {
	clearFileIndexCache(t)
	key := fileIndexKey{sessionID: "s1"}

	var walks int32
	release := make(chan struct{})
	build := func() (*session.FileIndex, error) {
		atomic.AddInt32(&walks, 1)
		<-release
		return &session.FileIndex{Files: []session.IndexedFile{{Path: "a.go"}}}, nil
	}

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for n := 0; n < callers; n++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			index, err := cachedFileIndex(key, build)
			if err == nil && len(index.Files) != 1 {
				err = errors.New("shared walk returned the wrong index")
			}
			errs[slot] = err
		}(n)
	}

	// Let the first walk finish only once every caller has had a chance to
	// arrive, so the collapse is actually exercised.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	for slot, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", slot, err)
		}
	}
	if got := atomic.LoadInt32(&walks); got != 1 {
		t.Errorf("walked %d times, want the concurrent callers to share one walk", got)
	}
}

func TestInvalidateSessionFileIndexDropsBothWalks(t *testing.T) {
	clearFileIndexCache(t)

	var walks int32
	build := func() (*session.FileIndex, error) {
		atomic.AddInt32(&walks, 1)
		return &session.FileIndex{}, nil
	}
	for _, includeAll := range []bool{false, true} {
		if _, err := cachedFileIndex(fileIndexKey{sessionID: "s1", includeAll: includeAll}, build); err != nil {
			t.Fatal(err)
		}
	}
	// A second session's entry must survive — refreshing one browser must not
	// cost every other session its index.
	if _, err := cachedFileIndex(fileIndexKey{sessionID: "s2"}, build); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.InvalidateSessionFileIndex("s1")

	fileIndexMu.Lock()
	_, keptDefault := fileIndexCache[fileIndexKey{sessionID: "s1"}]
	_, keptAll := fileIndexCache[fileIndexKey{sessionID: "s1", includeAll: true}]
	_, other := fileIndexCache[fileIndexKey{sessionID: "s2"}]
	fileIndexMu.Unlock()

	if keptDefault || keptAll {
		t.Error("invalidation must drop both of the session's walks")
	}
	if !other {
		t.Error("invalidation must not touch another session's index")
	}
}

func TestSessionFileSearchRequiresCanonicalRootSnapshot(t *testing.T) {
	clearFileIndexCache(t)
	storage := guardedTestStorage(t)
	root := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("unique needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "other.txt"), []byte("wrong tree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{ID: "quick-open-root", Path: root, Status: session.StatusStopped}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app := &App{storage: storage}

	alias := filepath.Join(t.TempDir(), "root-alias")
	expectedRoot := root
	if err := os.Symlink(root, alias); err == nil {
		expectedRoot = alias
	}
	index, err := app.SearchSessionFileIndex(inst.ID, false, -1, expectedRoot)
	if err != nil {
		t.Fatalf("canonical root snapshot rejected: %v", err)
	}
	if len(index.Files) != 1 || index.Files[0].Path != "root.txt" {
		t.Fatalf("index searched wrong root: %+v", index.Files)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	fileIndexMu.Lock()
	_, cachedUnderCanonicalRoot := fileIndexCache[fileIndexKey{sessionID: inst.ID, root: resolvedRoot}]
	fileIndexMu.Unlock()
	if !cachedUnderCanonicalRoot {
		t.Fatalf("index was not cached under canonical root %q", resolvedRoot)
	}

	contents, err := app.SearchSessionFileContents(inst.ID, "needle", false, false, -1, resolvedRoot)
	if err != nil {
		t.Fatalf("content search through root snapshot: %v", err)
	}
	if len(contents.Matches) != 1 || contents.Matches[0].Path != "root.txt" {
		t.Fatalf("content search used wrong root: %+v", contents.Matches)
	}
	if _, err := app.SearchSessionFileIndex(inst.ID, false, -1, ""); err == nil {
		t.Fatal("index accepted a missing root snapshot")
	}
	if _, err := app.SearchSessionFileIndex(inst.ID, false, -1, other); err == nil {
		t.Fatal("index accepted a stale root snapshot")
	}
	if _, err := app.SearchSessionFileContents(inst.ID, "wrong", false, false, -1, other); err == nil {
		t.Fatal("content search accepted a stale root snapshot")
	}
}
