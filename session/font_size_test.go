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

// The view bar override is tri-state so that "show this tab" can survive a
// global hide; 0 must keep meaning "follow the global setting".
func TestViewBarOverrideRoundTrips(t *testing.T) {
	var legacy Instance
	if err := json.Unmarshal([]byte(`{"name":"legacy"}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy session: %v", err)
	}
	if legacy.HideViewBar != 0 {
		t.Errorf("legacy session got %d, want 0 (inherit)", legacy.HideViewBar)
	}

	inst := &Instance{
		HideViewBar:     1, // hidden
		FollowedWindows: []FollowedWindow{{Index: 1, HideViewBar: 2}}, // shown
	}
	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.HideViewBar != 1 {
		t.Errorf("session state = %d, want 1", loaded.HideViewBar)
	}
	if len(loaded.FollowedWindows) != 1 || loaded.FollowedWindows[0].HideViewBar != 2 {
		t.Errorf("tab state lost: %+v", loaded.FollowedWindows)
	}
}

// The bottom bar has its own tri-state override, independent of the view
// bar's — hiding one must not hide the other.
func TestStatusBarOverrideIsIndependent(t *testing.T) {
	inst := &Instance{
		HideViewBar:   1, // view bar hidden
		HideStatusBar: 2, // status bar explicitly shown
		FollowedWindows: []FollowedWindow{
			{Index: 1, HideViewBar: 0, HideStatusBar: 1},
		},
	}
	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.HideViewBar != 1 || loaded.HideStatusBar != 2 {
		t.Errorf("session states crossed over: view=%d status=%d",
			loaded.HideViewBar, loaded.HideStatusBar)
	}
	fw := loaded.FollowedWindows[0]
	if fw.HideViewBar != 0 || fw.HideStatusBar != 1 {
		t.Errorf("tab states crossed over: view=%d status=%d",
			fw.HideViewBar, fw.HideStatusBar)
	}
}
