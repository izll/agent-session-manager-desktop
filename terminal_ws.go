package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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

// mirrorWindowIndexes lists the window indexes present in a mirror session.
func mirrorWindowIndexes(sessionName string) []int {
	out, err := session.TmuxCommand("list-windows", "-t", sessionName,
		"-F", "#{window_index}").Output()
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
func linkedWindowIndex(mirror, base string, baseIdx int) (int, bool) {
	wantOut, err := session.TmuxCommand("display-message", "-p", "-t",
		fmt.Sprintf("%s:%d", base, baseIdx), "#{window_id}").Output()
	if err != nil {
		return 0, false
	}
	want := strings.TrimSpace(string(wantOut))
	if want == "" {
		return 0, false
	}

	listOut, err := session.TmuxCommand("list-windows", "-t", mirror,
		"-F", "#{window_index} #{window_id}").Output()
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
	// attachAllowed points at App.projectLocked. When false (another instance
	// owns the active project), we refuse terminal attaches so two GUIs can't
	// fight over the same tmux sessions and kill each other's ptys.
	attachAllowed *bool
}

type termConn struct {
	ws *websocket.Conn
	// ptmx is the attached multiplexer's byte stream. It is a PTY master on
	// Unix and a pipe pair on Windows (see session.StartTerminal); everything
	// below only reads, writes and closes it, which is all both can do.
	ptmx      session.TerminalStream
	cmd       *exec.Cmd
	done      chan struct{}
	writeMu   sync.Mutex
	closeOnce sync.Once
	// hidden: when 1, this tab is in the background. We keep reading the PTY
	// (so tmux never blocks) but DROP the output instead of sending WS frames.
	// On WebKitGTK every WS frame is dispatched on the single webview main
	// thread; a background agent flooding output would otherwise starve the
	// FOREGROUND tab's keystroke handling — the user-visible asymmetry where a
	// heavy background agent made typing in the visible tab unbearably laggy.
	// The agent keeps running; on un-hide the frontend asks tmux to redraw.
	hidden int32
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

	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()
	_, err := tc.ptmx.Write([]byte(data))
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

	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()
	_, err := tc.ptmx.Write(bs)
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
	return &TerminalServer{
		storage:   storage,
		port:      port,
		authToken: hex.EncodeToString(b),
		conns:     make(map[string]*termConn),
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
	http.HandleFunc("/terminal", ts.handleTerminal)

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

	if ts.port != requested {
		log.Printf("Terminal WebSocket server bound to fallback port %d (preferred %d was busy)", ts.port, requested)
	} else {
		log.Printf("Terminal WebSocket server starting on 127.0.0.1:%d", ts.port)
	}

	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Printf("Terminal server error: %v", err)
		}
	}()

	return nil
}

