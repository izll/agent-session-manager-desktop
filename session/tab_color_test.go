package session

import (
	"encoding/json"
	"testing"
)

func TestSetTabColors(t *testing.T) {
	inst := &Instance{
		FollowedWindows: []FollowedWindow{{Index: 3, Name: "agent"}},
	}

	if err := inst.SetTabColors(0, "auto", "#1a2B3c"); err != nil {
		t.Fatalf("set main tab colors: %v", err)
	}
	if inst.TabTextColor != "auto" || inst.TabBackgroundColor != "#1a2B3c" {
		t.Fatalf("unexpected main tab colors: %q, %q", inst.TabTextColor, inst.TabBackgroundColor)
	}

	if err := inst.SetTabColors(3, "#fff", "#12345678"); err != nil {
		t.Fatalf("set followed tab colors: %v", err)
	}
	followed := inst.FollowedWindows[0]
	if followed.TextColor != "#fff" || followed.BackgroundColor != "#12345678" {
		t.Fatalf("unexpected followed tab colors: %q, %q", followed.TextColor, followed.BackgroundColor)
	}

	if err := inst.SetTabColors(3, "", ""); err != nil {
		t.Fatalf("clear followed tab colors: %v", err)
	}
	if inst.FollowedWindows[0].TextColor != "" || inst.FollowedWindows[0].BackgroundColor != "" {
		t.Fatal("clearing colors did not remove both overrides")
	}
}

func TestSetTabColorsRecognisesNonZeroMainWindowIndex(t *testing.T) {
	inst := &Instance{FollowedWindows: []FollowedWindow{{Index: 9, Name: "agent"}}}
	if err := inst.setTabColors(4, 4, "#fff", "#123456"); err != nil {
		t.Fatalf("set non-zero main tab colors: %v", err)
	}
	if inst.TabTextColor != "#fff" || inst.TabBackgroundColor != "#123456" {
		t.Fatalf("non-zero main tab was not recognized: %#v", inst)
	}
}

func TestSetTabColorsRejectsUnsafeValuesWithoutMutation(t *testing.T) {
	inst := &Instance{
		TabTextColor:       "#fff",
		TabBackgroundColor: "#000",
		FollowedWindows: []FollowedWindow{{
			Index:           2,
			TextColor:       "#111",
			BackgroundColor: "#eee",
		}},
	}

	tests := []struct {
		name       string
		windowIdx  int
		textColor  string
		background string
	}{
		{name: "css injection", windowIdx: 0, textColor: "red; display:none", background: "#fff"},
		{name: "non-hex name", windowIdx: 2, textColor: "red", background: "#fff"},
		{name: "auto background", windowIdx: 2, textColor: "#fff", background: "auto"},
		{name: "unknown window", windowIdx: 99, textColor: "#fff", background: "#000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := inst.SetTabColors(tt.windowIdx, tt.textColor, tt.background); err == nil {
				t.Fatal("expected error")
			}
			if inst.TabTextColor != "#fff" || inst.TabBackgroundColor != "#000" {
				t.Fatal("main tab colors changed after rejected update")
			}
			followed := inst.FollowedWindows[0]
			if followed.TextColor != "#111" || followed.BackgroundColor != "#eee" {
				t.Fatal("followed tab colors changed after rejected update")
			}
		})
	}
}

func TestGetFollowedWindowIncludesMainTabColors(t *testing.T) {
	inst := &Instance{TabTextColor: "#abc", TabBackgroundColor: "#1234"}
	main := inst.GetFollowedWindow(0)
	if main.TextColor != "#abc" || main.BackgroundColor != "#1234" {
		t.Fatalf("main tab colors missing: %#v", main)
	}
}

func TestTabColorsPersistInInstanceJSON(t *testing.T) {
	original := &Instance{
		TabTextColor:       "#abc",
		TabBackgroundColor: "#12345678",
		FollowedWindows: []FollowedWindow{{
			Index:           4,
			TextColor:       "auto",
			BackgroundColor: "#def",
		}},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	var restored Instance
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal instance: %v", err)
	}
	if restored.TabTextColor != original.TabTextColor || restored.TabBackgroundColor != original.TabBackgroundColor {
		t.Fatalf("main tab colors were not preserved: %#v", restored)
	}
	if len(restored.FollowedWindows) != 1 || restored.FollowedWindows[0].TextColor != "auto" || restored.FollowedWindows[0].BackgroundColor != "#def" {
		t.Fatalf("followed tab colors were not preserved: %#v", restored.FollowedWindows)
	}
}
