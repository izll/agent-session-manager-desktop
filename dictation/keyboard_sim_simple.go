package dictation

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
)

// Global timestamp to indicate when typing last occurred (used by hotkey manager to ignore events)
var (
	lastTypingTimestamp   int64 // Unix timestamp in milliseconds
	typingTimestampMutex  sync.Mutex
	typingCooldownMs      int64 = 150 // Ignore key events for this many ms after typing
)

// IsTypingInProgress returns true if keyboard simulation recently typed
// (within the cooldown period)
func IsTypingInProgress() bool {
	typingTimestampMutex.Lock()
	defer typingTimestampMutex.Unlock()
	now := time.Now().UnixMilli()
	return (now - lastTypingTimestamp) < typingCooldownMs
}

// markTypingStarted marks that typing has started (sets timestamp)
func markTypingStarted() {
	typingTimestampMutex.Lock()
	lastTypingTimestamp = time.Now().UnixMilli()
	typingTimestampMutex.Unlock()
}

// markTypingEnded marks that typing has ended (extends timestamp for cooldown)
func markTypingEnded() {
	typingTimestampMutex.Lock()
	lastTypingTimestamp = time.Now().UnixMilli()
	typingTimestampMutex.Unlock()
}

// PopupTextHandler is a callback for writing text to popup window
type PopupTextHandler interface {
	AppendText(text string)
	DeleteChars(count int)
	GetText() string
	SetText(text string)
}

// KeyboardSimulatorSimple handles keyboard simulation using xdotool/ydotool
type KeyboardSimulatorSimple struct {
	wordHistory   []string   // History of typed words (last 50)
	mu            sync.Mutex // Mutex for thread-safe access
	sessionType   string     // "x11", "wayland", or empty for non-Linux
	isTerminal    bool       // Cache for terminal detection (updated on each delete)
	popupHandler  PopupTextHandler // If set, typing goes to popup instead of xdotool
	popupDirect   bool             // If true, IsPopupMode() returns false but handler stays active (for buffer mode)
}

// NewKeyboardSimulatorSimple creates a new KeyboardSimulatorSimple
func NewKeyboardSimulatorSimple() (*KeyboardSimulatorSimple, error) {
	ks := &KeyboardSimulatorSimple{
		wordHistory: make([]string, 0, 50),
	}

	// On Linux, detect session type and check tools
	if runtime.GOOS == "linux" {
		// Detect X11 vs Wayland
		sessionType := os.Getenv("XDG_SESSION_TYPE")
		ks.sessionType = strings.ToLower(sessionType)

		if ks.sessionType == "wayland" {
			// Wayland: use ydotool
			if hasYdotool() {
				ensureYdotoold()
				fmt.Println("🖥️  Wayland detected, using ydotool")
			} else {
				fmt.Println("⚠️  WARNING: Wayland detected but ydotool not found!")
				fmt.Println("   Keyboard simulation will NOT work on Wayland without ydotool.")
				fmt.Println("   Please install: sudo apt-get install ydotool")
				fmt.Println("   Then start the daemon: ydotoold &")
				return nil, fmt.Errorf("ydotool not found for Wayland. Please install: sudo apt-get install ydotool")
			}
		} else {
			// X11: use xdotool
			_, err := exec.LookPath("xdotool")
			if err != nil {
				return nil, fmt.Errorf("xdotool not found. Please install: sudo apt-get install xdotool")
			}
			debugLog("🖥️  X11 detected, using xdotool\n")
		}
	}
	// On Windows/macOS, robotgo will be used (no check needed)

	return ks, nil
}

// SetPopupHandler sets a popup handler for redirecting keyboard output
// When set, all typing goes to the popup instead of the active window
func (ks *KeyboardSimulatorSimple) SetPopupHandler(handler PopupTextHandler) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.popupHandler = handler
	if handler != nil {
		fmt.Println("🪟 Popup mode enabled - keyboard output redirected to popup")
	} else {
		fmt.Println("🪟 Popup mode disabled - keyboard output goes to active window")
	}
}

