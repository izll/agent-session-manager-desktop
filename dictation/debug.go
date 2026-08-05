package dictation

import (
	"fmt"
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

// InitLogging initializes the logging system (opens file, but stays in buffer mode)
// If clearLog is true, the log file is cleared on startup, otherwise it appends
func InitLogging(clearLog bool) error {
	configDir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config dir: %w", err)
	}

	logPath := filepath.Join(configDir, "ai-dictate.log")

	// Choose file flags based on clearLog parameter
	var flags int
	if clearLog {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		fmt.Printf("🗑️  Clearing log file\n")
	} else {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	// Open log file
	logFile, err = os.OpenFile(logPath, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
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
				logFile.WriteString(msg)
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
				logFile.WriteString(msg)
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
	if logFile != nil {
		if loggingEnabled {
			timestamp := time.Now().Format("2006-01-02 15:04:05")
			logFile.WriteString(fmt.Sprintf("=== AI Dictate Stopped: %s ===\n\n", timestamp))
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
		logFile.WriteString(logMessage)
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
		logFile.WriteString(logMessage)
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
		logFile.WriteString(logMessage)
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
