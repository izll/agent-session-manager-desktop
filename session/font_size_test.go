package session

import (
	"encoding/json"
	"testing"
)

// A size of 0 means "not set" at every level, so settings written before the
// feature existed must keep the size they had rather than jumping to a new
// default.
func TestFontSizeDefaultsToUnset(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"compact_list":true}`), &s); err != nil {
		t.Fatalf("unmarshal legacy settings: %v", err)
	}
	if s.TerminalFontSize != 0 {
		t.Errorf("legacy settings got font size %d, want 0 (unset)", s.TerminalFontSize)
	}

	var inst Instance
	if err := json.Unmarshal([]byte(`{"name":"legacy"}`), &inst); err != nil {
		t.Fatalf("unmarshal legacy session: %v", err)
	}
	if inst.TerminalFontSize != 0 {
		t.Errorf("legacy session got font size %d, want 0", inst.TerminalFontSize)
	}
}

func TestFontSizeRoundTrips(t *testing.T) {
	inst := &Instance{
		TerminalFontSize: 18,
		FollowedWindows:  []FollowedWindow{{Index: 1, TerminalFontSize: 11}},
	}
	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.TerminalFontSize != 18 {
		t.Errorf("session font size = %d, want 18", loaded.TerminalFontSize)
	}
	if len(loaded.FollowedWindows) != 1 || loaded.FollowedWindows[0].TerminalFontSize != 11 {
		t.Errorf("tab font size lost: %+v", loaded.FollowedWindows)
	}
}