// IsPopupMode returns true if popup handler is set and not in direct mode
// In direct mode (buffer), the handler is active but streaming recognizer uses word-by-word path
func (ks *KeyboardSimulatorSimple) IsPopupMode() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.popupHandler != nil && !ks.popupDirect
}

// SetPopupDirect sets direct mode for popup handler
// When true, IsPopupMode() returns false but typing still goes through the handler
func (ks *KeyboardSimulatorSimple) SetPopupDirect(direct bool) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.popupDirect = direct
}

// TypeText types the given text by simulating keyboard events
// Also adds words to history for delete command support
func (ks *KeyboardSimulatorSimple) TypeText(text string) error {
	if text == "" {
		return nil
	}

	// Add words to history (before typing)
	words := strings.Fields(text)
	ks.AddToHistory(words)

	return ks.typeTextInternal(text)
}

// TypeTextNoHistory types text WITHOUT adding to history
// Use this for interim results in streaming mode where words may be corrected
func (ks *KeyboardSimulatorSimple) TypeTextNoHistory(text string) error {
	if text == "" {
		return nil
	}
	return ks.typeTextInternal(text)
}

// hasYdotool checks if ydotool is available
func hasYdotool() bool {
	_, err := exec.LookPath("ydotool")
	return err == nil
}

// ensureYdotoold ensures ydotoold daemon is running, starts it if not
func ensureYdotoold() {
	if !hasYdotool() {
		return
	}

	// Check if ydotoold is already running
	cmd := exec.Command("pgrep", "-x", "ydotoold")
	if err := cmd.Run(); err == nil {
		// Already running
		return
	}

	// Start ydotoold in background
	fmt.Println("🚀 Starting ydotoold daemon...")
	cmd = exec.Command("ydotoold")
	cmd.Start() // Don't wait, run in background

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)
}

// typeTextInternal is the internal implementation of text typing
func (ks *KeyboardSimulatorSimple) typeTextInternal(text string) error {
	// Check if popup mode is enabled
	ks.mu.Lock()
	handler := ks.popupHandler
	ks.mu.Unlock()

	if handler != nil {
		// Popup mode: write directly to popup handler
		logToFile("⌨️  Typing to popup: '%s'\n", text)
		handler.AppendText(text)
		logToFile("✅ popup append completed\n")
		return nil
	}

	// Mark typing started to prevent hotkey manager from processing our simulated keys
	markTypingStarted()
	defer markTypingEnded()

	// Platform-specific typing
	if runtime.GOOS == "linux" {
		logToFile("⌨️  Typing: '%s'\n", text)

		if ks.sessionType == "wayland" {
			// Wayland: use ydotool
			cmd := exec.Command("ydotool", "type", "--key-delay", "0", "--", text)
			output, err := cmd.CombinedOutput()
			if err != nil {
				logToFile("❌ ydotool error: %v, output: %s\n", err, string(output))
				return fmt.Errorf("failed to type text: %w", err)
			}
			logToFile("✅ ydotool type completed\n")
		} else {
			// X11: use xdotool
			cmd := exec.Command("xdotool", "type", "--", text)
			output, err := cmd.CombinedOutput()
			if err != nil {
				logToFile("❌ xdotool error: %v, output: %s\n", err, string(output))
				return fmt.Errorf("failed to type text: %w", err)
			}
			logToFile("✅ xdotool type completed\n")
		}
	} else if runtime.GOOS == "windows" {
		// Use Windows SendInput API for proper Unicode support
		err := typeTextWindows(text)
		if err != nil {
			return fmt.Errorf("failed to type text: %w", err)
		}
	} else {
		// Use robotgo on macOS
		robotgo.TypeStr(text)
	}

	return nil
}

// InsertTextAtCursor inserts text at the current cursor position
func (ks *KeyboardSimulatorSimple) InsertTextAtCursor(text string) error {
	return ks.TypeText(text)
}

