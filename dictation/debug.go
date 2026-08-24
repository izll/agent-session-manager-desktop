package dictation

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// debugMode controls whether debug messages are printed to console
var debugMode bool

// loggingEnabled controls whether logging to file is enabled
var loggingEnabled bool = true // Default: enabled until settings load

// logFile is the global log file
var logFile *os.File
var logMutex sync.Mutex

// logBuffer stores log messages until settings are loaded
var logBuffer []string
var bufferingMode bool = true // Start in buffering mode

const (
	dictationMaxLogBytes      = int64(16 << 20)
	dictationRetainedLogBytes = int64(8 << 20)
)

// InitLogging initializes the logging system (opens file, but stays in buffer mode)
// If clearLog is true, the log file is cleared on startup, otherwise it appends
func InitLogging(clearLog bool) error {
	configDir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config dir: %w", err)
	}

	logPath := filepath.Join(configDir, "ai-dictate.log")
	if err := os.Chmod(configDir, 0700); err != nil {
		return fmt.Errorf("failed to secure config directory: %w", err)
	}

	if clearLog {
		fmt.Printf("🗑️  Clearing log file\n")
	}

	// O_APPEND plus a filesystem lock lets multiple GUI instances keep one
	// diagnostic log without overwriting each other's independent offsets.
	opened, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	if err := opened.Chmod(0600); err != nil {
		_ = opened.Close()
		return fmt.Errorf("failed to secure log file: %w", err)
	}
	if clearLog {
		// Truncated through a separate handle, not the append-mode one.
		//
		// Windows refuses Truncate on a file opened with O_APPEND — "Access is
		// denied" — because append mode grants only the right to add. Unix
		// allows it, which is why this went unnoticed until the suite ran there.
		// Opening a second handle for the one operation keeps the append handle
		// doing what it is for.
		if err := withDictationLogLock(configDir, func() error {
			truncator, openErr := os.OpenFile(logPath, os.O_RDWR, 0600)
			if openErr != nil {
				return openErr
			}
			defer truncator.Close()
			return truncator.Truncate(0)
		}); err != nil {
			_ = opened.Close()
			return fmt.Errorf("failed to clear log file: %w", err)
		}
	}

	// Add startup marker to buffer (will be written when settings load)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	var startupMsg string
	if clearLog {
		startupMsg = fmt.Sprintf("=== AI Dictate Started: %s (log cleared) ===\n", timestamp)
	} else {
		startupMsg = fmt.Sprintf("\n=== AI Dictate Started: %s ===\n", timestamp)
	}

	logMutex.Lock()
	if logFile != nil {
		_ = logFile.Close()
	}
	logFile = opened
	logBuffer = append(logBuffer, startupMsg)
	logMutex.Unlock()

	fmt.Printf("📝 Logging initialized (buffering until settings load): %s\n", logPath)
	return nil
}

// ApplyLoggingSettings applies logging settings after they are loaded
// If logging is enabled, flush buffer to file. If disabled, discard buffer.
func ApplyLoggingSettings(enableLogging, enableDebug bool) {
	logMutex.Lock()
	defer logMutex.Unlock()

	loggingEnabled = enableLogging
	debugMode = enableDebug

	bufferingMode = false // Exit buffering mode

	if enableLogging {
		// Flush buffer to file
		if logFile != nil {
			for _, msg := range logBuffer {
				writeDictationLogLocked(msg)
			}
			fmt.Printf("✅ Logging enabled - %d buffered messages written to file\n", len(logBuffer))
		}
	} else if logFile != nil {
		// Verbose lines buffered before the settings loaded are dropped, but
		// errors are not: they are the reason someone opens this file, and
		// discarding them because tracing is off is how a startup failure
		// becomes invisible.
		kept := 0
		for _, msg := range logBuffer {
			if strings.Contains(msg, "[ERROR]") {
				writeDictationLogLocked(msg)
				kept++
			}
		}
		fmt.Printf("🚫 Verbose logging disabled - %d buffered messages dropped, %d errors kept\n",
			len(logBuffer)-kept, kept)
	}

	// Clear buffer
	logBuffer = nil
}

// CloseLogging closes the log file
func CloseLogging() {
	logMutex.Lock()
	defer logMutex.Unlock()
	if logFile != nil {
		if loggingEnabled {
			timestamp := time.Now().Format("2006-01-02 15:04:05")
			writeDictationLogLocked(fmt.Sprintf("=== AI Dictate Stopped: %s ===\n\n", timestamp))
		}
		logFile.Close()
		logFile = nil
	}
}

