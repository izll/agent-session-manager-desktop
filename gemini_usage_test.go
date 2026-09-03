package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The count is an approximation, and its parts have to be right: today's
// entries, from logs recent enough to hold any, matched against the tier's
// allowance. Each of those has a way to go wrong quietly.

func geminiDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_DIR", dir)
	geminiCacheMu.Lock()
	geminiCache = nil
	geminiCacheMu.Unlock()
	return dir
}

func writeLog(t *testing.T, dir, session, body string, modTime time.Time) {
	t.Helper()
	logDir := filepath.Join(dir, "tmp", session)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDir, "logs.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestCountsTodaysEntries(t *testing.T) {
	dir := geminiDir(t)
	today := time.Now().UTC().Format("2006-01-02")
	writeLog(t, dir, "a", `[{"timestamp": "`+today+`T08:00:00Z"},{"timestamp": "`+today+`T09:00:00Z"}]`, time.Now())

	info := fetchGeminiUsage()
	if info.RequestsToday != 2 {
		t.Fatalf("counted %d, want 2", info.RequestsToday)
	}
}

// Yesterday's prompts are not today's usage, and the log they are in was very
// likely touched recently.
func TestIgnoresOtherDays(t *testing.T) {
	dir := geminiDir(t)
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	writeLog(t, dir, "a",
		`[{"timestamp": "`+yesterday+`T23:00:00Z"},{"timestamp": "`+today+`T00:30:00Z"}]`, time.Now())

	if got := fetchGeminiUsage().RequestsToday; got != 1 {
		t.Fatalf("counted %d, want 1 — yesterday's entry was included", got)
	}
}

// Both spacings appear in the CLI's own logs depending on how they were written.
func TestCountsBothJSONSpacings(t *testing.T) {
	dir := geminiDir(t)
	today := time.Now().UTC().Format("2006-01-02")
	writeLog(t, dir, "a", `[{"timestamp":"`+today+`T08:00:00Z"},{"timestamp": "`+today+`T09:00:00Z"}]`, time.Now())

	if got := fetchGeminiUsage().RequestsToday; got != 2 {
		t.Fatalf("counted %d, want 2 — one of the two spacings was missed", got)
	}
}

// A log untouched for days cannot hold today's entries, and there is one per
// session directory — of which there are many.
func TestSkipsStaleLogs(t *testing.T) {
	dir := geminiDir(t)
	today := time.Now().UTC().Format("2006-01-02")
	writeLog(t, dir, "old", `[{"timestamp": "`+today+`T08:00:00Z"}]`, time.Now().AddDate(0, 0, -5))

	if got := fetchGeminiUsage().RequestsToday; got != 0 {
		t.Fatalf("counted %d from a five-day-old log, want 0", got)
	}
}

func TestTierDecidesTheAllowance(t *testing.T) {
	for _, tc := range []struct {
		selected string
		label    string
		limit    int
	}{
		{"oauth-workspace-standard", "Standard", 1500},
		{"oauth-workspace-enterprise", "Enterprise", 2000},
		{"gemini-api-key", "API Key", 250},
		{"oauth-personal", geminiDefaultTier, geminiDefaultLimit},
		{"", geminiDefaultTier, geminiDefaultLimit},
	} {
		t.Run(tc.selected, func(t *testing.T) {
			dir := geminiDir(t)
			body := `{"selectedType":"` + tc.selected + `"}`
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			info := fetchGeminiUsage()
			if info.TierLabel != tc.label || info.DailyLimit != tc.limit {
				t.Fatalf("tier = %q/%d, want %q/%d", info.TierLabel, info.DailyLimit, tc.label, tc.limit)
			}
		})
	}
}

// Newer versions moved the key under security.auth; both spellings are live.
func TestTierIsFoundUnderSecurityAuth(t *testing.T) {
	dir := geminiDir(t)
	body := `{"security":{"auth":{"selectedType":"gemini-api-key"}}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fetchGeminiUsage().TierLabel; got != "API Key" {
		t.Fatalf("tier = %q, want the one nested under security.auth", got)
	}
}

// No Gemini installed is not an error to shout about; the ring simply cannot
// be drawn.
func TestUnconfiguredGeminiIsUnavailable(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
	geminiCacheMu.Lock()
	geminiCache = nil
	geminiCacheMu.Unlock()
	if info := fetchGeminiUsage(); info.Available {
		t.Fatal("an absent ~/.gemini reported usage")
	}
}
