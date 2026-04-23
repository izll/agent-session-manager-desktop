package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"asmgr-desktop/session"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32768,
	WriteBufferSize: 32768,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local dev
	},
}

// TerminalServer handles WebSocket connections for terminal I/O
type TerminalServer struct {
	storage        *session.Storage
	port           int
	mu             sync.RWMutex
	conns          map[string]*termConn
	typingSignal   *int64 // pointer to App.lastTypingSignal for zero-overhead typing detection
}

type termConn struct {
	ws        *websocket.Conn
	ptmx      *os.File
	cmd       *exec.Cmd
	done      chan struct{}
	writeMu   sync.Mutex
	closeOnce sync.Once
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
	_, err := tc.ptmx.WriteString(data)
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

// NewTerminalServer creates a new terminal WebSocket server
func NewTerminalServer(storage *session.Storage, port int) *TerminalServer {
	return &TerminalServer{
		storage: storage,
		port:    port,
		conns:   make(map[string]*termConn),
	}
}

// Start starts the WebSocket server
func (ts *TerminalServer) Start() error {
	http.HandleFunc("/terminal", ts.handleTerminal)

	addr := fmt.Sprintf("127.0.0.1:%d", ts.port)
	log.Printf("Terminal WebSocket server starting on %s", addr)

	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("Terminal server error: %v", err)
		}
	}()

	return nil
}

// handleTerminal handles WebSocket connections
func (ts *TerminalServer) handleTerminal(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	windowIdx := r.URL.Query().Get("window")

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
	linkedName := fmt.Sprintf("%s_gui_%d_%d", tmuxSession, winIdx, time.Now().UnixMilli())

	// Create a grouped (linked) tmux session targeting the specific window
	attachTarget := linkedName
	createCmd := exec.Command("tmux", "new-session", "-d", "-s", linkedName, "-t", tmuxSession)
	if err := createCmd.Run(); err != nil {
		// Fallback to direct attach if grouped session fails
		log.Printf("Failed to create linked session %s: %v, falling back to direct attach", linkedName, err)
		attachTarget = tmuxSession
	}

	// Hide tmux status bar in the session (the desktop app has its own UI)
	exec.Command("tmux", "set-option", "-t", attachTarget, "status", "off").Run()

	// Select the target window in the session
	selectCmd := exec.Command("tmux", "select-window", "-t", fmt.Sprintf("%s:%d", attachTarget, winIdx))
	selectCmd.Run()

	// Attach to the session (linked sessions have their own active window state)
	cmd := exec.Command("tmux", "attach-session", "-t", attachTarget)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		// Clean up linked session on error (only if it was created)
		if attachTarget == linkedName {
			exec.Command("tmux", "kill-session", "-t", linkedName).Run()
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

		// PTY reader goroutine
		go func() {
			for {
				n, err := ptmx.Read(buf)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])
					dataCh <- chunk
				}
				if err != nil {
					errCh <- err
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
				return
			case chunk := <-dataCh:
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
				if len(pendingData) > 0 {
					tc.writeMu.Lock()
					err := ws.WriteMessage(websocket.BinaryMessage, pendingData)
					tc.writeMu.Unlock()
					pendingData = pendingData[:0]
					if err != nil {
						log.Printf("WebSocket write error: %v", err)
						return
					}
				}
			}
		}
	}()

	// Read from WebSocket, write to PTY
	go func() {
		defer func() {
			ts.mu.Lock()
			delete(ts.conns, connID)
			ts.mu.Unlock()

			tc.closeOnce.Do(func() {
				close(tc.done)
			})
			ptmx.Close()
			ws.Close()
			cmd.Process.Kill()

			// Clean up the linked tmux session (only if it was created)
			if attachTarget == linkedName {
				exec.Command("tmux", "kill-session", "-t", linkedName).Run()
			}
		}()

		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			switch msgType {
			case websocket.TextMessage, websocket.BinaryMessage:
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
						pty.Setsize(ptmx, &pty.Winsize{
							Cols: uint16(cols),
							Rows: uint16(rows),
						})
						// Also resize the window (aggregate) so tmux picks up
						// the biggest client and sends a full redraw.
						exec.Command("tmux", "resize-window", "-t", attachTarget, "-A").Run()
						exec.Command("tmux", "refresh-client", "-t", attachTarget).Run()
					}
				} else {
					// Regular input - signal typing for polling suppression
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
