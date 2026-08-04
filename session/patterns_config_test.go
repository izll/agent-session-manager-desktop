package session

import (
	"encoding/json"
	"testing"
)

// The embedded file is what the app falls back to when there is no download, so
// a broken one would leave detection running on the compiled defaults without
// anything saying so.
func TestEmbeddedPatternsParse(t *testing.T) {
	p := parsePatterns(embeddedPatterns)
	if p == nil {
		t.Fatal("the embedded patterns file does not parse; the app would fall back " +
			"to compiled defaults with no sign that it had")
	}
	if p.Version < 1 {
		t.Error("version is not set, so a downloaded file can never be recognised as newer")
	}
	if len(p.DefaultSpinners) == 0 {
		t.Error("no default spinners: the spinner is the primary busy signal, so every " +
			"agent would look idle while working")
	}

	// Every agent the app knows about needs an entry, or it silently falls back
	// to Claude's patterns — which is how Codex questions went undetected.
	for _, agent := range []AgentType{
		AgentClaude, AgentGemini, AgentAider, AgentCodex,
		AgentAmazonQ, AgentOpenCode, AgentCustom,
	} {
		entry, ok := p.Agents[string(agent)]
		if !ok {
			t.Errorf("no patterns for %q", agent)
			continue
		}
		if len(entry.Waiting) == 0 {
			t.Errorf("%q has no waiting patterns, so it would never be reported as "+
				"waiting for an answer", agent)
		}
	}
}

// A file that parses but says nothing must be rejected rather than accepted:
// empty patterns switch off every waiting notification, which looks like agents
// that never ask for anything.
func TestUnusablePatternsAreRejected(t *testing.T) {
	cases := map[string]string{
		"not json":       "{{{",
		"no agents":      `{"version": 9, "agents": {}}`,
		"agents missing": `{"version": 9}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if parsePatterns([]byte(body)) != nil {
				t.Error("accepted; a file like this would disable waiting detection")
			}
		})
	}
}

// Extra spinners must not be appended into the shared default slice: append on
// a slice with spare capacity writes through to it, and every other agent reads
// the same one.
func TestExtraSpinnersDoNotLeakIntoDefaults(t *testing.T) {
	p := currentPatterns()
	if p == nil {
		t.Skip("no patterns loaded")
	}
	defaultsBefore := append([]string(nil), p.DefaultSpinners...)

	gemini, ok := patternsFor(AgentGemini)
	if !ok {
		t.Fatal("no patterns for gemini")
	}
	claude, ok := patternsFor(AgentClaude)
	if !ok {
		t.Fatal("no patterns for claude")
	}

	if len(gemini.Spinners) <= len(defaultsBefore) {
		t.Error("gemini's extra spinners were not added")
	}
	if len(claude.Spinners) != len(defaultsBefore) {
		t.Errorf("claude has %d spinners against %d defaults — gemini's extras leaked "+
			"into the shared slice", len(claude.Spinners), len(defaultsBefore))
	}
}

// The file has to stay valid JSON with the comment keys in it, since those are
// what tell the next person what a pattern change costs.
func TestPatternsFileIsValidJSON(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(embeddedPatterns, &raw); err != nil {
		t.Fatalf("patterns.json is not valid JSON: %v", err)
	}
	if _, ok := raw["$comment"]; !ok {
		t.Error("the explanatory comment is gone; someone editing this file needs to " +
			"know a too-general phrase makes an agent look permanently stuck")
	}
}

// The file has to reproduce what was compiled in, or moving the patterns out
// silently changes detection for every agent.
func TestFilePatternsMatchCompiledDefaults(t *testing.T) {
	for agent, compiled := range agentPatterns {
		fromFile, ok := patternsFor(agent)
		if !ok {
			t.Errorf("%q is in the compiled map but not in patterns.json", agent)
			continue
		}
		if !sameStrings(fromFile.WaitingPatterns, compiled.WaitingPatterns) {
			t.Errorf("%q: waiting patterns differ\n  file:     %q\n  compiled: %q",
				agent, fromFile.WaitingPatterns, compiled.WaitingPatterns)
		}
		if !sameStrings(fromFile.Spinners, compiled.Spinners) {
			t.Errorf("%q: spinners differ\n  file:     %q\n  compiled: %q",
				agent, fromFile.Spinners, compiled.Spinners)
		}
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
