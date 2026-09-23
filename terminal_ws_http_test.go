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
	"time"
	"unicode/utf8"

	"asmgr-desktop/session"

	"github.com/gorilla/websocket"
)

// newTestTerminalStorage gives a test its own empty configuration directory.
func newTestTerminalStorage(t *testing.T) (*session.Storage, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	return storage, home
}

func terminalEndpoint(server *httptest.Server, values url.Values) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/?" + values.Encode()
}

// expectRefusal dials endpoint and requires the attach to be turned down with
// a close frame naming why — the only form of refusal a browser's WebSocket
// passes on to the page. An HTTP error before the upgrade arrives there as a
// bare "connection failed", whatever the reason was.
func expectRefusal(t *testing.T, endpoint string, code int, reason string) {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial error/status = %v/%v, want an upgrade the page can read a reason from", err, response)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, data, err := conn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read = type %d %q / %v, want a close frame before anything else", messageType, data, err)
	}
	if closeErr.Code != code || closeErr.Text != reason {
		t.Fatalf("close = %d %q, want %d %q", closeErr.Code, closeErr.Text, code, reason)
	}
}

func TestTerminalRefusesMissingTmuxSessionWithAReason(t *testing.T) {
	storage, home := newTestTerminalStorage(t)
	inst := &session.Instance{ID: "definitely-not-a-live-tmux-session", Name: "missing", Path: home}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}

	ts := NewTerminalServer(storage, 0)
	ts.authToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()
	values := url.Values{
		"session": {inst.ID},
		"token":   {ts.authToken},
		"project": {""},
	}

	expectRefusal(t, terminalEndpoint(server, values), closeSessionNotRunning, "session-not-running")

	// Something that is not a WebSocket is still answered in HTTP.
	response, err := http.Get(server.URL + "/?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("plain HTTP status = %d, want 404", response.StatusCode)
	}
}

func TestTerminalRefusesUnknownSessionWithAReason(t *testing.T) {
	storage, _ := newTestTerminalStorage(t)
	ts := NewTerminalServer(storage, 0)
	ts.authToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()

	expectRefusal(t, terminalEndpoint(server, url.Values{
		"session": {"no-such-session"},
		"token":   {ts.authToken},
		"project": {""},
	}), closeSessionNotFound, "session-not-found")
}

// Another instance holding the project is not a fault the user can fix by
// retrying, and "connection failed" sent them looking for one.
func TestTerminalRefusesALockedProjectWithAReason(t *testing.T) {
	ts := NewTerminalServer(nil, 0)
	ts.authToken = "test-token"
	ts.beginAttach = func(string) (func(), bool) { return nil, false }
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()

	expectRefusal(t, terminalEndpoint(server, url.Values{
		"session": {"any"},
		"token":   {ts.authToken},
		"project": {"p1"},
	}), closeProjectLocked, "project-locked")
}

// A bad token must stay a plain HTTP refusal: a socket is exactly what the
// token keeps from a caller that is not the app.
func TestTerminalRejectsABadTokenBeforeWebSocketUpgrade(t *testing.T) {
	ts := NewTerminalServer(nil, 0)
	ts.authToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()

	conn, response, err := websocket.DefaultDialer.Dial(terminalEndpoint(server, url.Values{
		"session": {"any"},
		"token":   {"wrong"},
		"project": {""},
	}), nil)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("a bad token was upgraded to a WebSocket")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("dial error/status = %v/%v, want HTTP 403 before upgrade", err, response)
	}
}

// A tab on a server whose attach failed used to get the error written into
// the pane as text, then an unexplained close — which the frontend took for a
// dropped connection and retried. It has to arrive as a verdict, with its
// detail. Reached here with no app, so there is no connection pool to use.
func TestTerminalReportsAFailedRemoteAttachInTheClose(t *testing.T) {
	storage, home := newTestTerminalStorage(t)
	inst := &session.Instance{ID: "remote-attach-fails", Name: "remote", Path: home, ServerID: "srv1"}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	executor := &probeExecutor{}
	session.SetExecutor(inst.ID, executor)
	t.Cleanup(func() { session.ClearExecutor(inst.ID) })

	ts := NewTerminalServer(storage, 0)
	ts.authToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()

	expectRefusal(t, terminalEndpoint(server, url.Values{
		"session": {inst.ID},
		"token":   {ts.authToken},
		"project": {""},
	}), closeAttachFailed, truncateCloseReason("attach-failed: "+errNoRemoteSupport.Error()))
}

func TestCloseReasonsFitAFrameWithoutSplittingACharacter(t *testing.T) {
	got := truncateCloseReason(strings.Repeat("é", 100)) // two bytes each
	if len(got) > maxCloseReasonBytes {
		t.Fatalf("reason is %d bytes, a close frame holds %d", len(got), maxCloseReasonBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncation split a character: %q", got)
	}
	if short := "session-not-running"; truncateCloseReason(short) != short {
		t.Fatal("a short reason was altered")
	}
}

func TestTerminalRejectsMissingProjectIdentityBeforeWebSocketUpgrade(t *testing.T) {
	ts := NewTerminalServer(nil, 0)
	ts.authToken = "test-token"
	server := httptest.NewServer(http.HandlerFunc(ts.handleTerminal))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/?" + url.Values{
		"session": {"same-id-can-exist-in-another-project"},
		"token":   {ts.authToken},
	}.Encode()

	conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("missing project identity was upgraded to a WebSocket")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("dial error/status = %v/%v, want HTTP 400 before upgrade", err, response)
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
