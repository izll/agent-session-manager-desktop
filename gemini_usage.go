package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Gemini usage, counted from the CLI's own logs rather than asked for.
//
// Unlike Claude (a live endpoint) and Codex (a rate-limit snapshot the agent
// writes down), Gemini reports no remaining quota anywhere local. Its own
// quota call needs an OAuth refresh and a project id the CLI keeps to itself.
//
// So this counts instead: one logged prompt in ~/.gemini/tmp/*/logs.json is one
// request, matched against the daily allowance for the configured tier. That is
// an approximation — a prompt is not always exactly one model request — and it
// is the same one the ai-usage-hub widget makes, so the two agree.

type GeminiUsageInfo struct {
	Available     bool    `json:"available"`
	UsedPercent   float64 `json:"usedPercent"`
	RequestsToday int     `json:"requestsToday"`
	DailyLimit    int     `json:"dailyLimit"`
	TierLabel     string  `json:"tierLabel,omitempty"`
	Account       string  `json:"account,omitempty"`
	// ResetsAt is the next local midnight, when the daily count starts over.
	ResetsAt string `json:"resetsAt,omitempty"` // RFC3339
	Error    string `json:"error,omitempty"`
}

var (
	geminiCacheMu sync.Mutex
	geminiCache   *GeminiUsageInfo
	geminiCacheAt time.Time
)

// Daily request allowances by auth type, as the CLI documents them. Compiled in
// rather than fetched: they change with Google's plans, not by the hour, and a
// wrong number here is a wrong denominator, not a broken app.
var geminiTierLimits = map[string]struct {
	label string
	limit int
}{
	"oauth-workspace-standard":   {"Standard", 1500},
	"oauth-workspace-enterprise": {"Enterprise", 2000},
	"gemini-api-key":             {"API Key", 250},
}

const geminiDefaultTier = "Free"
const geminiDefaultLimit = 1000

// GetGeminiUsage returns today's request count against the daily allowance,
// cached for a minute like the other two.
func (a *App) GetGeminiUsage() *GeminiUsageInfo {
	geminiCacheMu.Lock()
	defer geminiCacheMu.Unlock()

	if geminiCache != nil && time.Since(geminiCacheAt) < usageCacheTTL {
		return geminiCache
	}
	info := fetchGeminiUsage()
	geminiCache = info
	geminiCacheAt = time.Now()
	return info
}

func geminiConfigDir() string {
	if dir := os.Getenv("GEMINI_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini")
}

func fetchGeminiUsage() *GeminiUsageInfo {
	dir := geminiConfigDir()
	if dir == "" {
		return &GeminiUsageInfo{Error: "no home directory"}
	}
	if _, err := os.Stat(dir); err != nil {
		return &GeminiUsageInfo{Error: "gemini is not configured"}
	}

	tier, limit := geminiTier(dir)
	count := geminiRequestsToday(dir)

	percent := 0.0
	if limit > 0 {
		percent = float64(count) / float64(limit) * 100
	}
	return &GeminiUsageInfo{
		Available:     true,
		UsedPercent:   percent,
		RequestsToday: count,
		DailyLimit:    limit,
		TierLabel:     tier,
		Account:       geminiAccount(dir),
		ResetsAt:      nextLocalMidnight().Format(time.RFC3339),
	}
}

// geminiTier reads the configured auth type and maps it to an allowance.
func geminiTier(dir string) (string, int) {
	raw, err := readClaudeUsageFileAtMost(filepath.Join(dir, "settings.json"), claudeUsagePayloadLimit)
	if err != nil {
		return geminiDefaultTier, geminiDefaultLimit
	}
	var settings struct {
		SelectedType string `json:"selectedType"`
		Security     struct {
			Auth struct {
				SelectedType string `json:"selectedType"`
			} `json:"auth"`
		} `json:"security"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return geminiDefaultTier, geminiDefaultLimit
	}
	// The key moved into security.auth in newer versions; accept either.
	selected := settings.SelectedType
	if selected == "" {
		selected = settings.Security.Auth.SelectedType
	}
	if tier, ok := geminiTierLimits[selected]; ok {
		return tier.label, tier.limit
	}
	return geminiDefaultTier, geminiDefaultLimit
}

func geminiAccount(dir string) string {
	raw, err := readClaudeUsageFileAtMost(filepath.Join(dir, "google_accounts.json"), claudeUsagePayloadLimit)
	if err != nil {
		return ""
	}
	var accounts struct {
		Active string `json:"active"`
	}
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return ""
	}
	return accounts.Active
}

// geminiRequestsToday counts prompts logged today, in UTC — which is how the
// timestamps are written, and how the daily quota is reckoned.
func geminiRequestsToday(dir string) int {
	logs, err := filepath.Glob(filepath.Join(dir, "tmp", "*", "logs.json"))
	if err != nil {
		return 0
	}
	today := time.Now().UTC().Format("2006-01-02")
	cutoff := time.Now().Add(-48 * time.Hour)

	total := 0
	for _, path := range logs {
		// A log untouched for two days cannot hold anything from today, and
		// there is one of these per session directory — of which there are
		// many.
		if info, err := os.Stat(path); err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		raw, err := readClaudeUsageFileAtMost(path, geminiLogSizeLimit)
		if err != nil {
			continue
		}
		total += strings.Count(string(raw), `"timestamp": "`+today) +
			strings.Count(string(raw), `"timestamp":"`+today)
	}
	return total
}

// geminiLogSizeLimit bounds one log file's read. Session logs are transcripts,
// so they can be large; the count only needs the timestamps.
const geminiLogSizeLimit = 16 << 20

func nextLocalMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}
