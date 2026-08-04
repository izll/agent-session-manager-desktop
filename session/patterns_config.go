package session

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Activity-detection patterns, loaded from JSON rather than compiled in.
//
// An agent's wording is not ours to control: Claude, Codex and the rest reword
// their prompts whenever they like, and a changed phrase meant the app stopped
// noticing that agent waiting for an answer — fixable only by shipping a new
// release, which is the wrong shape of fix for a string change.
//
// Three sources, in order of preference:
//
//   - the file in the user's config directory, refreshed from the repository
//   - the copy embedded in the binary, which always works and needs no network
//   - the compiled defaults, if the embedded copy somehow will not parse
//
// The embedded copy is what makes this safe: a download that fails, returns
// nonsense, or is blocked by a firewall leaves the app exactly as it was.

//go:embed patterns.json
var embeddedPatterns []byte

// patternsFile is the JSON shape. Field names match the file.
type patternsFile struct {
	Version            int                       `json:"version"`
	DefaultSpinners    []string                  `json:"defaultSpinners"`
	ThinkingIndicators []string                  `json:"thinkingIndicators"`
	Agents             map[string]agentPatterned `json:"agents"`
}

type agentPatterned struct {
	Waiting       []string `json:"waiting"`
	Busy          []string `json:"busy"`
	ExtraSpinners []string `json:"extraSpinners"`
}

var (
	patternsOnce   sync.Once
	patternsMu     sync.RWMutex
	loadedPatterns *patternsFile
)

// PatternsURL is where an updated pattern file is fetched from. A raw file on
// the default branch, so publishing a fix is one commit.
const PatternsURL = "https://raw.githubusercontent.com/izll/agent-session-manager-desktop/master/session/patterns.json"

// patternsRefreshInterval bounds how often the file is fetched. Wording changes
// arrive with agent releases, not by the minute.
const patternsRefreshInterval = 24 * time.Hour

// patternsPath returns where the downloaded copy lives.
func patternsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "agent-session-manager-desktop")
	return filepath.Join(dir, "patterns.json"), nil
}

// currentPatterns returns the patterns in force, loading them once.
func currentPatterns() *patternsFile {
	patternsOnce.Do(func() {
		p := parsePatterns(embeddedPatterns)
		if p == nil {
			// The embedded file is part of the build, so this means the build
			// is broken rather than the user's install. Carrying on with the
			// compiled defaults keeps detection working either way.
			return
		}
		// A downloaded copy only replaces it when it is newer, so a stale file
		// left behind by an older release cannot undo a fix shipped since.
		if path, err := patternsPath(); err == nil {
			if data, readErr := os.ReadFile(path); readErr == nil {
				if downloaded := parsePatterns(data); downloaded != nil &&
					downloaded.Version > p.Version {
					p = downloaded
				}
			}
		}
		patternsMu.Lock()
		loadedPatterns = p
		patternsMu.Unlock()
	})
	patternsMu.RLock()
	defer patternsMu.RUnlock()
	return loadedPatterns
}

// parsePatterns reads a pattern file, returning nil if it is unusable.
//
// Rejects a file with no agents rather than accepting it: an empty file would
// silently switch off every waiting notification, which looks like agents that
// never ask for anything.
func parsePatterns(data []byte) *patternsFile {
	var p patternsFile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	if len(p.Agents) == 0 {
		return nil
	}
	return &p
}

// patternsFor returns the detection patterns for one agent type.
func patternsFor(agent AgentType) (AgentPatterns, bool) {
	p := currentPatterns()
	if p == nil {
		return AgentPatterns{}, false
	}
	entry, ok := p.Agents[string(agent)]
	if !ok {
		return AgentPatterns{}, false
	}

	spinners := p.DefaultSpinners
	if len(entry.ExtraSpinners) > 0 {
		// Copied rather than appended in place: append on a shared slice with
		// spare capacity would write into the defaults every other agent reads.
		spinners = make([]string, 0, len(p.DefaultSpinners)+len(entry.ExtraSpinners))
		spinners = append(spinners, p.DefaultSpinners...)
		spinners = append(spinners, entry.ExtraSpinners...)
	}
	return AgentPatterns{
		WaitingPatterns: entry.Waiting,
		BusyPatterns:    entry.Busy,
		Spinners:        spinners,
	}, true
}

