package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"asmgr-desktop/session"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32768,
	WriteBufferSize: 32768,
	CheckOrigin:     checkTerminalOrigin,
}

// checkTerminalOrigin only permits the Wails webview itself. The webview
// either sends no Origin header or one under the wails:// scheme / the
// wails.localhost asset host. A real browser tab on any site sends an
// http(s):// Origin with a real host — those are rejected so a visited
// web page cannot hijack the terminal socket (CSWSH). The per-launch
// token (checked in handleTerminal) is the primary defense; this is
// belt-and-braces and also blocks the no-token-needed browser probe.
func checkTerminalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // native webview / non-browser client
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "wails":
		return true
	case "http", "https":
		// Wails serves assets from wails.localhost; allow only that host.
		h := u.Hostname()
		return h == "wails.localhost" || h == "localhost" || h == "127.0.0.1"
	default:
		return false
	}
}

// No automatic Ctrl-L anywhere in this file. It is sent from exactly one place,
// the Refresh button in app.go, where the user has asked for it.
//
// Ctrl-L is INPUT. It only means "redraw" if the program reading the pane
// chooses to treat it that way, and the programs here often do not: an
// interactive list reads it as a keystroke — Codex's /resume picker wiped its
// screen on one — and Claude Code turned a run of them into a "/clear" typed
// into the composer.
//
// It was sent automatically after a resize, to stop a bottom-aligned TUI
// keeping a frame laid out for the old geometry. That is covered without any
// keystroke: resize-window signals the program, which lays itself out again,
// and a tab returning from the background replays the output it missed instead
// of asking for a repaint it cannot get right.

// mirrorWindowIndexes lists the window indexes present in a mirror session.
func terminalTmuxCommand(ctx context.Context, args ...string) (*exec.Cmd, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	return session.TmuxCommandContext(commandCtx, args...), cancel
}

func terminalTmuxRun(ctx context.Context, args ...string) error {
	cmd, cancel := terminalTmuxCommand(ctx, args...)
	defer cancel()
	return cmd.Run()
}

func terminalTmuxOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd, cancel := terminalTmuxCommand(ctx, args...)
	defer cancel()
	return cmd.Output()
}

func mirrorWindowIndexes(ctx context.Context, sessionName string) []int {
	out, err := terminalTmuxOutput(ctx, "list-windows", "-t", sessionName,
		"-F", "#{window_index}")
	if err != nil {
		return nil
	}
	var idx []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if n, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil {
			idx = append(idx, n)
		}
	}
	return idx
}

// linkedWindowIndex reports where a window from the base session ended up in
// the mirror.
//
// Matched on window_id, which a link preserves — the same window object appears
// in both sessions — because the INDEX is not preserved: tmux honours the one
// we asked for, psmux picks the next free one. Returns false when the id cannot
// be established, so the caller can leave the mirror alone rather than guess.
func linkedWindowIndex(ctx context.Context, mirror, base string, baseIdx int) (int, bool) {
	wantOut, err := terminalTmuxOutput(ctx, "display-message", "-p", "-t",
		fmt.Sprintf("%s:%d", base, baseIdx), "#{window_id}")
	if err != nil {
		return 0, false
	}
	want := strings.TrimSpace(string(wantOut))
	if want == "" {
		return 0, false
	}

	listOut, err := terminalTmuxOutput(ctx, "list-windows", "-t", mirror,
		"-F", "#{window_index} #{window_id}")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != want {
			continue
		}
		if n, convErr := strconv.Atoi(fields[0]); convErr == nil {
			return n, true
		}
	}
	return 0, false
}

// TerminalServer handles WebSocket connections for terminal I/O
type TerminalServer struct {
	storage      *session.Storage
	port         int
	authToken    string // per-launch secret; required on every /terminal connect
	mu           sync.RWMutex
	conns        map[string]*termConn
	typingSignal *int64 // pointer to App.lastTypingSignal for zero-overhead typing detection
	// beginAttach pins App's active project until this handler has resolved the
	// session and created its terminal attach. Returning only a bool would leave
	// a check/use window where SelectProject could switch Storage in between.
	beginAttach func() (release func(), allowed bool)
	server      *http.Server
	listener    net.Listener
	serveDone   chan struct{}
	stopping    bool
	connWG      sync.WaitGroup
	handlerWG   sync.WaitGroup
	lifecycle   context.Context
	cancel      context.CancelFunc
}

type termConn struct {
	ws *websocket.Conn
	// ptmx is the attached multiplexer's byte stream. It is a PTY master on
	// Unix and a pipe pair on Windows (see session.StartTerminal); everything
	// below only reads, writes and closes it, which is all both can do.
	ptmx      session.TerminalStream
	cmd       *exec.Cmd
	done      chan struct{}
	writeMu   sync.Mutex // websocket output/control frames
	inputMu   sync.Mutex // terminal input from websocket, dictation and backspace
	closeOnce sync.Once
	// hidden is true while this tab is in the background. We keep reading the PTY
	// (so tmux never blocks) but hold the output instead of sending WS frames.
	// On WebKitGTK every WS frame is dispatched on the single webview main
	// thread; a background agent flooding output would otherwise starve the
	// FOREGROUND tab's keystroke handling — the user-visible asymmetry where a
	// heavy background agent made typing in the visible tab unbearably laggy.
	// The agent keeps running; on un-hide the held stream is replayed.
	hidden bool

	// Output produced while this tab was hidden, replayed when it comes back.
	//
	// Held rather than dropped: dropping it left tmux believing the client was
	// current, so it sent only differences against a screen this client never
	// received — a half-repainted pane with leftovers, recoverable only by a
	// Ctrl-L, which is input and repeatedly landed in an agent's composer.
	// heldMu protects the visibility transition as well as held. Keeping the
	// decision and the buffer under one lock prevents output racing an un-hide:
	// a chunk must be either replayed before the tab becomes visible or sent as
	// live output afterwards, never stranded in held between those two steps.
	heldMu   sync.Mutex
	held     []byte
	heldOver bool
}

