package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Claude subscription usage, replicating the approach of the user's KDE
// claude-usage-widget: instead of parsing local transcript JSONLs and
// pricing tokens, ask Anthropic's OAuth usage endpoint, which returns
// pre-computed utilization percentages against the subscription's rate-limit
// windows (5-hour and 7-day, plus per-model 7-day splits).

// ClaudeUsageWindow is one rate-limit window's state.
type ClaudeUsageWindow struct {
	Utilization float64 `json:"utilization"` // 0-100 (%)
	ResetsAt    string  `json:"resetsAt"`    // RFC3339, may be empty
}

// ClaudeUsageInfo is what the dashboard renders.
type ClaudeUsageInfo struct {
	Available      bool              `json:"available"`
	FiveHour       ClaudeUsageWindow `json:"fiveHour"`
	SevenDay       ClaudeUsageWindow `json:"sevenDay"`
	SevenDaySonnet ClaudeUsageWindow `json:"sevenDaySonnet"`
	SevenDayOpus   ClaudeUsageWindow `json:"sevenDayOpus"`
	FetchedAt      string            `json:"fetchedAt"`
	Error          string            `json:"error,omitempty"`
}

var (
	usageCacheMu sync.Mutex
	usageCache   *ClaudeUsageInfo
	usageCacheAt time.Time
)

const usageCacheTTL = 60 * time.Second

// GetClaudeUsage returns the Claude subscription utilization, cached for
// a minute so dashboard polling can't hammer the endpoint.
func (a *App) GetClaudeUsage() *ClaudeUsageInfo {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()

	if usageCache != nil && time.Since(usageCacheAt) < usageCacheTTL {
		return usageCache
	}

	info := fetchClaudeUsage()
	// Keep serving the previous good result over a transient failure.
	if !info.Available && usageCache != nil && usageCache.Available {
		return usageCache
	}
	usageCache = info
	usageCacheAt = time.Now()
	return info
}

func fetchClaudeUsage() *ClaudeUsageInfo {
	info := &ClaudeUsageInfo{FetchedAt: time.Now().Format(time.RFC3339)}

	token, err := readClaudeOAuthToken()
	if err != nil {
		info.Error = err.Error()
		return info
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "asmgr-desktop/"+Version)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		info.Error = fmt.Sprintf("usage endpoint returned %d", resp.StatusCode)
		return info
	}

	var payload struct {
		FiveHour struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day"`
		SevenDaySonnet struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day_sonnet"`
		SevenDayOpus struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day_opus"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		info.Error = err.Error()
		return info
	}

	info.Available = true
	info.FiveHour = ClaudeUsageWindow{payload.FiveHour.Utilization, payload.FiveHour.ResetsAt}
	info.SevenDay = ClaudeUsageWindow{payload.SevenDay.Utilization, payload.SevenDay.ResetsAt}
	info.SevenDaySonnet = ClaudeUsageWindow{payload.SevenDaySonnet.Utilization, payload.SevenDaySonnet.ResetsAt}
	info.SevenDayOpus = ClaudeUsageWindow{payload.SevenDayOpus.Utilization, payload.SevenDayOpus.ResetsAt}
	return info
}

// readClaudeOAuthToken pulls the OAuth access token Claude Code itself uses
// (~/.claude/.credentials.json). Read-only; never persisted anywhere else.
func readClaudeOAuthToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		return "", fmt.Errorf("no Claude credentials: %w", err)
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return "", err
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("no OAuth access token in credentials")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}