// PressBackspace simulates pressing backspace N times
func (ks *KeyboardSimulatorSimple) PressBackspace(count int) error {
	if count <= 0 {
		return nil
	}

	// Check if popup mode is enabled
	ks.mu.Lock()
	handler := ks.popupHandler
	ks.mu.Unlock()

	if handler != nil {
		// Popup mode: delete characters from popup handler
		logToFile("⌫ Deleting %d char(s) from popup\n", count)
		handler.DeleteChars(count)
		logToFile("✅ popup delete completed\n")
		return nil
	}

	// Mark typing started to prevent hotkey manager from processing our simulated keys
	markTypingStarted()
	defer markTypingEnded()

	if runtime.GOOS == "linux" {
		var cmd *exec.Cmd
		if ks.sessionType == "wayland" {
			// Wayland: ydotool
			logToFile("⌫ Pressing %d backspace(s) via ydotool\n", count)
			cmd = exec.Command("ydotool", "key", "--repeat", fmt.Sprintf("%d", count), "--key-delay", "0", "Backspace")
		} else {
			// X11: xdotool
			logToFile("⌫ Pressing %d backspace(s) via xdotool\n", count)
			cmd = exec.Command("xdotool", "key", "--repeat", fmt.Sprintf("%d", count), "--delay", "0", "BackSpace")
		}
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to press backspace: %w", err)
		}
		logToFile("✅ backspace completed\n")
	} else {
		// Windows/macOS: use robotgo
		for i := 0; i < count; i++ {
			robotgo.KeyTap("backspace")
		}
	}
	return nil
}

// PressDelete simulates pressing delete N times
func (ks *KeyboardSimulatorSimple) PressDelete(count int) error {
	if count <= 0 {
		return nil
	}

	// Mark typing started to prevent hotkey manager from processing our simulated keys
	markTypingStarted()
	defer markTypingEnded()

	if runtime.GOOS == "linux" {
		var cmd *exec.Cmd
		if ks.sessionType == "wayland" {
			// Wayland: ydotool
			cmd = exec.Command("ydotool", "key", "--repeat", fmt.Sprintf("%d", count), "--key-delay", "0", "Delete")
		} else {
			// X11: xdotool
			cmd = exec.Command("xdotool", "key", "--repeat", fmt.Sprintf("%d", count), "--delay", "0", "Delete")
		}
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to press delete: %w", err)
		}
	} else {
		// Windows/macOS: use robotgo
		for i := 0; i < count; i++ {
			robotgo.KeyTap("delete")
		}
	}
	return nil
}

// PressEnter simulates pressing enter
func (ks *KeyboardSimulatorSimple) PressEnter() error {
	if runtime.GOOS == "linux" {
		if ks.sessionType == "wayland" {
			cmd := exec.Command("ydotool", "key", "Enter")
			return cmd.Run()
		}
		cmd := exec.Command("xdotool", "key", "Return")
		return cmd.Run()
	}
	robotgo.KeyTap("enter")
	return nil
}

// PressTab simulates pressing tab
func (ks *KeyboardSimulatorSimple) PressTab() error {
	if runtime.GOOS == "linux" {
		if ks.sessionType == "wayland" {
			cmd := exec.Command("ydotool", "key", "Tab")
			return cmd.Run()
		}
		cmd := exec.Command("xdotool", "key", "Tab")
		return cmd.Run()
	}
	robotgo.KeyTap("tab")
	return nil
}

// PressCtrlBackspace simulates pressing Ctrl+Backspace (word delete)
func (ks *KeyboardSimulatorSimple) PressCtrlBackspace() error {
	if runtime.GOOS == "linux" {
		cmd := exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+BackSpace")
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to press Ctrl+Backspace: %w", err)
		}
		return nil
	} else if runtime.GOOS == "windows" {
		// Windows: Ctrl+Backspace
		robotgo.KeyTap("backspace", "ctrl")
		return nil
	}
	// macOS
	robotgo.KeyTap("backspace", "ctrl")
	return nil
}

// PressCtrlAltBackspace simulates pressing Ctrl+Alt+Backspace (line delete)
func (ks *KeyboardSimulatorSimple) PressCtrlAltBackspace() error {
	if runtime.GOOS == "linux" {
		cmd := exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+alt+BackSpace")
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to press Ctrl+Alt+Backspace: %w", err)
		}
		return nil
	} else if runtime.GOOS == "windows" {
		// Windows: Ctrl+Alt+Backspace
		robotgo.KeyTap("backspace", "ctrl", "alt")
		return nil
	}
	// macOS
	robotgo.KeyTap("backspace", "ctrl", "alt")
	return nil
}

