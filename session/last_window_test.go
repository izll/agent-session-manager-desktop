package session

import (
	"encoding/json"
	"testing"
)

// The remembered tab has to survive a save/load round trip, otherwise
// reopening a session always lands on the main window.
func TestLastWindowIndexRoundTrips(t *testing.T) {
	inst := &Instance{LastWindowIndex: 4}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.LastWindowIndex != 4 {
		t.Fatalf("last window index = %d, want 4", loaded.LastWindowIndex)
	}
}

// Sessions stored by earlier versions have no last_window_index key at all;
// they must load as 0 (the main window) rather than failing.
func TestLastWindowIndexDefaultsForOlderSessions(t *testing.T) {
	var loaded Instance
	if err := json.Unmarshal([]byte(`{"name":"legacy"}`), &loaded); err != nil {
		t.Fatalf("unmarshal legacy session: %v", err)
	}
	if loaded.LastWindowIndex != 0 {
		t.Fatalf("last window index = %d, want 0", loaded.LastWindowIndex)
	}
}

// Index 0 is the common case, so it must not be dropped as an "empty" value
// in a way that changes meaning on reload.
func TestLastWindowIndexZeroStaysZero(t *testing.T) {
	data, err := json.Marshal(&Instance{LastWindowIndex: 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.LastWindowIndex != 0 {
		t.Fatalf("last window index = %d, want 0", loaded.LastWindowIndex)
	}
}
