package filters

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RemoteFiltersURL is the canonical location of the curated filter bundle.
// It is hard-coded on purpose — accepting a URL from user config or env
// would let a malicious actor redirect the loader to their own server.
// In `-tags devremote` builds it can be set to a local test server (see
// remote_devremote.go).
//
// TODO: replace with the real GitHub raw URL once the repo exists, e.g.
//   https://raw.githubusercontent.com/<owner>/<repo>/main/filters/v1.json
var RemoteFiltersURL = ""

// RemoteFiltersHostAllowlist limits which hosts the loader will accept,
// even if RemoteFiltersURL is changed by a future release. Prevents an
// unintentional typo from pointing the app at an arbitrary host.
// `-tags devremote` builds add `127.0.0.1` and `localhost` for testing.
var RemoteFiltersHostAllowlist = []string{
	"raw.githubusercontent.com",
	"objects.githubusercontent.com", // GitHub redirects raw.* there sometimes
}

// remoteTLSInsecure is true only in `-tags devremote` builds, to allow the
// self-signed certificate of the local test server.
var remoteTLSInsecure = false

// SchemaVersion is the contract between this client and the published JSON.
// The remote bundle MUST have a "schema_version" field that matches; any
// other value is rejected so a future incompatible change can't break older
// clients silently.
const SchemaVersion = 1

// remoteBundle is what we expect the JSON to look like:
//
//   {
//     "schema_version": 1,
//     "filters": {
//       "claude": { "skip_contains": [...], ... },
//       "codex":  { "skip_prefixes": [...], ... }
//     }
//   }
type remoteBundle struct {
	SchemaVersion int          `json:"schema_version"`
	Filters       AgentFilters `json:"filters"`
}

const (
	remoteFetchTimeout = 8 * time.Second
	remoteMaxBytes     = 64 * 1024
	remoteMinInterval  = 30 * time.Minute
)

var (
	remoteFetchMu   sync.Mutex
	lastRemoteFetch time.Time
)

// RefreshRemoteFilters downloads, validates, and atomically caches the
// remote filter bundle. Safe to call repeatedly; rate-limited to one fetch
// per remoteMinInterval. Errors are logged and swallowed — a missing or
// broken remote should never break the app, since the cache + defaults
// still serve the previous good config.
func RefreshRemoteFilters(ctx context.Context) {
	if RemoteFiltersURL == "" {
		return // not configured yet
	}

	remoteFetchMu.Lock()
	defer remoteFetchMu.Unlock()
	if time.Since(lastRemoteFetch) < remoteMinInterval {
		return
	}

	bundle, err := fetchRemoteFilters(ctx, RemoteFiltersURL)
	if err != nil {
		log.Printf("[filters] remote refresh failed: %v", err)
		return
	}

	if err := writeRemoteCache(bundle.Filters); err != nil {
		log.Printf("[filters] remote cache write failed: %v", err)
		return
	}

	lastRemoteFetch = time.Now()
	ResetCache() // next LoadFilters call sees the new bundle
	log.Printf("[filters] remote refresh ok: %d agent entries", len(bundle.Filters))
}

// StartRemoteUpdater kicks off an immediate refresh in the background and
// then keeps refreshing every remoteMinInterval until ctx is done. Run as
// a goroutine from app startup.
func StartRemoteUpdater(ctx context.Context) {
	if RemoteFiltersURL == "" {
		return
	}
	go func() {
		// Initial fetch shortly after startup so we don't compete with
		// other startup work for bandwidth.
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
		RefreshRemoteFilters(ctx)

		ticker := time.NewTicker(remoteMinInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				RefreshRemoteFilters(ctx)
			}
		}
	}()
}

func fetchRemoteFilters(ctx context.Context, raw string) (*remoteBundle, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("only https is allowed")
	}
	if !hostAllowed(u.Hostname()) {
		return nil, fmt.Errorf("host %q is not in the allowlist", u.Hostname())
	}

	client := &http.Client{
		Timeout: remoteFetchTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: remoteTLSInsecure, // dev override only
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "asmgr-desktop")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	// Cap the read to remoteMaxBytes so a malicious server can't OOM us.
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > remoteMaxBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", remoteMaxBytes)
	}

	var b remoteBundle
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields() // any extra field == reject
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("malformed JSON: %w", err)
	}
	if b.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("schema version %d not supported", b.SchemaVersion)
	}
	if len(b.Filters) == 0 {
		return nil, errors.New("empty filter set")
	}

	// Per-filter sanity caps so a single huge entry can't grow memory.
	for agent, f := range b.Filters {
		if f == nil {
			return nil, fmt.Errorf("filter %q is null", agent)
		}
		total := len(f.SkipContains) + len(f.SkipPrefixes) + len(f.SkipSuffixes) +
			len(f.SkipExact) + len(f.ShowContains) + len(f.ShowAs)
		if total > 500 {
			return nil, fmt.Errorf("filter %q exceeds pattern budget", agent)
		}
	}
	return &b, nil
}

func hostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, allowed := range RemoteFiltersHostAllowlist {
		if host == allowed {
			return true
		}
	}
	return false
}

func writeRemoteCache(f AgentFilters) error {
	path := GetRemoteCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