// AddToHistory adds words to the typing history (max 50 words)
func (ks *KeyboardSimulatorSimple) AddToHistory(words []string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.wordHistory = append(ks.wordHistory, words...)

	// Keep only last 50 words
	if len(ks.wordHistory) > 50 {
		ks.wordHistory = ks.wordHistory[len(ks.wordHistory)-50:]
	}
}

// isActiveWindowTerminal checks if the currently focused window is a terminal
func (ks *KeyboardSimulatorSimple) isActiveWindowTerminal() bool {
	if runtime.GOOS != "linux" {
		// On Windows/Mac, we don't have easy terminal detection
		// Default to non-terminal behavior (Ctrl+Backspace)
		return false
	}

	// X11 - use xdotool and xprop
	if ks.sessionType == "x11" || ks.sessionType == "" {
		// Get window class using xprop
		cmd := exec.Command("sh", "-c", "xdotool getactivewindow | xargs -I {} xprop -id {} WM_CLASS")
		output, err := cmd.Output()
		if err != nil {
			// If detection fails, assume non-terminal
			return false
		}

		windowClass := strings.ToLower(string(output))

		// Common terminal emulator class names
		terminals := []string{
			"gnome-terminal", "konsole", "xterm", "alacritty",
			"kitty", "tilix", "terminator", "xfce4-terminal",
			"lxterminal", "rxvt", "urxvt", "st", "termite",
			"yakuake", "guake", "tilda", "terminology",
		}

		for _, term := range terminals {
			if strings.Contains(windowClass, term) {
				return true
			}
		}
		return false
	}

	// Wayland - terminal detection is harder, we could check process name
	// For now, default to non-terminal
	return false
}

// deleteLastWordWithBackspaces deletes the last typed word using individual backspace presses
// This is the old/fallback method that works in all applications
func (ks *KeyboardSimulatorSimple) deleteLastWordWithBackspaces(lastWord string) error {
	// Calculate backspace count: word CHARACTER count + space
	// IMPORTANT: Use rune count, not byte count, for correct UTF-8 character handling!
	// Example: "beszél" has 6 characters but 7 bytes (é = 2 bytes)
	characterCount := len([]rune(lastWord))

	// SMART SPACE HANDLING:
	// - If there are MORE words after this one in the history → delete word + space (+1)
	// - If this is the LAST word (history will be empty) → delete ONLY the word (no +1)
	//   because there's no trailing space to delete!
	deleteCount := characterCount
	if len(ks.wordHistory) > 0 {
		// There are more words before this one → delete the trailing space too
		deleteCount = characterCount + 1
		fmt.Printf("🗑️  Deleting last word: '%s' (%d characters + 1 space = %d backspaces, %d words remain)\n",
			lastWord, characterCount, deleteCount, len(ks.wordHistory))
	} else {
		// This was the last word → no trailing space to delete
		fmt.Printf("🗑️  Deleting last word: '%s' (%d characters, no space = %d backspaces, history empty)\n",
			lastWord, characterCount, deleteCount)
	}

	// Check if popup mode is enabled - route through popup handler
	ks.mu.Lock()
	handler := ks.popupHandler
	ks.mu.Unlock()

	if handler != nil {
		logToFile("⌫ Deleting %d char(s) via popup handler (DeleteLastWord)\n", deleteCount)
		handler.DeleteChars(deleteCount)
		return nil
	}

	// Mark typing started to prevent hotkey manager from processing our simulated keys
	markTypingStarted()
	defer markTypingEnded()

	// Press backspace multiple times
	if runtime.GOOS == "linux" {
		var cmd *exec.Cmd
		if ks.sessionType == "wayland" {
			// Wayland: ydotool
			cmd = exec.Command("ydotool", "key", "--repeat", fmt.Sprintf("%d", deleteCount), "--key-delay", "0", "Backspace")
		} else {
			// X11: xdotool
			cmd = exec.Command("xdotool", "key", "--repeat", fmt.Sprintf("%d", deleteCount), "--delay", "0", "BackSpace")
		}
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to press backspace: %w", err)
		}
	} else {
		// Windows/macOS: use robotgo (still needs loop but it's faster)
		for i := 0; i < deleteCount; i++ {
			robotgo.KeyTap("backspace")
		}
	}

	return nil
}

