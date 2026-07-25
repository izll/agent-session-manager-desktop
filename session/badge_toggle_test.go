package session

import (
	"encoding/json"
	"testing"
)

// YOLO stays visible unless hidden (it flags bypassed permissions), while the
// resume marker is opt-in. Settings written before either flag existed must
// therefore show YOLO and hide resume.
func TestBadgeFlagDefaults(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"compact_list":true}`), &s); err != nil {
		t.Fatalf("unmarshal legacy settings: %v", err)
	}
	if s.HideYoloBadge {
		t.Error("legacy settings hid the YOLO badge; it should stay visible")
	}
	if s.ShowResumeBadge {
		t.Error("legacy settings showed the resume badge; it should be opt-in")
	}
}

func TestBadgeFlagsRoundTrip(t *testing.T) {
	data, err := json.Marshal(Settings{HideYoloBadge: true, ShowResumeBadge: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !loaded.HideYoloBadge || !loaded.ShowResumeBadge {
		t.Fatalf("flags lost: hideYolo=%v showResume=%v",
			loaded.HideYoloBadge, loaded.ShowResumeBadge)
	}
}