// handleTerminal handles WebSocket connections
func (ts *TerminalServer) handleTerminal(w http.ResponseWriter, r *http.Request) {
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
	if ts.attachAllowed != nil && !*ts.attachAllowed {
		http.Error(w, "project locked by another instance", http.StatusConflict)
		log.Printf("[terminal] refused attach: project locked by another instance")
		return
	}

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

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
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
		if err := session.TmuxCommand("has-session", "-t", tmuxSession).Run(); err == nil {
			running = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !running {
		http.Error(w, "session not running", http.StatusNotFound)
		log.Printf("[ws] refused attach: %s is not running", tmuxSession)
		return
	}

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
		createCmd := session.TmuxCommand("new-session", "-d", "-s", linkedName, "-x", "221", "-y", "44")
		if err := createCmd.Run(); err != nil {
			log.Printf("Failed to create mirror session %s: %v, falling back to direct attach", linkedName, err)
			attachTarget = tmuxSession
		}
	}

	if attachTarget == linkedName {
		// Link the target window from the base into this session at the same
		// index (-k replaces our placeholder if it collides). Same window
		// object → the agent keeps running; only THIS window is in the mirror.
		linkErr := session.TmuxCommand("link-window", "-k",
			"-s", fmt.Sprintf("%s:%d", tmuxSession, winIdx),
			"-t", fmt.Sprintf("%s:%d", linkedName, winIdx)).Run()
		if linkErr != nil {
			// Link failed — clean up and fall back to grouped behaviour so the
			// tab still works (just without the isolation win).
			log.Printf("link-window failed for %s win %d: %v, falling back to grouped", tmuxSession, winIdx, linkErr)
			session.TmuxCommand("kill-session", "-t", linkedName).Run()
			session.TmuxCommand("new-session", "-d", "-s", linkedName, "-t", tmuxSession).Run()
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
			if linked, ok := linkedWindowIndex(linkedName, tmuxSession, winIdx); ok {
				for _, idx := range mirrorWindowIndexes(linkedName) {
					if idx != linked {
						session.TmuxCommand("kill-window", "-t", fmt.Sprintf("%s:%d", linkedName, idx)).Run()
					}
				}
				winIdx = linked
			}
		}
		// Window sizing stays manual so a resize on one mirror can't ripple.
		session.TmuxCommand("set-option", "-t", attachTarget, "window-size", "manual").Run()
		session.TmuxCommand("set-window-option", "-t", attachTarget, "aggressive-resize", "off").Run()
	}

	// Hide tmux status bar in the session (the desktop app has its own UI)
	session.TmuxCommand("set-option", "-t", attachTarget, "status", "off").Run()

	// Select the target window in the session
	selectCmd := session.TmuxCommand("select-window", "-t", fmt.Sprintf("%s:%d", attachTarget, winIdx))
	selectCmd.Run()

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
	if id := session.SessionIDFor(attachTarget); id != "" && id != attachTarget {
		log.Printf("[ws] attaching by id %s (%s)", id, attachTarget)
		attachTarget = id
	}
	cmd := session.TmuxCommand("attach-session", "-t", attachTarget)
	// Force a sane TERM. When the app is launched from a desktop menu / KRunner
	// instead of a shell, it inherits TERM=dumb (or empty), and tmux refuses to
	// attach with "open terminal failed: terminal does not support clear".
	// xterm.js speaks xterm-256color, so pin that for the attach PTY.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := session.StartTerminal(cmd)
	if err != nil {
		// Clean up linked session on error (only if it was created)
		if attachTarget == linkedName {
			session.TmuxCommand("kill-session", "-t", linkedName).Run()
		}
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v", err)))
		ws.Close()
		return
	}

	connID := fmt.Sprintf("%s-%d", sessionID, winIdx)
	tc := &termConn{
		ws:   ws,
		ptmx: ptmx,
		cmd:  cmd,
		done: make(chan struct{}),
	}

	ts.mu.Lock()
	// Close existing connection if any
	if old, exists := ts.conns[connID]; exists {
		old.closeOnce.Do(func() {
			close(old.done)
		})
		old.ptmx.Close()
		old.ws.Close()
	}
	ts.conns[connID] = tc
	ts.mu.Unlock()
	log.Printf("[ws] attach session=%s win=%d target=%s", sessionID, winIdx, attachTarget)

	// Read from PTY, write to WebSocket with output throttling.
	// Without throttling, rapid terminal output (Claude Code spinners, etc.)
	// causes WebKit to use 100% CPU due to excessive rendering.
	// We batch PTY output and flush at ~120fps max.
	go func() {
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
				// Flush remaining data before exit
				if len(pendingData) > 0 {
					tc.writeMu.Lock()
					ws.WriteMessage(websocket.BinaryMessage, pendingData)
					tc.writeMu.Unlock()
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
				_ = ws.Close()
				return
			case chunk := <-dataCh:
				// Hidden (background) tab: keep draining the PTY so tmux never
				// blocks, but DROP the bytes — sending WS frames to a hidden tab
				// only burns the webview's single main thread and starves the
				// foreground tab's input. On un-hide the frontend triggers a
				// tmux redraw, so nothing is permanently lost.
				if atomic.LoadInt32(&tc.hidden) == 1 {
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
					tc.writeMu.Lock()
					err := ws.WriteMessage(websocket.BinaryMessage, pendingData)
					tc.writeMu.Unlock()
					pendingData = pendingData[:0]
					if err != nil {
						log.Printf("WebSocket write error: %v", err)
						return
					}
				}
			case <-flushTicker.C:
				if len(pendingData) > 0 && atomic.LoadInt32(&tc.hidden) == 0 {
					tc.writeMu.Lock()
					err := ws.WriteMessage(websocket.BinaryMessage, pendingData)
					tc.writeMu.Unlock()
					pendingData = pendingData[:0]
					if err != nil {
						log.Printf("WebSocket write error: %v", err)
						return
					}
				} else if atomic.LoadInt32(&tc.hidden) == 1 {
					pendingData = pendingData[:0]
				}
			}
		}
	}()

	// Read from WebSocket, write to PTY
	go func() {
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

			tc.closeOnce.Do(func() {
				close(tc.done)
			})
			ptmx.Close()
			ws.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()

			// Clean up the linked tmux session (only if it was created)
			if attachTarget == linkedName {
				session.TmuxCommand("kill-session", "-t", linkedName).Run()
			}
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
				ptmx.Write(data)
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
						if attachTarget == linkedName || !session.MirrorSupported() {
							session.TmuxCommand("resize-window", "-t",
								fmt.Sprintf("%s:%d", attachTarget, winIdx),
								"-x", fmt.Sprintf("%d", cols),
								"-y", fmt.Sprintf("%d", rows)).Run()
							session.TmuxCommand("refresh-client", "-t", attachTarget).Run()
						}
					}
				} else if len(data) >= 2 && data[0] == 0x02 {
					// Visibility: 0x02 + (1 = visible, 0 = hidden). A hidden tab
					// has its PTY output dropped at the backend (see the pump
					// loop) so a background agent can't starve the foreground
					// tab's input on the single webview main thread.
					if data[1] == 0 {
						atomic.StoreInt32(&tc.hidden, 1)
					} else {
						atomic.StoreInt32(&tc.hidden, 0)
						// Coming back to the foreground: force tmux to repaint the
						// pane so we recover everything that was dropped while
						// hidden, in a single redraw.
						if attachTarget == linkedName {
							session.TmuxCommand("refresh-client", "-t", attachTarget).Run()
						}
					}
				} else {
					// A binary frame that isn't a known control message — treat
					// as raw input (defensive; the frontend sends keystrokes as
					// text frames, which are handled before this switch).
					if ts.typingSignal != nil {
						atomic.StoreInt64(ts.typingSignal, time.Now().UnixNano())
					}
					ptmx.Write(data)
				}
			}
		}
	}()
}

// GetPort returns the WebSocket server port
func (ts *TerminalServer) GetPort() int {
	return ts.port
}
