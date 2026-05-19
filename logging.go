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
