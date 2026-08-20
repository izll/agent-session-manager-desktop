package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"asmgr-desktop/session"

	"github.com/gorilla/websocket"
)

func TestTerminalRejectsMissingTmuxSessionBeforeWebSocketUpgrade(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{ID: "definitely-not-a-live-tmux-session", Name: "missing", Path: home}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}

	ts := &TerminalServer{storage: storage, authToken: "test-token"}
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/?" + url.Values{
		"session": {inst.ID},
		"token":   {ts.authToken},
	}.Encode()

	conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("missing tmux session was upgraded to a WebSocket")
	}
	if err == nil || response == nil || response.StatusCode != 404 {
		t.Fatalf("dial error/status = %v/%v, want HTTP 404 before upgrade", err, response)
	}
}
