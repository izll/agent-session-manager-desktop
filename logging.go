package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// setupLogging wires the standard logger so that:
//   - EVERYTHING goes to a rotating-ish log file
//     (~/.config/agent-session-manager-desktop/asmgr-desktop.log, truncated
//     on each launch so it never grows unbounded), and
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
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)

	// Truncate on launch — we only care about the current run, and an
	// always-appending file would balloon given how chatty the poller is.
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil
	}

	logOut.file = f
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
	file *os.File
}

var logOut = &filteredLogWriter{}

func (w *filteredLogWriter) Write(p []byte) (int, error) {
	// Always persist the full line to the file.
	if w.file != nil {
		_, _ = w.file.Write(p)
	}

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
// The log is chatty by design — the sidebar poll writes on every tick — so a
// long-running session's file reaches megabytes. Everything after a problem is
// what explains it, so the tail is what gets read; sending the whole file would
// mostly ship poll noise from hours ago.
const maxLogLines = 2000

// ReadAppLog returns the end of the current run's log.
func ReadAppLog() AppLog {
	path := defaultLogPath()
	result := AppLog{Path: path}
	if path == "" {
		result.Missing = true
		return result
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// A missing file is the ordinary case before anything has been logged,
		// not an error worth showing as one.
		result.Missing = true
		return result
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// A file holding only a trailing newline splits to one empty string.
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
		result.Truncated = true
	}
	result.Lines = lines
	return result
}
