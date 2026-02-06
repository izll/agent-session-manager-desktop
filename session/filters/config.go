package filters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// FilterConfig defines filter rules for an agent
type FilterConfig struct {
	SkipContains   []string `json:"skip_contains"`    // Skip if line contains any of these
	SkipPrefixes   []string `json:"skip_prefixes"`    // Skip if line starts with any of these
	SkipSuffixes   []string `json:"skip_suffixes"`    // Skip if line ends with any of these
	SkipExact      []string `json:"skip_exact"`       // Skip if line equals any of these
	MinSeparators  int      `json:"min_separators"`   // Skip if line has more than N separator chars (─━)
	ContentPrefix  string   `json:"content_prefix"`   // Extract content after this prefix (e.g., "┃")
	MinContentLen  int      `json:"min_content_len"`  // Minimum content length to show
	ShowContains   []string `json:"show_contains"`    // Show special status if line contains (e.g., "Generating")
	ShowAs         []string `json:"show_as"`          // What to show for each ShowContains match
}

// AgentFilters holds all agent filter configurations
type AgentFilters map[string]*FilterConfig

var loadedFilters AgentFilters
var filtersLoaded bool

// GetFiltersPath returns the path to the filters config file
func GetFiltersPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "agent-session-manager", "filters.json")
}

// LoadFilters loads filter configurations from file
func LoadFilters() AgentFilters {
	if filtersLoaded {
		return loadedFilters
	}

	loadedFilters = getDefaultFilters()
	filtersLoaded = true

	data, err := os.ReadFile(GetFiltersPath())
	if err != nil {
		// No custom config, use defaults
		return loadedFilters
	}

	var customFilters AgentFilters
	if err := json.Unmarshal(data, &customFilters); err != nil {
		return loadedFilters
	}

	// Merge custom filters with defaults
	for agent, config := range customFilters {
		loadedFilters[agent] = config
	}

	return loadedFilters
}

// SaveDefaultFilters saves the default filters to config file
func SaveDefaultFilters() error {
	filters := getDefaultFilters()
	data, err := json.MarshalIndent(filters, "", "  ")
	if err != nil {
		return err
	}

	configDir := filepath.Dir(GetFiltersPath())
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(GetFiltersPath(), data, 0644)
}

// ApplyFilter applies filter config to a line
func ApplyFilter(config *FilterConfig, cleanLine string) (skip bool, content string) {
	if config == nil {
		return false, ""
	}

	// Check separator count
	if config.MinSeparators > 0 {
		sepCount := strings.Count(cleanLine, "─") + strings.Count(cleanLine, "━")
		if sepCount > config.MinSeparators {
			return true, ""
		}
	}

	// Check exact matches
	for _, exact := range config.SkipExact {
		if cleanLine == exact {
			return true, ""
		}
	}

	// Check prefixes
	for _, prefix := range config.SkipPrefixes {
		if strings.HasPrefix(cleanLine, prefix) {
			return true, ""
		}
	}

	// Check suffixes
	for _, suffix := range config.SkipSuffixes {
		if strings.HasSuffix(cleanLine, suffix) {
			return true, ""
		}
	}

	// Check contains
	for _, contains := range config.SkipContains {
		if strings.Contains(cleanLine, contains) {
			return true, ""
		}
	}

	// Check special status indicators
	for i, contains := range config.ShowContains {
		if strings.Contains(cleanLine, contains) {
			if i < len(config.ShowAs) {
				return false, config.ShowAs[i]
			}
			return false, contains
		}
	}

	// Extract content from prefix
	if config.ContentPrefix != "" && strings.HasPrefix(cleanLine, config.ContentPrefix) {
		extracted := strings.TrimSpace(strings.TrimPrefix(cleanLine, config.ContentPrefix))
		if len(extracted) >= config.MinContentLen {
			return false, extracted
		}
		return true, ""
	}

	return false, ""
}

func getDefaultFilters() AgentFilters {
	return AgentFilters{
		"claude": {
			SkipContains:  []string{"? for", "Context left", "accept edits"},
			SkipPrefixes:  []string{"╭", "╰", "> ", ">"},
			SkipExact:     []string{">"},
			MinSeparators: 20,
		},
		"gemini": {
			SkipContains:  []string{"Type your message", "no sandbox", "/model", "Auto (Gemini"},
			SkipPrefixes:  []string{"╭", "╰", "│", ">", "~/", "~"},
			MinSeparators: 20,
		},
		"aider": {
			SkipPrefixes:  []string{">", "aider>"},
			MinSeparators: 20,
		},
		"codex": {
			SkipContains:  []string{"context left", "? for"},
			SkipPrefixes:  []string{">", "codex>", "›", "╭", "╰", "│"},
			MinSeparators: 20,
		},
		"amazonq": {
			SkipContains:  []string{"Amazon Q"},
			SkipPrefixes:  []string{">"},
			MinSeparators: 20,
		},
		"opencode": {
			SkipContains:   []string{"ctrl+?", "Context:", "press enter to send", "press esc", "No diagnostics", "GPT-4o", "Cost:"},
			SkipPrefixes:   []string{"└", "├", "│", "Glob:", "List:", "Task:"},
			SkipExact:      []string{">", "›"},
			MinSeparators:  15,
			ContentPrefix:  "┃",
			MinContentLen:  15,
			ShowContains:   []string{"Generating"},
			ShowAs:         []string{"Generating..."},
		},
		"custom": {},
	}
}