// A webview is local, so ten seconds is deliberately generous. It is still a
// real deadline: without one a client that stopped consuming frames could pin
// the output pump (and its visibility/write locks) forever.
const terminalWSWriteTimeout = 10 * time.Second

const (
	terminalHTTPReadHeaderTimeout = 5 * time.Second
	terminalHTTPIdleTimeout       = 30 * time.Second
	terminalHTTPMaxHeaderBytes    = 16 << 10
	// A terminal keystroke/control frame is normally bytes or kilobytes. Bound
	// the exceptional paste as well: gorilla otherwise allocates the complete
	// frame before the input queue can apply backpressure, and Windows can queue
	// many such frames while psmux drains them through send-keys.
	terminalWSReadLimit = 1 << 20
)

func configureTerminalWebsocket(ws *websocket.Conn) {
	ws.SetReadLimit(terminalWSReadLimit)
}

// closeTransport is the common, idempotent failure signal for both websocket
// pumps. Closing the socket wakes ReadMessage, whose defer owns map removal,
// process reaping and mirror cleanup; closing the stream wakes the PTY reader.
func (tc *termConn) closeTransport() {
	tc.closeOnce.Do(func() {
		close(tc.done)
		if tc.ptmx != nil {
			_ = tc.ptmx.Close()
		}
		if tc.ws != nil {
			_ = tc.ws.Close()
		}
	})
}

func (tc *termConn) writeBinary(data []byte) error {
	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()
	if err := tc.ws.SetWriteDeadline(time.Now().Add(terminalWSWriteTimeout)); err != nil {
		return err
	}
	err := tc.ws.WriteMessage(websocket.BinaryMessage, data)
	_ = tc.ws.SetWriteDeadline(time.Time{})
	return err
}

// writeInput serializes every producer of terminal input without coupling it
// to websocket output backpressure. Dictation and backspace used to take
// writeMu while ordinary websocket keystrokes did not: a slow browser could
// hold voice input behind a ten-second socket deadline, and concurrent PTY
// writes could interleave on streams whose implementation shells out to the
// multiplexer (notably psmux on Windows).
func (tc *termConn) writeInput(data []byte) (int, error) {
	tc.inputMu.Lock()
	defer tc.inputMu.Unlock()
	return tc.ptmx.Write(data)
}

func (tc *termConn) handleOutputWriteError(err error) bool {
	if err == nil {
		return false
	}
	tc.closeTransport()
	return true
}

// handleInputWriteError gives a failed terminal write the same lifecycle as a
// failed websocket write. In particular, controlModeStream.Write on Windows
// runs psmux send-keys separately from the websocket: that command can fail
// while the socket itself remains healthy. Leaving it open makes every later
// keystroke disappear into a connection that can no longer reach the pane;
// closing the transport lets the frontend reconnect or show the disconnect.
func (tc *termConn) handleInputWriteError(err error) bool {
	if err == nil {
		return false
	}
	tc.closeTransport()
	return true
}

// maxHeldWhileHidden bounds what one hidden tab may accumulate.
//
// Sized against what an agent actually emits, which is not plain text: a
// working Claude or Codex redraws its whole screen many times a second, and
// every redraw carries the ANSI control sequences to reposition and repaint
// it. At 4 MiB — the first estimate, reasoned from "a screen is about ten
// kilobytes" — a tab left running for a few minutes overflowed, and the user
// came back to a repainted screen with no scrollback and had to scroll up to
// find what had happened. Measured in the wild, not predicted.
//
// The figure has to survive being multiplied. This is a per-tab budget, and a
// real workspace here runs 55 tabs across 17 sessions — at 64 MiB each that is
// 3.4 GiB in the worst case, which is not a budget, it is a leak with a
// ceiling. 16 MiB keeps the same worst case under a gigabyte while still being
// four times what overflowed in about an hour of a working agent.
//
// Past the limit the oldest bytes fall off the top and the newest are kept, so
// exceeding it costs the far end of the scrollback rather than all of it.
const maxHeldWhileHidden = 16 * 1024 * 1024

// holdWhileHidden stores output for a hidden tab.
func (tc *termConn) holdWhileHidden(data []byte) {
	if len(data) == 0 {
		return
	}
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	tc.holdWhileHiddenLocked(data)
}

func (tc *termConn) holdWhileHiddenLocked(data []byte) {
	tc.held = append(tc.held, data...)
	if len(tc.held) <= maxHeldWhileHidden {
		return
	}

	// Over the limit: keep the most recent bytes and drop the oldest.
	//
	// The earlier behaviour dropped everything and repainted, on the reasoning
	// that replaying a prefix would show the start of what happened and then
	// jump — which reads as corruption. That reasoning holds for a prefix. It
	// does not hold for a suffix: a terminal stream is written in order, so
	// starting partway through is exactly what scrolling back through any
	// terminal shows. What was lost is off the top, which is where lost
	// history belongs.
	//
	// The flag is still set, so the repaint still happens: the tail may begin
	// mid-escape-sequence, and the repaint puts the visible screen right
	// regardless of where the replay started.
	// Copied into a right-sized buffer rather than resliced. Reslicing keeps
	// the original array alive — the window moves, the memory does not — and
	// append grows by doubling, so a tab that overflowed could sit on twice
	// the limit in memory while reporting the limit in length. With many tabs
	// hidden at once that difference is measured in gigabytes.
	tail := make([]byte, maxHeldWhileHidden)
	copy(tail, tc.held[len(tc.held)-maxHeldWhileHidden:])
	tc.held = tail
	tc.heldOver = true
}

