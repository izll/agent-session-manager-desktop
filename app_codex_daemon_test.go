package main

import (
	"encoding/json"
	"testing"

	"asmgr-desktop/session"
)

// The notice reaches the frontend flat, with the window and the project: its
// "Restart tab" needs both to restart the right tab and no other.
func TestTheDaemonNoticeCarriesTheWindowAndProject(t *testing.T) {
	payload := (&App{}).codexDaemonHeldPayload(session.CodexDaemonHeldNotice{
		SessionID: "s1", SessionName: "Tickwell", ConversationID: "c1", WindowIndex: 3,
	})
	payload.ProjectID = "p1"
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"sessionId": "s1", "sessionName": "Tickwell", "serverId": "",
		"conversationId": "c1", "windowIdx": float64(3), "projectId": "p1",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v (payload %s)", key, got[key], value, data)
		}
	}
}
