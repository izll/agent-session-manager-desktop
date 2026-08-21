package main

import (
	"testing"

	"asmgr-desktop/session"
)

func TestResumeSessionDisplayNameHandlesShortAndEmptyIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{name: "empty", id: "", want: ""},
		{name: "short", id: "abc", want: "abc"},
		{name: "long", id: "123456789", want: "12345678..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resumeSessionDisplayName(session.AgentSession{SessionID: tc.id})
			if got != tc.want {
				t.Fatalf("display name = %q, want %q", got, tc.want)
			}
		})
	}
}
