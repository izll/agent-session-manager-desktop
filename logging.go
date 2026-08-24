package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"asmgr-desktop/dictation"
)

// setupLogging wires the standard logger so that:
//   - EVERYTHING goes to a size-bounded log file
//     (~/.config/agent-session-manager-desktop/asmgr-desktop.log), and
//   - the terminal/stderr only sees lines whose prefix is in the
//     consoleAllowPrefixes set. This keeps the high-volume sidebar/status
//     spam out of the console while still letting targeted debug lines
//     (e.g. [SetExtraArgs], [RestartWindow]) show up live.
//
// Returns the opened log file so the caller can close it on shutdown
// (best-effort; the OS reclaims it anyway).
func setupLogging() *os.File {
	logPath := defaultLogPath()
	if logPath == "" {
		return nil
	}
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil
	}
	// Tighten directories/files created by older versions. The log can include
	// session names, filesystem paths, terminal diagnostics and dictation/API
	// failures, none of which should be readable by other local users.
	if err := os.Chmod(dir, 0700); err != nil {
		return nil
	}

	// Multiple GUI instances are supported for different projects. O_APPEND
	// plus the cross-process writer lock prevents their independent file offsets
	// from overwriting one another. Rotation below keeps history bounded.
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return nil
	}

	logOut.file = f
	logOut.path = logPath
	// Default visible everywhere; the dev build flips it via -tags devmode
	// if desired. Keep stderr quiet by default so the console is usable.
	return f
}

// LogFilePath is exposed so the frontend / a menu item could surface it.
func LogFilePath() string { return defaultLogPath() }

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agent-session-manager-desktop", "asmgr-desktop.log")
}

// consoleAllowPrefixes are the log-line prefixes that are still echoed to
// stderr. Everything else only lands in the log file. Add a prefix here
// while chasing a specific bug, remove it when done.
var consoleAllowPrefixes = []string{
	"[SetExtraArgs]",
	"[RestartWindow]",
	"[ExtraArgs]",
	"[StartSession]",
	"[StartWithResume]",
	"[StartSessionWithResume]",
	"[RestartTabWithResume]",
	// Chasing a report that the update dialog shows nothing; remove once the
	// cause is known.
	"[update]",
	"Error",
	"panic",
	"fatal",
}

// filteredLogWriter implements io.Writer. It always writes to the file and
// conditionally mirrors a line to stderr based on its prefix.
type filteredLogWriter struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// truncate empties the file this writer owns and rewinds it.
//
// Under both the process mutex and the filesystem lock because writes can come
// from every goroutine and from another GUI instance.
// truncateByPath shortens a file through a handle opened for the purpose.
//
// Windows refuses Truncate on a handle opened with O_APPEND — append mode
// grants the right to add, not to shorten — and the log handles are all
// O_APPEND, deliberately, so several instances can share one file. Unix allows
// it, which is why clearing and compacting the logs worked everywhere except
// the platform where they silently failed.
func truncateByPath(path string, size int64) error {
	handle, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Truncate(size)
}

func (w *filteredLogWriter) truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	if w.path == "" {
		// No path to reopen: an in-memory or test writer, where append mode is
		// not in play.
		return w.file.Truncate(0)
	}
	action := func() error { return truncateByPath(w.path, 0) }
	return withLogFileLock(w.path+".lock", action)
}

var logOut = &filteredLogWriter{}

func (w *filteredLogWriter) Write(p []byte) (int, error) {
	// Always persist the full line to the file.
	w.mu.Lock()
	if w.file != nil {
		write := func() error {
			if _, err := w.file.Write(p); err != nil {
				return err
			}
			return compactLogFile(w.file, maxLogFileBytes, retainedLogBytes)
		}
		if w.path == "" {
			_ = write()
		} else {
			_ = withLogFileLock(w.path+".lock", write)
		}
	}
	w.mu.Unlock()

	// The standard logger prepends a timestamp; the message prefix we care
	// about sits after it. Just substring-match against the whole line so
	// we don't have to parse the timestamp.
	line := string(p)
	for _, prefix := range consoleAllowPrefixes {
		if strings.Contains(line, prefix) {
			_, _ = os.Stderr.Write(p)
			break
		}
	}

	// Report the original length so the logger doesn't think it short-wrote.
	return len(p), nil
}

var _ io.Writer = (*filteredLogWriter)(nil)

// AppLog is the tail of the current run's log, for the viewer in Settings.
type AppLog struct {
	// Path is shown so the user can open the whole file themselves, and so a
	// bug report can name it.
	Path string `json:"path"`
	// Lines are the most recent ones, oldest first.
	Lines []string `json:"lines"`
	// Truncated is true when older lines were dropped to fit the limit.
	Truncated bool `json:"truncated"`
	// Missing is true when there is no log file — logging could not be set up,
	// or this build writes only to stderr. Distinct from an empty log.
	Missing bool `json:"missing"`
}

