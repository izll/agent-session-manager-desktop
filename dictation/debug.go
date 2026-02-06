package dictation

import (
	"fmt"
	"os"
	"path/filepath"
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
	} else {
		// Discard buffer
		fmt.Printf("🚫 Logging disabled - %d buffered messages discarded\n", len(logBuffer))
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