// logToFile writes a message to log file only (or buffer if in buffering mode)
func logToFile(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")
	logMessage := fmt.Sprintf("[%s] %s", timestamp, message)

	logMutex.Lock()
	defer logMutex.Unlock()

	if bufferingMode {
		// Still buffering - add to buffer
		logBuffer = append(logBuffer, logMessage)
	} else if loggingEnabled && logFile != nil {
		// Logging enabled - write to file
		writeDictationLogLocked(logMessage)
	}
	// If logging disabled and not buffering - do nothing (discard)
}

// logError records a failure, whatever the logging setting says.
//
// Verbose tracing is opt-in because it is noise most of the time. A failure is
// not: it is the one thing worth having a record of, and it is discovered
// after the fact, when turning logging on and reproducing it is no longer
// possible. Tying both to the same switch meant the log was empty in exactly
// the situation someone opened it — a dictation that silently transcribed
// nothing left no trace of why.
//
// Errors are prefixed so they can be picked out of a long log at a glance.
func logError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")
	logMessage := fmt.Sprintf("[%s] [ERROR] %s", timestamp, message)

	logMutex.Lock()
	defer logMutex.Unlock()

	if bufferingMode {
		logBuffer = append(logBuffer, logMessage)
	} else if logFile != nil {
		writeDictationLogLocked(logMessage)
	}
}

// debugLog writes a debug message to log file only (only if debug mode enabled)
func debugLog(format string, args ...interface{}) {
	if !debugMode {
		return
	}

	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")
	logMessage := fmt.Sprintf("[%s] [DEBUG] %s", timestamp, message)

	logMutex.Lock()
	defer logMutex.Unlock()

	// Write to file or buffer (NO console output)
	if bufferingMode {
		// Still buffering - add to buffer
		logBuffer = append(logBuffer, logMessage)
	} else if loggingEnabled && logFile != nil {
		// Logging enabled - write to file
		writeDictationLogLocked(logMessage)
	}
}

// SetDebugMode enables or disables debug logging to console
func SetDebugMode(enabled bool) {
	debugMode = enabled
}

// LogPath is where dictation writes its own diagnostics.
//
// Separate from the application log because dictation predates it and keeps its
// own format; exposed so the log viewer can offer both. Its content is what
// answers "the microphone recorded nothing" — the application log carries none
// of it.
func LogPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "ai-dictate.log"), nil
}

// ClearLog truncates the dictation log under the same process and filesystem
// locks used by writers. Truncating it by path from the outer application used
// to race another app instance and could discard a concurrent diagnostic.
func ClearLog() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(configDir, "ai-dictate.log")
	logMutex.Lock()
	defer logMutex.Unlock()
	return withDictationLogLock(configDir, func() error {
		if logFile != nil {
			return logFile.Truncate(0)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		if err := file.Chmod(0600); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
}

// Caller holds logMutex.
func writeDictationLogLocked(message string) {
	if logFile == nil {
		return
	}
	configDir := filepath.Dir(logFile.Name())
	_ = withDictationLogLock(configDir, func() error {
		if _, err := logFile.WriteString(message); err != nil {
			return err
		}
		return compactDictationLogLocked(logFile, dictationMaxLogBytes, dictationRetainedLogBytes)
	})
}

func withDictationLogLock(configDir string, action func() error) error {
	lock, err := os.OpenFile(filepath.Join(configDir, ".log.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	_ = lock.Chmod(0600)
	if err := lockConfigFile(lock); err != nil {
		return err
	}
	defer unlockConfigFile(lock)
	return action()
}

func compactDictationLogLocked(file *os.File, maximum, retain int64) error {
	info, err := file.Stat()
	if err != nil || info.Size() <= maximum {
		return err
	}
	if retain <= 0 || retain >= maximum {
		return fmt.Errorf("invalid dictation log retention limits")
	}
	start := info.Size() - retain
	buf := make([]byte, retain)
	n, err := file.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return err
	}
	buf = buf[:n]
	if newline := bytes.IndexByte(buf, '\n'); newline >= 0 {
		buf = buf[newline+1:]
	} else {
		buf = nil
	}
	// Not file.Truncate: the handle is opened O_APPEND, and Windows grants an
	// append handle the right to add to the file, not to shorten it — Truncate
	// there fails with "Access is denied". A separate O_RDWR handle can.
	truncator, err := os.OpenFile(file.Name(), os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err := truncator.Truncate(0); err != nil {
		truncator.Close()
		return err
	}
	if err := truncator.Close(); err != nil {
		return err
	}
	if len(buf) != 0 {
		_, err = file.Write(buf)
	}
	return err
}
