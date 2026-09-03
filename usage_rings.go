package main

import "asmgr-desktop/session"

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
	// One fetch serves both windows — the endpoint returns them together — so
	// showing both costs no more than showing one.
	if rings.ShowFiveHour || rings.ShowSevenDay {
		rings.Claude = a.GetClaudeUsage()
	}
	if settings.ShowCodexUsageRing {
		rings.Codex = a.GetCodexUsage()
	}
	if settings.ShowGeminiUsageRing {
		rings.Gemini = a.GetGeminiUsage()
	}
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
