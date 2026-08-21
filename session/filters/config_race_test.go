package filters

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadFiltersConcurrentResetPublishesCompleteImmutableCopies(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)

	const workers = 12
	const iterations = 200
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				filters := LoadFilters()
				if filters["claude"] == nil || filters["codex"] == nil || filters["custom"] == nil {
					t.Errorf("LoadFilters published an incomplete default set")
					return
				}
				// A caller may retain and even mutate its result while a remote
				// refresh resets the cache. Neither action may affect another
				// caller or the published cache.
				filters["claude"].SkipContains[0] = "caller mutation"
				filters["injected"] = &FilterConfig{}
				if iteration%7 == 0 {
					ResetCache()
				}
			}
		}()
	}
	wg.Wait()

	loaded := LoadFilters()
	if loaded["injected"] != nil {
		t.Fatal("caller map mutation escaped into the published filter cache")
	}
	if loaded["claude"].SkipContains[0] == "caller mutation" {
		t.Fatal("caller slice mutation escaped into the published filter cache")
	}
}

func TestReadFiltersFileRejectsOversizedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	if err := os.WriteFile(path, []byte(`{"agent":{"skip_contains":["`+strings.Repeat("x", 64*1024)+`"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFiltersFile(path); ok {
		t.Fatal("oversized local filter file was accepted")
	}
}
