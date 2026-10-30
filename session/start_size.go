package session

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The size a new multiplexer session starts at: the viewer's terminal, as
// last seen.
//
// A session created without a size gets tmux's default, 80x24, and keeps it
// until a terminal attaches. Nobody is looking at a session reopened at start
// or started in the background, so an agent in it ran at 80 columns — and
// Claude Code reprints the whole conversation when it resumes. Those lines
// stayed in the scrollback wrapped at 80 once the tab was opened at its real
// width: everything above the newest output read as a narrow column.
//
// Remembered across restarts in a small file, because reopening interrupted
// sessions happens as the app starts, before any terminal has a size to give.

const (
	// Smaller than this is a pane measured mid-layout, not a viewer's size.
	minStartCols = 40
	minStartRows = 10
)

// startSizeSaveDelay: a window drag sends a size at every step; only where it
// settles is saved. A variable so a test need not wait for it.
var startSizeSaveDelay = 2 * time.Second

var startSize struct {
	mu        sync.Mutex
	cols      int
	rows      int
	path      string
	saveTimer *time.Timer
}

// ConfigureStartSize reads the remembered size and saves later ones to path.
// Until it is called nothing is written, so a test never touches a real
// configuration directory.
func ConfigureStartSize(path string) {
	cols, rows := readStartSize(path)
	startSize.mu.Lock()
	defer startSize.mu.Unlock()
	startSize.path = path
	if cols > 0 && startSize.cols == 0 {
		startSize.cols, startSize.rows = cols, rows
	}
}

// SetStartSize records the viewer's terminal size. An implausibly small one —
// a hidden or half laid-out pane — is ignored.
func SetStartSize(cols, rows int) {
	if !plausibleStartSize(cols, rows) {
		return
	}
	startSize.mu.Lock()
	defer startSize.mu.Unlock()
	if startSize.cols == cols && startSize.rows == rows {
		return
	}
	startSize.cols, startSize.rows = cols, rows
	if startSize.path == "" {
		return
	}
	if startSize.saveTimer != nil {
		startSize.saveTimer.Stop()
	}
	path := startSize.path
	startSize.saveTimer = time.AfterFunc(startSizeSaveDelay, func() {
		startSize.mu.Lock()
		cols, rows := startSize.cols, startSize.rows
		startSize.mu.Unlock()
		if err := writeStartSize(path, cols, rows); err != nil {
			log.Printf("[size] could not remember the terminal size: %v", err)
		}
	})
}

// FlushStartSize writes a size still waiting out the save delay, at shutdown.
func FlushStartSize() {
	startSize.mu.Lock()
	pending := startSize.saveTimer != nil && startSize.saveTimer.Stop()
	startSize.saveTimer = nil
	path, cols, rows := startSize.path, startSize.cols, startSize.rows
	startSize.mu.Unlock()
	if pending && path != "" {
		if err := writeStartSize(path, cols, rows); err != nil {
			log.Printf("[size] could not remember the terminal size: %v", err)
		}
	}
}

// StartSize returns the size new sessions start at, or zeros when unknown.
func StartSize() (cols, rows int) {
	startSize.mu.Lock()
	defer startSize.mu.Unlock()
	return startSize.cols, startSize.rows
}

// startSizeArgs returns new-session's size flags, or nothing when the size is
// not known yet. Not for a local psmux, on Windows: it is not known to take
// them, and a session that fails to start is worse than a narrow one. A
// session on a server runs real tmux, wherever the app does.
func startSizeArgs(remote bool) []string {
	if !remote && runtime.GOOS == "windows" {
		return nil
	}
	cols, rows := StartSize()
	if cols == 0 {
		return nil
	}
	return []string{"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)}
}

// newSessionArgs builds a detached new-session at the start size.
func newSessionArgs(sessionName, dir string, remote bool) []string {
	args := []string{"new-session", "-d", "-s", sessionName}
	args = append(args, startSizeArgs(remote)...)
	return append(args, "-c", dir)
}

func plausibleStartSize(cols, rows int) bool {
	return cols >= minStartCols && cols <= 1000 && rows >= minStartRows && rows <= 500
}

func readStartSize(path string) (int, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	colsText, rowsText, ok := strings.Cut(strings.TrimSpace(string(data)), "x")
	if !ok {
		return 0, 0
	}
	cols, colsErr := strconv.Atoi(colsText)
	rows, rowsErr := strconv.Atoi(rowsText)
	if colsErr != nil || rowsErr != nil || !plausibleStartSize(cols, rows) {
		return 0, 0
	}
	return cols, rows
}

func writeStartSize(path string, cols, rows int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(fmt.Sprintf("%dx%d\n", cols, rows)), 0o644)
}

// StartSizePath is where the remembered terminal size is kept.
func (s *Storage) StartSizePath() string {
	return filepath.Join(s.configDir, "terminal-size")
}
