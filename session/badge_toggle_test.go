package session

import (
	"encoding/json"
	"testing"
)

// Both badges are visible by default, so the flags must be "hide": settings
// written before they existed have to keep showing the badges.
func TestBadgeFlagsDefaultToVisible(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"compact_list":true}`), &s); err != nil {
		t.Fatalf("unmarshal legacy settings: %v", err)
	}
	if s.HideYoloBadge || s.HideResumeBadge {
		t.Fatalf("legacy settings hid a badge: yolo=%v resume=%v",
			s.HideYoloBadge, s.HideResumeBadge)
	}
}

func TestBadgeFlagsRoundTrip(t *testing.T) {
	data, err := json.Marshal(Settings{HideYoloBadge: true, HideResumeBadge: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !loaded.HideYoloBadge || !loaded.HideResumeBadge {
		t.Fatalf("flags lost: yolo=%v resume=%v", loaded.HideYoloBadge, loaded.HideResumeBadge)
	}
}
