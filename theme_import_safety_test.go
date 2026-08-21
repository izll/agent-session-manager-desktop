package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOnlineSchemeIndexCacheIsConcurrentAndDoesNotPublishMutableSlice(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, `[{"name":"Safe.colorscheme","type":"file"}]`)
	}))
	defer server.Close()

	onlineIndexMu.Lock()
	oldURL, oldClient := schemeAPIURL, schemeHTTPClient
	onlineIndexCache = nil
	onlineIndexAt = time.Time{}
	schemeAPIURL = server.URL
	schemeHTTPClient = func(time.Duration) *http.Client { return server.Client() }
	onlineIndexMu.Unlock()
	t.Cleanup(func() {
		onlineIndexMu.Lock()
		defer onlineIndexMu.Unlock()
		schemeAPIURL, schemeHTTPClient = oldURL, oldClient
		onlineIndexCache = nil
		onlineIndexAt = time.Time{}
	})

	app := &App{}
	results := make(chan []OnlineSchemeInfo, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := app.ListOnlineSchemes()
			if err != nil {
				t.Errorf("ListOnlineSchemes: %v", err)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if len(got) != 1 || got[0].File != "Safe.colorscheme" {
			t.Fatalf("unexpected index: %#v", got)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("index fetched %d times, want one serialized fill", requests.Load())
	}

	first, err := app.ListOnlineSchemes()
	if err != nil {
		t.Fatal(err)
	}
	first[0].File = "mutated"
	second, err := app.ListOnlineSchemes()
	if err != nil || second[0].File != "Safe.colorscheme" {
		t.Fatalf("caller mutated published cache: %#v, %v", second, err)
	}
}

func TestSchemeInputSizeAndFetchCountAreBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.colorscheme")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", schemeDocumentLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSchemeFile(path); err == nil {
		t.Fatal("oversized local scheme was accepted")
	}

	app := &App{}
	if _, err := app.FetchOnlineSchemes(make([]string, maxOnlineSchemeFetch+1)); err == nil {
		t.Fatal("unbounded online scheme batch was accepted")
	}
}
