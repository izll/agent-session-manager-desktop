package main

import (
	"sync"

	"asmgr-desktop/session"
)

// UsageRings is what the sidebar draws beside the search box.
//
// One call rather than two, because the sidebar wants both at once and each
// carries its own "off" — a ring that is switched off must cost nothing, and
// the Claude figure is a live request against the user's own rate limit.
type UsageRings struct {
	Claude *ClaudeUsageInfo `json:"claude,omitempty"`
	Codex  *CodexUsageInfo  `json:"codex,omitempty"`
	Gemini *GeminiUsageInfo `json:"gemini,omitempty"`
	// Which Claude rings to draw. Both can be on at once: they answer different
	// questions — whether the next hour is at risk, and whether the week is —
	// and someone watching one usually wants the other beside it.
	ShowFiveHour bool `json:"showFiveHour"`
	ShowSevenDay bool `json:"showSevenDay"`
}

// GetUsageRings returns only what the settings ask for.
//
// The gate is here rather than in the sidebar because turning a ring off has to
// stop the work, not just hide the result. Asking Anthropic how much of
// someone's limit is left, every few minutes, to render a circle they have
// switched off, spends their quota on nothing.
func (a *App) GetUsageRings() *UsageRings {
	rings := &UsageRings{}

	settings, err := a.currentSettings()
	if err != nil || settings == nil {
		return rings
	}
	rings.ShowFiveHour = settings.ShowClaudeFiveHourRing
	rings.ShowSevenDay = settings.ShowClaudeSevenDayRing

	// In parallel, because one of the three is not like the others: Claude is a
	// network round trip of a few hundred milliseconds, while Codex and Gemini
	// read local files in microseconds. Run in sequence, the two instant ones
	// waited behind the slow one for no reason, and switching a ring on left a
	// visible gap before anything appeared.
	var wg sync.WaitGroup
	if rings.ShowFiveHour || rings.ShowSevenDay {
		// One fetch serves both windows — the endpoint returns them together —
		// so showing both costs no more than showing one.
		wg.Add(1)
		go func() { defer wg.Done(); rings.Claude = a.GetClaudeUsage() }()
	}
	if settings.ShowCodexUsageRing {
		wg.Add(1)
		go func() { defer wg.Done(); rings.Codex = a.GetCodexUsage() }()
	}
	if settings.ShowGeminiUsageRing {
		wg.Add(1)
		go func() { defer wg.Done(); rings.Gemini = a.GetGeminiUsage() }()
	}
	wg.Wait()
	return rings
}

// currentSettings reads the stored settings, or nil when they cannot be read.
func (a *App) currentSettings() (*session.Settings, error) {
	if a.storage == nil {
		return nil, nil
	}
	_, _, settings, err := a.storage.LoadAllWithSettings()
	return settings, err
}
