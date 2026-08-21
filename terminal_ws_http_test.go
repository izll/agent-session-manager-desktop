package main

import (
	"bytes"
	"errors"
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

	ts := NewTerminalServer(storage, 0)
	ts.authToken = "test-token"
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

func TestTerminalWebsocketRejectsOversizedInputFrame(t *testing.T) {
	readErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			readErr <- err
			return
		}
		defer conn.Close()
		configureTerminalWebsocket(conn)
		_, _, err = conn.ReadMessage()
		readErr <- err
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.BinaryMessage, bytes.Repeat([]byte{'x'}, terminalWSReadLimit+1)); err != nil {
		t.Fatal(err)
	}
	if err := <-readErr; !errors.Is(err, websocket.ErrReadLimit) {
		t.Fatalf("oversized terminal frame read error = %v, want ErrReadLimit", err)
	}
}
