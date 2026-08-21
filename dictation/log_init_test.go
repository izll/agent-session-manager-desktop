package dictation

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The dictation log was a no-op in every build: InitLogging existed but nobody
// called it, so logFile stayed nil and logToFile discarded every line it was
// handed. Switching logging on in settings changed nothing, and a user chasing
// a failed dictation found a zero-byte file.
//
// Asserts the whole chain rather than the call: opening the file, flushing the
// buffer, and writing afterwards. A test for "InitLogging is called" would pass
// on a call that failed.
func TestLoggingWritesAfterInit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	resetLoggingState(t)

	// Goes through the real constructor rather than calling InitLogging here.
	// The bug was that startup never opened the file at all, so a test that
	// opens it itself passes against the broken code — it proves the logging
	// functions work, which was never in question.
	writeSettings(t, `{"enable_logging": true}`)
	NewAppService()

	logToFile("after settings load\n")
	flushLog(t)

	body := readDictationLog(t)
	for _, want := range []string{"after settings load"} {
		if !strings.Contains(body, want) {
			t.Errorf("log is missing %q; got:\n%s", want, body)
		}
	}
}

// Verbose tracing is opt-in, but a failure is not: it is discovered after the
// fact, when reproducing it with logging switched on is no longer possible.
// Both used to share one switch, so the log was empty in exactly the situation
// someone opened it.
func TestErrorsAreLoggedWithLoggingOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	resetLoggingState(t)

	writeSettings(t, `{"enable_logging": false}`)
	NewAppService()

	logError("later failure\n")
	logToFile("chatty trace line\n")
	flushLog(t)

	body := readDictationLog(t)
	for _, want := range []string{"later failure"} {
		if !strings.Contains(body, want) {
			t.Errorf("error %q was dropped with logging off; got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "chatty trace line") {
		t.Error("verbose line was written even though logging is off")
	}
}

func TestDictationLogIsPrivateAndSizeBounded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	resetLoggingState(t)

	if err := InitLogging(true); err != nil {
		t.Fatalf("InitLogging: %v", err)
	}
	path, err := LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("dictation log mode = %04o, want 0600", got)
		}
	}

	logMutex.Lock()
	for i := 0; i < 30; i++ {
		_, _ = fmt.Fprintf(logFile, "line-%02d-xxxxxxxx\n", i)
	}
	if err := compactDictationLogLocked(logFile, 250, 120); err != nil {
		logMutex.Unlock()
		t.Fatalf("compact dictation log: %v", err)
	}
	_ = logFile.Sync()
	logMutex.Unlock()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 120 || !strings.Contains(string(body), "line-29-xxxxxxxx") {
		t.Fatalf("dictation log was not capped at newest lines: len=%d body=%q", len(body), body)
	}
}

// The package keeps its logging state in globals, so tests must not inherit
// state from each other or from init().
func resetLoggingState(t *testing.T) {
	t.Helper()
	logMutex.Lock()
	if logFile != nil {
		logFile.Close()
	}
	logFile = nil
	logBuffer = nil
	bufferingMode = true
	loggingEnabled = true
	logMutex.Unlock()
	t.Cleanup(func() {
		logMutex.Lock()
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
		logMutex.Unlock()
	})
}

func flushLog(t *testing.T) {
	t.Helper()
	logMutex.Lock()
	defer logMutex.Unlock()
	if logFile != nil {
		logFile.Sync()
	}
}

func readDictationLog(t *testing.T) string {
	t.Helper()
	configDir, err := getConfigDir()
	if err != nil {
		t.Fatalf("getConfigDir: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(configDir, "ai-dictate.log"))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	return string(body)
}

// The settings file the constructor reads on startup.
func writeSettings(t *testing.T, body string) {
	t.Helper()
	configDir, err := getConfigDir()
	if err != nil {
		t.Fatalf("getConfigDir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	path := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing settings: %v", err)
	}
}