// deliverOrHold atomically chooses the destination for terminal bytes. The
// callback runs while the visibility lock is held so hide/un-hide cannot cross
// a websocket write and reorder live output ahead of the replay buffer.
func (tc *termConn) deliverOrHold(data []byte, deliver func([]byte) error) (held bool, err error) {
	if len(data) == 0 {
		return false, nil
	}
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	if tc.hidden {
		tc.holdWhileHiddenLocked(data)
		return true, nil
	}
	return false, deliver(data)
}

func (tc *termConn) holdIfHidden(chunks ...[]byte) bool {
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	if !tc.hidden {
		return false
	}
	for _, chunk := range chunks {
		if len(chunk) > 0 {
			tc.holdWhileHiddenLocked(chunk)
		}
	}
	return true
}

func (tc *termConn) setHidden() {
	tc.heldMu.Lock()
	tc.hidden = true
	tc.heldMu.Unlock()
}

// reveal marks the connection visible and replays everything accumulated
// while hidden before a live writer can overtake it.
func (tc *termConn) reveal(deliver func([]byte) error) (wasHidden, overflowed bool, err error) {
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	wasHidden = tc.hidden
	if !wasHidden {
		return false, false, nil
	}
	tc.hidden = false
	data := tc.held
	overflowed = tc.heldOver
	tc.held, tc.heldOver = nil, false
	if len(data) > 0 {
		err = deliver(data)
	}
	return wasHidden, overflowed, err
}

// discardHeldWhileHidden frees the buffer without replaying it.
//
// Held output is only worth keeping for a tab that will come back to this
// connection. Once the connection is closing — the tab was detached, the
// session stopped, the window went away — nobody will ever ask for those
// bytes, and holding them until the connection is garbage collected keeps up
// to the full limit alive per dead tab.
func (tc *termConn) discardHeldWhileHidden() {
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	tc.held, tc.heldOver = nil, false
}

// takeHeldWhileHidden returns and clears what was held, and whether the limit
// was hit — in which case the caller has to fall back to a repaint.
func (tc *termConn) takeHeldWhileHidden() (data []byte, overflowed bool) {
	tc.heldMu.Lock()
	defer tc.heldMu.Unlock()
	data, overflowed = tc.held, tc.heldOver
	tc.held, tc.heldOver = nil, false
	return data, overflowed
}

// WriteToTerminal writes data directly to a PTY connection (for dictation)
func (ts *TerminalServer) WriteToTerminal(sessionID string, windowIdx int, data string) error {
	connID := fmt.Sprintf("%s-%d", sessionID, windowIdx)

	ts.mu.RLock()
	tc, exists := ts.conns[connID]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no terminal connection for %s", connID)
	}

	_, err := tc.writeInput([]byte(data))
	return err
}

// SendBackspace sends N backspace keys directly to a PTY connection
func (ts *TerminalServer) SendBackspace(sessionID string, windowIdx int, count int) error {
	if count <= 0 {
		return nil
	}
	connID := fmt.Sprintf("%s-%d", sessionID, windowIdx)

	ts.mu.RLock()
	tc, exists := ts.conns[connID]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no terminal connection for %s", connID)
	}

	// Build backspace sequence (0x7f = DEL, standard terminal backspace)
	bs := make([]byte, count)
	for i := range bs {
		bs[i] = 0x7f
	}

	_, err := tc.writeInput(bs)
	return err
}

// NewTerminalServer creates a new terminal WebSocket server with a fresh
// random auth token. The token lives only in memory for this process, so
// other local processes / visited web pages cannot guess or read it.
func NewTerminalServer(storage *session.Storage, port int) *TerminalServer {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a time-seeded value so we never
		// run with an empty (always-accept) token.
		b = []byte(fmt.Sprintf("fallback-%d", time.Now().UnixNano()))
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	return &TerminalServer{
		storage:   storage,
		port:      port,
		authToken: hex.EncodeToString(b),
		conns:     make(map[string]*termConn),
		lifecycle: lifecycle,
		cancel:    cancel,
	}
}

// AuthToken returns the per-launch terminal auth token. Exposed to the
// frontend via a Wails-bound App method so the WebSocket URL can carry it.
func (ts *TerminalServer) AuthToken() string {
	return ts.authToken
}

// Start starts the WebSocket server.
//
// It binds the listener synchronously so a port conflict is detected before
// the frontend tries to connect. If the preferred port is taken (e.g. another
// asmgr-desktop instance is already running), it walks upward to the next
// free port instead of silently failing — which previously left the terminal
// pane blank with no obvious cause. The actually-bound port is stored back
// in ts.port so GetPort() (exposed to the frontend) returns the right value.
func (ts *TerminalServer) Start() error {
	const maxAttempts = 20
	requested := ts.port
	var ln net.Listener
	var lastErr error

	for p := requested; p < requested+maxAttempts; p++ {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			lastErr = err
			log.Printf("Terminal server: port %d unavailable (%v), trying next", p, err)
			continue
		}
		ln = l
		ts.port = p
		break
	}

	if ln == nil {
		return fmt.Errorf("terminal server: no free port in range %d-%d: %w",
			requested, requested+maxAttempts-1, lastErr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/terminal", ts.handleTerminal)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: terminalHTTPReadHeaderTimeout,
		IdleTimeout:       terminalHTTPIdleTimeout,
		MaxHeaderBytes:    terminalHTTPMaxHeaderBytes,
	}
	serveDone := make(chan struct{})
	ts.mu.Lock()
	if ts.server != nil || ts.stopping {
		ts.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("terminal server is already started or stopping")
	}
	ts.server = server
	ts.listener = ln
	ts.serveDone = serveDone
	ts.mu.Unlock()

	if ts.port != requested {
		log.Printf("Terminal WebSocket server bound to fallback port %d (preferred %d was busy)", ts.port, requested)
	} else {
		log.Printf("Terminal WebSocket server starting on 127.0.0.1:%d", ts.port)
	}

	go func() {
		defer close(serveDone)
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("Terminal server error: %v", err)
		}
	}()

	return nil
}