// maxLogLines bounds what the viewer receives.
//
// Sized against a real log rather than guessed: with ASMGR_DEBUG on, a session
// running for an hour had written 68,000 lines — so 2000 covered barely the
// last ninety seconds, and a fault from ten minutes ago had already scrolled
// out of reach. 20,000 is around twenty minutes of that, and a few megabytes
// at most over the bridge.
//
// The tail rather than the whole file: what explains a failure is always after
// it, and the beginning of a long log is poll chatter from hours ago.
const maxLogLines = 20000

const (
	// The writer keeps the file below this ceiling even during a weeks-long
	// session; the lower retained size gives it room to grow before the next
	// compaction. Both values are deliberately byte limits, so one pathological
	// line cannot bypass a line-count-only cap.
	maxLogFileBytes  = int64(32 << 20)
	retainedLogBytes = int64(16 << 20)
	maxLogReadBytes  = int64(16 << 20)
)

func compactLogFile(file *os.File, maximum, retain int64) error {
	info, err := file.Stat()
	if err != nil || info.Size() <= maximum {
		return err
	}
	if retain <= 0 || retain >= maximum {
		return fmt.Errorf("invalid log retention limits")
	}
	start := info.Size() - retain
	buf := make([]byte, retain)
	n, err := file.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return err
	}
	buf = buf[:n]
	// Begin on a complete line, not the middle of a potentially sensitive or
	// misleading diagnostic record.
	if newline := bytes.IndexByte(buf, '\n'); newline >= 0 {
		buf = buf[newline+1:]
	} else {
		buf = nil
	}
	// Reopened for the truncate: file is O_APPEND, which Windows will not let
	// us shorten. file.Name() is the path it was opened with.
	if err := truncateByPath(file.Name(), 0); err != nil {
		return err
	}
	if len(buf) != 0 {
		_, err = file.Write(buf) // O_APPEND writes at the new end after truncate.
	}
	return err
}

// ReadAppLog returns the end of the current run's log.
func ReadAppLog() AppLog {
	return readLogFile(defaultLogPath())
}

// ReadLogAt returns the end of any log the viewer offers, named by key rather
// than by path so the frontend cannot ask for an arbitrary file.
func ReadLogAt(key string) AppLog {
	switch key {
	case "dictation":
		path, err := dictationLogPath()
		if err != nil {
			return AppLog{Missing: true}
		}
		return readLogFile(path)
	default:
		return readLogFile(defaultLogPath())
	}
}

func readLogFile(path string) AppLog {
	return readLogFileWithLimit(path, maxLogReadBytes, maxLogLines)
}

func readLogFileWithLimit(path string, maxBytes int64, maxLines int) AppLog {
	result := AppLog{Path: path}
	if path == "" {
		result.Missing = true
		return result
	}

	f, err := os.Open(path)
	if err != nil {
		// A missing file is the ordinary case before anything has been logged,
		// not an error worth showing as one.
		result.Missing = true
		return result
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		result.Missing = true
		return result
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
		result.Truncated = true
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		result.Missing = true
		return result
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		result.Missing = true
		return result
	}
	if start > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			data = nil
		}
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// A file holding only a trailing newline splits to one empty string.
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		result.Truncated = true
	}
	result.Lines = lines
	return result
}

// ClearLog empties one of the offered logs.
//
// Truncates rather than deletes: the application log's file handle is held open
// for the life of the process (setupLogging), so removing the file would leave
// every later write going to an unlinked inode — the log would look empty and
// stay that way until the next launch. Truncating keeps the handle valid, and
// writes resume at the start of the file.
func ClearLog(key string) error {
	var path string
	switch key {
	case "dictation":
		p, err := dictationLogPath()
		if err != nil {
			return err
		}
		path = p
	default:
		path = defaultLogPath()
	}
	if path == "" {
		return fmt.Errorf("no log file for %q", key)
	}

	// The application log is held open by this process, so it is cleared
	// through the writer that owns it — truncating the path from underneath
	// would leave that handle writing past the end of an empty file.
	if key != "dictation" && logOut.file != nil {
		return logOut.truncate()
	}
	if key == "dictation" {
		return dictation.ClearLog()
	}

	// O_TRUNC on an existing file, created if absent so clearing a log that has
	// not been written yet is not an error.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// dictationLogPath is where dictation keeps its own diagnostics. Wrapped here
// so logging.go owns every path the viewer can reach.
func dictationLogPath() (string, error) {
	return dictation.LogPath()
}