// thinkingIndicatorsFromFile returns the configured thinking markers, or nil to
// use the compiled ones.
func thinkingIndicatorsFromFile() []string {
	if p := currentPatterns(); p != nil && len(p.ThinkingIndicators) > 0 {
		return p.ThinkingIndicators
	}
	return nil
}

// RefreshPatterns fetches the pattern file and stores it if it is newer.
//
// Called in the background at startup. Every failure is silent and harmless:
// the app keeps the patterns it already has, which are at worst the ones it
// shipped with.
func RefreshPatterns() {
	path, err := patternsPath()
	if err != nil {
		return
	}

	// Skip if the copy on disk was fetched recently, so starting the app
	// repeatedly does not mean fetching repeatedly.
	if info, statErr := os.Stat(path); statErr == nil {
		if time.Since(info.ModTime()) < patternsRefreshInterval {
			return
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(PatternsURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	// Bounded read: this is a small JSON file, and an unbounded read from the
	// network is how a wrong URL turns into memory exhaustion.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return
	}

	fetched := parsePatterns(data)
	if fetched == nil {
		return
	}
	// Never go backwards. A file served from a stale cache, or a branch that
	// has been reverted, would otherwise undo a fix already in place.
	current := currentPatterns()
	if current != nil && fetched.Version <= current.Version {
		// Still touch the file so the interval applies; otherwise every start
		// re-fetches a file that is not newer.
		_ = os.Chtimes(path, time.Now(), time.Now())
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	// Written via a temporary file: a partial write here would leave patterns
	// that fail to parse on the next start.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return
	}

	patternsMu.Lock()
	loadedPatterns = fetched
	patternsMu.Unlock()
}

// ForceRefreshPatterns fetches the pattern file regardless of when it was last
// checked, and reports what happened.
//
// The background refresh is deliberately quiet and rate-limited, which is right
// for something that runs on its own but useless when someone has just been
// told a fix is published and wants it now. Returns the version in force after
// the attempt, and whether it changed.
func ForceRefreshPatterns() (version int, updated bool, err error) {
	before := PatternsVersion()

	// Clear the interval guard, which is keyed on the file's timestamp.
	if path, pathErr := patternsPath(); pathErr == nil {
		_ = os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(PatternsURL)
	if err != nil {
		return before, false, fmt.Errorf("error.patternsUnreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return before, false, fmt.Errorf("error.patternsUnreachable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return before, false, fmt.Errorf("error.patternsUnreachable")
	}
	fetched := parsePatterns(data)
	if fetched == nil {
		return before, false, fmt.Errorf("error.patternsInvalid")
	}
	if fetched.Version <= before {
		return before, false, nil // already current
	}

	path, err := patternsPath()
	if err != nil {
		return before, false, fmt.Errorf("error.patternsNotWritten")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return before, false, fmt.Errorf("error.patternsNotWritten")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return before, false, fmt.Errorf("error.patternsNotWritten")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return before, false, fmt.Errorf("error.patternsNotWritten")
	}

	patternsMu.Lock()
	loadedPatterns = fetched
	patternsMu.Unlock()
	return fetched.Version, true, nil
}

// PatternsVersion reports the version in force, for the About box and logs.
func PatternsVersion() int {
	if p := currentPatterns(); p != nil {
		return p.Version
	}
	return 0
}

// patternsSummary is used in tests and diagnostics.
func patternsSummary() string {
	p := currentPatterns()
	if p == nil {
		return "compiled defaults"
	}
	return fmt.Sprintf("version %d, %d agents", p.Version, len(p.Agents))
}