// Stop closes the listener, every active WebSocket/terminal stream and all of
// their pumps. It is called before project-lock and mirror teardown: otherwise
// a connection accepted during shutdown can recreate or keep using resources
// after their owner has released them.
func (ts *TerminalServer) Stop(ctx context.Context) error {
	ts.mu.Lock()
	ts.stopping = true
	if ts.cancel != nil {
		ts.cancel()
	}
	server := ts.server
	listener := ts.listener
	serveDone := ts.serveDone
	conns := make([]*termConn, 0, len(ts.conns))
	for _, tc := range ts.conns {
		conns = append(conns, tc)
	}
	ts.mu.Unlock()

	for _, tc := range conns {
		tc.closeTransport()
	}

	var stopErr error
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			stopErr = err
			_ = server.Close()
		}
	} else if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			stopErr = err
		}
	}

	connectionsDone := make(chan struct{})
	go func() {
		ts.connWG.Wait()
		close(connectionsDone)
	}()
	select {
	case <-connectionsDone:
	case <-ctx.Done():
		stopErr = errors.Join(stopErr, ctx.Err())
	}
	handlersDone := make(chan struct{})
	go func() {
		ts.handlerWG.Wait()
		close(handlersDone)
	}()
	select {
	case <-handlersDone:
	case <-ctx.Done():
		stopErr = errors.Join(stopErr, ctx.Err())
	}
	if serveDone != nil {
		select {
		case <-serveDone:
		case <-ctx.Done():
			stopErr = errors.Join(stopErr, ctx.Err())
		}
	}
	return stopErr
}

func (ts *TerminalServer) beginHandler() (context.Context, func(), bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.stopping || ts.lifecycle == nil {
		return nil, nil, false
	}
	ts.handlerWG.Add(1)
	return ts.lifecycle, ts.handlerWG.Done, true
}