// deleteLastWordWithShortcut deletes the last typed word using keyboard shortcuts
// Uses Ctrl+Backspace on all platforms (works in most GUI apps and modern terminals)
func (ks *KeyboardSimulatorSimple) deleteLastWordWithShortcut(lastWord string) error {
	if lastWord == "" {
		fmt.Println("🗑️  Deleting word (using Ctrl+Backspace)")
	} else {
		fmt.Printf("🗑️  Deleting word: '%s' (using Ctrl+Backspace)\n", lastWord)
	}

	// Platform-specific deletion - always use Ctrl+Backspace
	// This works in:
	// - Most GUI apps (browsers, editors, IDEs)
	// - Modern terminals (gnome-terminal, konsole, alacritty, etc.)
	// Note: May not work in older terminals like xterm
	if runtime.GOOS == "linux" {
		if ks.sessionType == "wayland" {
			// Wayland - use ydotool
			// Key codes: 29=Ctrl, 14=Backspace
			// Format: keycode:1 (press), keycode:0 (release)
			cmd := exec.Command("ydotool", "key", "29:1", "14:1", "14:0", "29:0")
			err := cmd.Run()
			if err != nil {
				return fmt.Errorf("failed to send delete shortcut: %w", err)
			}
		} else {
			// X11 - use xdotool
			// Send Ctrl+Backspace and ensure modifiers are cleared after
			cmd := exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+BackSpace")
			err := cmd.Run()
			if err != nil {
				return fmt.Errorf("failed to send delete shortcut: %w", err)
			}

			// Extra safety: explicitly clear any stuck modifiers
			clearCmd := exec.Command("xdotool", "keyup", "ctrl", "alt", "shift", "super")
			clearCmd.Run() // Ignore errors
		}
	} else if runtime.GOOS == "windows" {
		// Windows - Ctrl+Backspace works in most apps
		robotgo.KeyTap("backspace", "ctrl")
	} else if runtime.GOOS == "darwin" {
		// macOS - Option+Backspace deletes word
		robotgo.KeyTap("backspace", "alt")
	}

	return nil
}

// DeleteLastWord deletes the last typed word from screen AND history
// Returns error if history is empty (caller should check GetHistorySize first)
func (ks *KeyboardSimulatorSimple) DeleteLastWord() error {
	ks.mu.Lock()

	// If no words in history, return error - caller should check history size first
	if len(ks.wordHistory) == 0 {
		ks.mu.Unlock()
		fmt.Println("⚠️ No words in history - nothing to delete")
		return fmt.Errorf("no words in history")
	}

	// Get last word
	lastWord := ks.wordHistory[len(ks.wordHistory)-1]
	ks.wordHistory = ks.wordHistory[:len(ks.wordHistory)-1]
	ks.mu.Unlock()

	// Use backspace method (more reliable than Ctrl+Backspace)
	return ks.deleteLastWordWithBackspaces(lastWord)
}

// GetHistorySize returns the number of words in history
func (ks *KeyboardSimulatorSimple) GetHistorySize() int {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return len(ks.wordHistory)
}

// ClearHistory clears the word history
func (ks *KeyboardSimulatorSimple) ClearHistory() {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.wordHistory = make([]string, 0, 50)
}

// RemoveFromHistory removes N words from history WITHOUT pressing Backspace
// This is useful when we've already deleted from screen and only need to update history
func (ks *KeyboardSimulatorSimple) RemoveFromHistory(count int) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if count <= 0 {
		return
	}

	// Remove up to 'count' words from the end of history
	if count > len(ks.wordHistory) {
		count = len(ks.wordHistory)
	}

	ks.wordHistory = ks.wordHistory[:len(ks.wordHistory)-count]
	fmt.Printf("🗑️  Removed %d word(s) from history (no Backspace), remaining: %d\n", count, len(ks.wordHistory))
}
