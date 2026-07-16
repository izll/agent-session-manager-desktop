package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Codex (OpenAI/GPT) subscription usage. The Codex CLI writes a rate-limit
// snapshot into its session logs (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl)
// with every token_count event:
//
//	{"timestamp":"...","payload":{"rate_limits":{"primary":{"used_percent":..,
//	  "window_minutes":..,"resets_at":<unix>},"secondary":..,"plan_type":".."}}}
//
// We read the newest snapshot locally — no network, no token handling. The
// data is as fresh as the user's last Codex request, which matches how the
// CLI's own /status works.

type CodexUsageWindow struct {
	UsedPercent   float64 `json:"usedPercent"`
	WindowMinutes int     `json:"windowMinutes"`
	ResetsAt      int64   `json:"resetsAt"` // unix seconds, 0 if unknown
}

type CodexUsageInfo struct {
	Available  bool              `json:"available"`
	Primary    *CodexUsageWindow `json:"primary,omitempty"`
	Secondary  *CodexUsageWindow `json:"secondary,omitempty"`
	PlanType   string            `json:"planType,omitempty"`
	SnapshotAt string            `json:"snapshotAt,omitempty"` // RFC3339 of the log entry
	Error      string            `json:"error,omitempty"`
}

var (
	codexCacheMu sync.Mutex
	codexCache   *CodexUsageInfo
	codexCacheAt time.Time
)

// GetCodexUsage returns the newest Codex rate-limit snapshot, cached for a
// minute (the dashboard polls on the git cadence).
func (a *App) GetCodexUsage() *CodexUsageInfo {
	codexCacheMu.Lock()
	defer codexCacheMu.Unlock()

	if codexCache != nil && time.Since(codexCacheAt) < usageCacheTTL {
		return codexCache
	}

	info := fetchCodexUsage()
	if !info.Available && codexCache != nil && codexCache.Available {
		return codexCache
	}
	codexCache = info
	codexCacheAt = time.Now()
	return info
}

func fetchCodexUsage() *CodexUsageInfo {
	info := &CodexUsageInfo{}

	home, err := os.UserHomeDir()
	if err != nil {
		info.Error = err.Error()
		return info
	}
	files, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*.jsonl"))
	if len(files) == 0 {
		info.Error = "no Codex session logs"
		return info
	}

	// Newest-modified files first. Long-running Codex sessions keep appending
	// to an old-dated file, so mtime (not the filename date) is the signal.
	sort.Slice(files, func(i, j int) bool {
		fi, err1 := os.Stat(files[i])
		fj, err2 := os.Stat(files[j])
		if err1 != nil || err2 != nil {
			return files[i] > files[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})

	for _, f := range files[:min(len(files), 5)] {
		if snap := lastRateLimitSnapshot(f); snap != nil {
			return snap
		}
	}
	info.Error = "no rate_limits snapshot found"
	return info
}

// lastRateLimitSnapshot tail-reads a session log (they grow to tens of MB;
// rate_limits lines are frequent, so the last 256 KiB is plenty) and parses
// the last rate_limits entry.
func lastRateLimitSnapshot(path string) *CodexUsageInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	const tailSize = 256 * 1024
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	offset := st.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, st.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && len(buf) == 0 {
		return nil
	}

	lines := bytes.Split(buf, []byte{'\n'})
	// Skip the first (possibly partial) line when we started mid-file.
	start := 0
	if offset > 0 {
		start = 1
	}
	// The log uses snake_case; CodexUsageWindow serializes camelCase to the
	// frontend, so parse into a log-shaped struct and convert.
	type logWindow struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes int     `json:"window_minutes"`
		ResetsAt      int64   `json:"resets_at"`
	}
	toWindow := func(w *logWindow) *CodexUsageWindow {
		if w == nil {
			return nil
		}
		return &CodexUsageWindow{
			UsedPercent:   w.UsedPercent,
			WindowMinutes: w.WindowMinutes,
			ResetsAt:      w.ResetsAt,
		}
	}

	for i := len(lines) - 1; i >= start; i-- {
		if !bytes.Contains(lines[i], []byte(`"rate_limits"`)) {
			continue
		}
		var entry struct {
			Timestamp string `json:"timestamp"`
			Payload   struct {
				RateLimits struct {
					Primary   *logWindow `json:"primary"`
					Secondary *logWindow `json:"secondary"`
					PlanType  string     `json:"plan_type"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(lines[i], &entry); err != nil {
			continue
		}
		rl := entry.Payload.RateLimits
		if rl.Primary == nil && rl.Secondary == nil {
			continue
		}
		return &CodexUsageInfo{
			Available:  true,
			Primary:    toWindow(rl.Primary),
			Secondary:  toWindow(rl.Secondary),
			PlanType:   rl.PlanType,
			SnapshotAt: entry.Timestamp,
		}
	}
	return nil
}