// handleTerminal handles WebSocket connections
func (ts *TerminalServer) handleTerminal(w http.ResponseWriter, r *http.Request) {
	handlerCtx, handlerDone, allowed := ts.beginHandler()
	if !allowed {
		http.Error(w, "terminal server is stopping", http.StatusServiceUnavailable)
		return
	}
	defer handlerDone()

	sessionID := r.URL.Query().Get("session")
	windowIdx := r.URL.Query().Get("window")

	// Require the per-launch token before doing anything else (and before
	// the WS upgrade). Constant-time compare to avoid a timing oracle.
	// This is the primary defense against CSWSH and other local processes:
	// neither a visited web page nor another process can read the in-memory
	// token, so they cannot forge a valid connection.
	token := r.URL.Query().Get("token")
	if ts.authToken == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(ts.authToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		log.Printf("[terminal] rejected connection: bad/missing token (origin=%q)", r.Header.Get("Origin"))
		return
	}

	// Refuse attaches when another instance owns the project. Attaching would
	// create/kill mirror tmux sessions that the owning instance is using,
	// ripping its ptys out ("read /dev/ptmx: input/output error").
	releaseAttach := func() {}
	attachReleased := false
	if ts.beginAttach != nil {
		release, allowed := ts.beginAttach()
		if !allowed {
			http.Error(w, "project locked by another instance", http.StatusConflict)
			log.Printf("[terminal] refused attach: project locked by another instance")
			return
		}
		if release != nil {
			releaseAttach = release
		}
	}
	defer func() {
		if !attachReleased {
			releaseAttach()
		}
	}()

	if sessionID == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}

	// Get session instance
	inst, err := ts.storage.GetInstance(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Parse window index
	winIdx := 0
	if windowIdx != "" {
		fmt.Sscanf(windowIdx, "%d", &winIdx)
	}

	// Start PTY with tmux attach.
	// We create a grouped session per-connection so each WebSocket has its own
	// active window, preventing window switches from affecting other connections.
	tmuxSession := inst.TmuxSessionName()

	// Nothing to attach to if the multiplexer session is gone — after the user
	// deleted it, say. Attaching anyway builds a mirror around a session that
	// does not exist, and the client exits immediately, which reaches the
	// browser as an unexplained socket close and the pane as whatever the
	// multiplexer printed on its way out.
	//
	// Retried briefly rather than checked once: the frontend attaches straight
	// after starting a session, and a multiplexer that has just forked its
	// server may not answer for it yet. A session that is genuinely gone fails
	// all three attempts in well under a second.
	running := false
	for attempt := 0; attempt < 3; attempt++ {
		if err := terminalTmuxRun(handlerCtx, "has-session", "-t", tmuxSession); err == nil {
			running = true
			break
		}
		delay := time.NewTimer(150 * time.Millisecond)
		select {
		case <-delay.C:
		case <-handlerCtx.Done():
			delay.Stop()
			return
		}
	}
	if !running {
		http.Error(w, "session not running", http.StatusNotFound)
		log.Printf("[ws] refused attach: %s is not running", tmuxSession)
		return
	}

	// Upgrade only after every check that can still return an HTTP error. Once
	// hijacked, http.Error cannot reach the client and an early return would
	// leave a successfully-opened WebSocket hanging without a close frame.
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	configureTerminalWebsocket(ws)
	connectionOwned := false
	defer func() {
		if !connectionOwned {
			_ = ws.Close()
		}
	}()

	linkedName := fmt.Sprintf("%s_gui_%d_%d", tmuxSession, winIdx, time.Now().UnixMilli())

	// Create an ISOLATED single-window mirror session.
	//
	// We used to create a GROUPED session (`new-session -t base`), which shares
	// ALL of the base session's windows. That has a fatal flaw for multi-tab
	// sessions: tmux pushes the BASE SESSION'S ACTIVE-WINDOW redraw to every
	// grouped client, regardless of which window that client is actually
	// viewing. So when one WebErp tab's agent worked in window 6, EVERY other
	// WebErp mirror — including the one showing an idle window 0 that the user
	// was typing in — received window 6's full redraw (~8 KB/s, measured via
	// the [send] diag). That continuous off-screen redraw drove the xterm DOM
	// renderer and made foreground typing stutter. asmgr-desktop (2 tabs) hurt
	// less than WebErp (5 tabs) purely because it had fewer mirrors sharing the
	// group — the exact asymmetry the user reported.
	//
	// Fix: instead of grouping, create an empty session and LINK only the one
	// target window into it. The linked window is the SAME tmux window object
	// (fully live/interactive — the agent's pane is unchanged), but this session
	// contains ONLY that window, so no other window's activity can ever reach
	// this mirror. Each tab is now truly isolated.
	attachTarget := linkedName
	if !session.MirrorSupported() {
		// Straight to the session itself. psmux accepts link-window and reports
		// success, but the window does not arrive: the mirror is left holding
		// only its own placeholder shell, which is what the terminal then
		// showed instead of the agent. Attaching directly works there, at the
		// cost of the per-tab isolation the mirror buys elsewhere — a resize in
		// one tab can reach another.
		attachTarget = tmuxSession
	} else {
		// Empty placeholder session (its own throwaway window 0).
		if err := terminalTmuxRun(handlerCtx, "new-session", "-d", "-s", linkedName, "-x", "221", "-y", "44"); err != nil {
			log.Printf("Failed to create mirror session %s: %v, falling back to direct attach", linkedName, err)
			attachTarget = tmuxSession
		} else {
			// The mirror is what the terminal actually attaches to, so the mouse
			// has to be on here as well — session options are per session, and
			// setting it on the base session does not reach this one.
			//
			// Without it the wheel does nothing at all in a pane: tmux's own
			// WheelUpPane binding is present but never fires, and the pane keeps
			// only the few thousand lines xterm holds. Seen on a machine whose
			// global default is `mouse off`, where the base sessions were set
			// explicitly and the mirrors inherited the default.
			_ = terminalTmuxRun(handlerCtx, "set-option", "-t", linkedName, "mouse", "on")
		}
	}

	if attachTarget == linkedName {
		// Link the target window from the base into this session at the same
		// index (-k replaces our placeholder if it collides). Same window
		// object → the agent keeps running; only THIS window is in the mirror.
		linkErr := terminalTmuxRun(handlerCtx, "link-window", "-k",
			"-s", fmt.Sprintf("%s:%d", tmuxSession, winIdx),
			"-t", fmt.Sprintf("%s:%d", linkedName, winIdx))
		if linkErr != nil {
			// Link failed — clean up and fall back to grouped behaviour so the
			// tab still works (just without the isolation win).
			log.Printf("link-window failed for %s win %d: %v, falling back to grouped", tmuxSession, winIdx, linkErr)
			_ = terminalTmuxRun(handlerCtx, "kill-session", "-t", linkedName)
			_ = terminalTmuxRun(handlerCtx, "new-session", "-d", "-s", linkedName, "-t", tmuxSession)
			// Same reason as above: a session created here needs the mouse too.
			_ = terminalTmuxRun(handlerCtx, "set-option", "-t", linkedName, "mouse", "on")
		} else {
			// Leave the mirror holding exactly the linked window.
			//
			// -k is meant to replace a colliding window, and tmux does — so
			// linking onto index 0 needed no cleanup there. psmux keeps both:
			// it puts the linked window at the next free index and leaves its
			// own placeholder active at 0, so attaching showed an empty shell
			// instead of the agent.
			//
			// Rather than trust either behaviour, find where the linked window
			// actually ended up and drop everything else. If it cannot be
			// found, nothing is removed — an over-full mirror still works,
			// while killing the wrong window would take the agent with it.
			if linked, ok := linkedWindowIndex(handlerCtx, linkedName, tmuxSession, winIdx); ok {
				for _, idx := range mirrorWindowIndexes(handlerCtx, linkedName) {
					if idx != linked {
						_ = terminalTmuxRun(handlerCtx, "kill-window", "-t", fmt.Sprintf("%s:%d", linkedName, idx))
					}
				}
				winIdx = linked
			}
		}
	}

	// Window sizing stays manual so one client's resize can't drag another's.
	//
	// Applies to a direct attach too, not just a mirror: without it psmux
	// defaults to `window-size latest`, which resizes to whichever client
	// connected most recently — so opening a second tab reshapes the first
	// one's window underneath it, and that tab keeps drawing at a size that is
	// no longer in force until something re-sends one.
	//
	// It is not a complete fix on psmux, which sizes per SESSION rather than
	// per window (measured: a size from either of two clients applied to both
	// windows, manual or not). The frontend therefore also re-announces the
	// active tab's size on every switch; this option stops the churn, that
	// keeps the visible tab correct.
	_ = terminalTmuxRun(handlerCtx, "set-option", "-t", attachTarget, "window-size", "manual")
	_ = terminalTmuxRun(handlerCtx, "set-window-option", "-t", attachTarget, "aggressive-resize", "off")

	// Hide tmux status bar in the session (the desktop app has its own UI)
	_ = terminalTmuxRun(handlerCtx, "set-option", "-t", attachTarget, "status", "off")

	// Let focus reach the agent. Without it Claude Code prints a notice into its
	// own UI — "tmux focus-events off · add 'set -g focus-events on' to
	// ~/.tmux.conf and reattach" — which lands in the middle of its frame and
	// reads as a rendering fault. Set globally because it is a server option;
	// psmux accepts it, verified before relying on it.
	_ = terminalTmuxRun(handlerCtx, "set-option", "-g", "focus-events", "on")

	// Select the target window in the session
	_ = terminalTmuxRun(handlerCtx, "select-window", "-t", fmt.Sprintf("%s:%d", attachTarget, winIdx))

	// Attach to the session, by id rather than by name.
	//
	// Measured on psmux: two clients attached to two different sessions both
	// ended up bound to the SAME one, leaving the other session with no client
	// at all — which is a terminal that shows nothing and accepts no typing,
	// while its neighbour works fine. Session ids ($61, $62) resolve exactly,
	// where the names involved did not.
	//
	// Falling back to the name keeps a failed lookup from blocking the attach:
	// worst case is the behaviour we already had.
	// Whether this connection owns a mirror session, recorded BEFORE the name is
	// swapped for an id below.
	//
	// Resizing is allowed only on our own mirror — never on the shared base
	// session, which would resize it under every other client. That check used
	// to compare attachTarget against linkedName at resize time, but by then
	// attachTarget holds a session id ($700) while linkedName is still a name,
	// so it never matched and the resize was skipped: the window stayed at the
	// size it was created with while the client asked for something else, and
	// every line wrapped in the wrong place until the user pressed Refresh.
	attachedToOwnMirror := attachTarget == linkedName

	if id := session.SessionIDForContext(handlerCtx, attachTarget); id != "" && id != attachTarget {
		log.Printf("[ws] attaching by id %s (%s)", id, attachTarget)
		attachTarget = id
	}
	// Name the window in the attach target, so this connection is pinned to the
	// tab it belongs to.
	//
	// The keystroke target used to be derived from the session's ACTIVE window,
	// resolved once at attach. Opening another tab makes that tab active, and
	// from then on the older tab's client is aiming at a window that moved out
	// from under it — measured: with tabs 0..3 open, the session reported its
	// active window as 3, so input meant for tab 0 was addressed to tab 3's
	// coordinates and simply vanished.
	windowTarget := fmt.Sprintf("%s:%d", attachTarget, winIdx)
	cmd := session.TmuxCommandContext(handlerCtx, "attach-session", "-t", windowTarget)
	// Force a sane TERM. When the app is launched from a desktop menu / KRunner
	// instead of a shell, it inherits TERM=dumb (or empty), and tmux refuses to
	// attach with "open terminal failed: terminal does not support clear".
	// xterm.js speaks xterm-256color, so pin that for the attach PTY.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	// Force a UTF-8 locale for the same reason.
	//
	// tmux decides whether to run in UTF-8 mode from LC_ALL/LC_CTYPE/LANG. A
	// GUI launch inherits none of them — `launchctl getenv LANG` is empty on
	// macOS, and a .desktop launch is no better — so tmux falls back to a
	// non-UTF-8 mode and mangles every multi-byte character on its way to the
	// client. The pane's own contents stay correct (verified with
	// capture-pane: "Zoltán" arrives intact), which is what makes this look
	// like a font or renderer fault: accented letters, box drawing and emoji
	// come out as replacement blocks no matter which renderer draws them.
	cmd.Env = append(cmd.Env, "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")

	ptmx, err := session.StartTerminal(cmd)
	if err != nil {
		// Clean up linked session on error (only if it was created)
		if attachedToOwnMirror {
			_ = terminalTmuxRun(handlerCtx, "kill-session", "-t", linkedName)
		}
		if handlerCtx.Err() == nil {
			_ = ws.SetWriteDeadline(time.Now().Add(terminalWSWriteTimeout))
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		}
		return
	}
	// The session lookup and terminal creation are now tied to the same pinned
	// project snapshot. The long-lived WebSocket must not block later switches.
	releaseAttach()
	attachReleased = true

	connID := fmt.Sprintf("%s-%d", sessionID, winIdx)
	tc := &termConn{
		ws:   ws,
		ptmx: ptmx,
		cmd:  cmd,
		done: make(chan struct{}),
	}

	ts.mu.Lock()
	if ts.stopping {
		ts.mu.Unlock()
		// Stop won before this attach reached registration. It cannot be put in
		// the connection map now (Stop has already snapshotted it), so release it
		// synchronously instead of leaving an untracked multiplexer child behind.
		tc.closeTransport()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		if attachedToOwnMirror {
			_ = terminalTmuxRun(handlerCtx, "kill-session", "-t", linkedName)
		}
		return
	}
	// Close existing connection if any
	if old, exists := ts.conns[connID]; exists {
		old.closeTransport()
	}
	ts.conns[connID] = tc
	// Output pump, its blocking PTY reader, and input/cleanup pump. Add all
	// three before releasing the lifecycle mutex so Stop cannot begin Wait while
	// a connection is still about to register one of its goroutines.
	ts.connWG.Add(3)
	ts.mu.Unlock()
	connectionOwned = true
	log.Printf("[ws] attach session=%s win=%d target=%s", sessionID, winIdx, attachTarget)
	writeTerminalOutput := tc.writeBinary

	// Read from PTY, write to WebSocket with output throttling.
	// Without throttling, rapid terminal output (Claude Code spinners, etc.)
	// causes WebKit to use 100% CPU due to excessive rendering.
	// We batch PTY output and flush at ~120fps max.
	go func() {
		defer ts.connWG.Done()
		buf := make([]byte, 32768)
		var pendingData []byte
		// ~33 fps — more than enough for a terminal UI. Higher tick rates
		// (we had 8ms ≈ 120 fps) caused WebKit renderer CPU to stay pinned
		// because every flush is a canvas write on the frontend.
		flushTicker := time.NewTicker(30 * time.Millisecond)
		defer flushTicker.Stop()

		dataCh := make(chan []byte, 64)
		errCh := make(chan error, 1)

		// PTY reader goroutine. The send must select on tc.done as well:
		// when the connection closes the consumer loop below returns, and
		// if dataCh happens to be full at that moment a plain `dataCh <-`
		// would block this goroutine (and its buffer) forever — a leak that
		// accumulates across reconnects.
		go func() {
			defer ts.connWG.Done()
			for {
				n, err := ptmx.Read(buf)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])
					select {
					case dataCh <- chunk:
					case <-tc.done:
						return
					}
				}
				if err != nil {
					select {
					case errCh <- err:
					case <-tc.done:
					}
					return
				}
			}
		}()

		for {
			select {
			case <-tc.done:
				return
			case err := <-errCh:
				// The reader sends every chunk before it reports EOF, but data and
				// errors use separate buffered channels. Drain already-queued chunks
				// because select is allowed to choose errCh first when both are ready.
			drainTerminalData:
				for {
					select {
					case chunk := <-dataCh:
						pendingData = append(pendingData, chunk...)
					default:
						break drainTerminalData
					}
				}
				// Flush remaining data before exit.
				if len(pendingData) > 0 {
					_, _ = tc.deliverOrHold(pendingData, writeTerminalOutput)
				}
				if err != io.EOF {
					log.Printf("PTY read error: %v", err)
				}
				// The stream is gone, so this terminal will never paint again.
				// Closing the socket tells the frontend that, instead of
				// leaving it holding an open connection to a dead pane — a tab
				// that still accepts keystrokes (they go out over separate
				// send-keys calls) while the screen stays frozen, which reads
				// as "the session is stuck" when the session is perfectly fine.
				log.Printf("[term] stream ended session=%s win=%d: %v", sessionID, winIdx, err)
				tc.writeMu.Lock()
				_ = ws.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "terminal stream ended"),
					time.Now().Add(time.Second))
				tc.writeMu.Unlock()
				tc.closeTransport()
				return
			case chunk := <-dataCh:
				// Hidden (background) tab: hold the bytes here instead of
				// sending them.
				//
				// Not sending is the point — every WS frame is dispatched on the
				// webview's single main thread, and a background agent's output
				// would starve the foreground tab's keystrokes. But they used to
				// be DROPPED, and that is what made a returning tab show a
				// half-repainted screen with leftovers: tmux then believes the
				// client is current and sends only differences against a state
				// it never received. Recovering from that needed a Ctrl-L, which
				// is input, and repeatedly ended up in an agent's composer.
				//
				// Held in the backend rather than the frontend: this is Go
				// memory rather than the webview's, and the frontend's own
				// hidden buffer never saw any of this anyway, because the bytes
				// were gone before they reached it.
				// Anything already queued goes in first, then this chunk, so
				// the held bytes stay in the order tmux produced them. The
				// visibility decision is made under the same lock as reveal.
				if tc.holdIfHidden(pendingData, chunk) {
					pendingData = pendingData[:0]
					continue
				}
				pendingData = append(pendingData, chunk...)
				// Only bypass the flush ticker when the buffer would otherwise
				// grow unboundedly in a single tick window. 64 KB is high enough
				// that normal bursty output (Claude redraws ~20–30 KB) still
				// gets coalesced via the ticker instead of slamming the WebKit
				// renderer with back-to-back canvas writes.
				if len(pendingData) >= 65536 {
					_, err := tc.deliverOrHold(pendingData, writeTerminalOutput)
					pendingData = pendingData[:0]
					if tc.handleOutputWriteError(err) {
						log.Printf("WebSocket write error: %v", err)
						return
					}
				}
			case <-flushTicker.C:
				if len(pendingData) > 0 {
					_, err := tc.deliverOrHold(pendingData, writeTerminalOutput)
					pendingData = pendingData[:0]
					if tc.handleOutputWriteError(err) {
						log.Printf("WebSocket write error: %v", err)
						return
					}
				}
			}
		}
	}()

	// Read from WebSocket, write to PTY
	go func() {
		defer ts.connWG.Done()
		defer func() {
			ts.mu.Lock()
			// Only remove the map entry if it still points at THIS conn. A
			// fast reconnect (tab-restart re-show) registers a NEW conn under
			// the same connID before the old conn's cleanup runs — an
			// unconditional delete would silently unregister the new
			// connection and break dictation writes (conns[connID] lookups).
			if cur, ok := ts.conns[connID]; ok && cur == tc {
				delete(ts.conns, connID)
			}
			ts.mu.Unlock()

			tc.closeTransport()
			// Nobody will come back to this connection, so whatever it was
			// holding for a hidden tab is dead weight until the conn is
			// collected — up to the full limit, per closed tab.
			tc.discardHeldWhileHidden()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()

			// Clean up the linked tmux session (only if it was created)
			if attachedToOwnMirror {
				_ = terminalTmuxRun(handlerCtx, "kill-session", "-t", linkedName)
			}
			// A redraw waiting for this tab has nowhere to go now.
			log.Printf("[ws] detach session=%s win=%d target=%s", sessionID, winIdx, attachTarget)
		}()

		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// Control messages (resize 0x01, visibility 0x02) are sent by the
			// frontend as BINARY frames; raw keystroke input arrives as TEXT
			// frames (xterm.onData → ws.send(string)). Routing by frame type —
			// not by the first byte — is essential: otherwise a keystroke whose
			// first byte happens to be 0x01 (Ctrl+A) or 0x02 (Ctrl+B, the tmux
			// prefix!) would be swallowed as a control message and never reach
			// the PTY. Text frames are therefore ALWAYS written to the PTY.
			if msgType == websocket.TextMessage {
				if ts.typingSignal != nil {
					atomic.StoreInt64(ts.typingSignal, time.Now().UnixNano())
				}
				// The error was previously discarded, which made a whole class
				// of failure invisible: on Windows this write shells out to
				// send-keys, so it can fail on its own while the socket stays
				// healthy — keystrokes vanish with nothing logged anywhere.
				if _, werr := tc.writeInput(data); werr != nil {
					log.Printf("[ws] input write failed session=%s win=%d (%d bytes): %v",
						sessionID, winIdx, len(data), werr)
					tc.handleInputWriteError(werr)
					return
				} else if session.DebugLogging {
					log.Printf("[ws] input session=%s win=%d %d bytes", sessionID, winIdx, len(data))
				}
				continue
			}

			switch msgType {
			case websocket.BinaryMessage:
				// Check for resize message
				if len(data) > 0 && data[0] == 0x01 {
					// Resize: 0x01 + cols (2 bytes) + rows (2 bytes)
					if len(data) >= 5 {
						cols := int(data[1])<<8 | int(data[2])
						rows := int(data[3])<<8 | int(data[4])
						// Ignore tiny/bogus sizes that would make tmux render
						// the pane at ~5 columns wide. Usually caused by
						// measuring a still-hidden container on the frontend.
						if cols < 10 || rows < 3 {
							continue
						}
						// Logged because a mismatch between what the frontend
						// measures and what the multiplexer renders shows up as
						// content offset by a line or two, with nothing else to
						// distinguish which side is wrong.
						if session.DebugLogging {
							log.Printf("[ws] resize session=%s win=%d %dx%d",
								sessionID, winIdx, cols, rows)
						}
						// On Unix this is the PTY ioctl. On Windows the pipe
						// carries no size, so this sends the multiplexer's own
						// refresh-client over the control-mode channel — and
						// that is the ONLY lever there: psmux ignores the
						// resize-window below entirely (exits 0, changes
						// nothing), so this call is what sizes the pane.
						session.SetTerminalSize(ptmx, cols, rows)
						// Resize this mirror's window to EXACTLY this client's
						// size. We deliberately do NOT use `-A` (aggregate =
						// largest client): with grouped per-tab mirrors, `-A`
						// would size the shared window to the biggest of all
						// attached mirrors, letting a background tab drag the
						// active window's size and trigger redraw churn. Pinning
						// to this client's own cols×rows (paired with the
						// `window-size manual` set at attach) keeps each tab's
						// view independent. Only meaningful on our linked
						// session; on a fallback direct attach we skip it so we
						// don't resize the shared base session under the user.
						// Where mirrors are unavailable there is no shared session
						// to protect — every attach is direct, so skipping the
						// resize would leave the terminal stuck at its opening
						// size for the whole run.
						if attachedToOwnMirror || !session.MirrorSupported() {
							_ = terminalTmuxRun(handlerCtx, "resize-window", "-t",
								fmt.Sprintf("%s:%d", attachTarget, winIdx),
								"-x", fmt.Sprintf("%d", cols),
								"-y", fmt.Sprintf("%d", rows))
							session.RefreshSessionClientsContext(handlerCtx, attachTarget)
							// No Ctrl-L here. resize-window signals the program, and a
							// TUI lays itself out again in response — the keystroke
							// only ever covered the gap while returning tabs kept a
							// stale frame, and the client clearing its own screen on
							// return closed that gap.
							//
							// Sending it anyway is not free: Ctrl-L is INPUT, and only
							// a program that chooses to read it as "redraw" treats it
							// that way. An interactive list — Codex's /resume picker —
							// reads it as a keystroke and wipes the screen. Claude
							// Code turned bursts of it into a "/clear" in the
							// composer. The Refresh button still sends one, where the
							// user has asked for it.
						}
					}
				} else if len(data) >= 2 && data[0] == 0x02 {
					// Visibility: 0x02 + (1 = visible, 0 = hidden). A hidden tab
					// has its PTY output dropped at the backend (see the pump
					// loop) so a background agent can't starve the foreground
					// tab's input on the single webview main thread.
					if data[1] == 0 {
						tc.setHidden()
						// Also tell the stream: on Windows its pane-size watcher
						// skips hidden tabs, which is most of them most of the
						// time (each check is a process launch, ~20ms).
						session.SetTerminalVisible(ptmx, false)
					} else {
						// Was this tab actually hidden? The flag starts at 0, so
						// the first "become visible" after an attach reports a
						// transition that never happened.
						session.SetTerminalVisible(ptmx, true)

						// Coming back to the foreground: send what was held
						// while this tab was hidden.
						//
						// This is the whole recovery. The bytes are the ones
						// tmux already produced, in order, so replaying them
						// leaves the client's screen exactly where the pane is —
						// no repaint to ask for, and no Ctrl-L, which is input
						// and repeatedly ended up in an agent's composer.
						wasHidden, overflowed, replayErr := tc.reveal(writeTerminalOutput)
						if replayErr != nil {
							log.Printf("[ws] held output replay failed session=%s win=%d: %v",
								sessionID, winIdx, replayErr)
							return
						}
						if wasHidden {
							if overflowed {
								// More was produced than is worth holding, so
								// the screen cannot be reconstructed from the
								// stream. Ask tmux to repaint instead: that is
								// incomplete on its own, but it is the honest
								// fallback and it sends no input.
								// Repaint whatever this connection is attached
								// to. Gated on the mirror alone, a tab that fell
								// back to a direct attach got no repaint at all
								// after an overflow — the screen simply stayed
								// half-drawn until something else happened to
								// redraw it. RefreshSessionClients only asks the
								// server to resend what it already has, so there
								// is nothing here to protect a shared session
								// from, unlike the resize above.
								session.RefreshSessionClientsContext(handlerCtx, attachTarget)
								// Logged unconditionally, unlike the ordinary
								// replay above: this is the path where a tab
								// comes back to a screen rebuilt by repaint
								// rather than restored byte for byte, so it is
								// the first thing to look for when someone
								// reports a stale-looking tab. Once per
								// overflow, not per frame.
								log.Printf("[ws] held output overflowed while hidden, repainting session=%s win=%d",
									sessionID, winIdx)
							}
						}
					}
				} else {
					// A binary frame that isn't a known control message — treat
					// as raw input (defensive; the frontend sends keystrokes as
					// text frames, which are handled before this switch).
					if ts.typingSignal != nil {
						atomic.StoreInt64(ts.typingSignal, time.Now().UnixNano())
					}
					if _, werr := tc.writeInput(data); werr != nil {
						log.Printf("[ws] binary input write failed session=%s win=%d (%d bytes): %v",
							sessionID, winIdx, len(data), werr)
						tc.handleInputWriteError(werr)
						return
					}
				}
			}
		}
	}()
}

// GetPort returns the WebSocket server port
func (ts *TerminalServer) GetPort() int {
	return ts.port
}
