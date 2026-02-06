//go:build linux

package dictation

import (
	"fmt"
	"sync"
	"unicode"

	"github.com/bendahl/uinput"
)

// UinputKeyboard manages a virtual keyboard using uinput
type UinputKeyboard struct {
	keyboard uinput.Keyboard
	mu       sync.Mutex
}

var (
	globalUinputKeyboard *UinputKeyboard
	uinputMutex          sync.Mutex
	uinputInitError      error
	uinputInitialized    bool
)

// Character to keycode mapping for US QWERTY layout
// For Hungarian layout, we'll need to handle accented characters differently
var charToKey = map[rune]struct {
	keycode int
	shift   bool
}{
	// Lowercase letters
	'a': {uinput.KeyA, false}, 'b': {uinput.KeyB, false}, 'c': {uinput.KeyC, false},
	'd': {uinput.KeyD, false}, 'e': {uinput.KeyE, false}, 'f': {uinput.KeyF, false},
	'g': {uinput.KeyG, false}, 'h': {uinput.KeyH, false}, 'i': {uinput.KeyI, false},
	'j': {uinput.KeyJ, false}, 'k': {uinput.KeyK, false}, 'l': {uinput.KeyL, false},
	'm': {uinput.KeyM, false}, 'n': {uinput.KeyN, false}, 'o': {uinput.KeyO, false},
	'p': {uinput.KeyP, false}, 'q': {uinput.KeyQ, false}, 'r': {uinput.KeyR, false},
	's': {uinput.KeyS, false}, 't': {uinput.KeyT, false}, 'u': {uinput.KeyU, false},
	'v': {uinput.KeyV, false}, 'w': {uinput.KeyW, false}, 'x': {uinput.KeyX, false},
	'y': {uinput.KeyY, false}, 'z': {uinput.KeyZ, false},

	// Uppercase letters (shift + key)
	'A': {uinput.KeyA, true}, 'B': {uinput.KeyB, true}, 'C': {uinput.KeyC, true},
	'D': {uinput.KeyD, true}, 'E': {uinput.KeyE, true}, 'F': {uinput.KeyF, true},
	'G': {uinput.KeyG, true}, 'H': {uinput.KeyH, true}, 'I': {uinput.KeyI, true},
	'J': {uinput.KeyJ, true}, 'K': {uinput.KeyK, true}, 'L': {uinput.KeyL, true},
	'M': {uinput.KeyM, true}, 'N': {uinput.KeyN, true}, 'O': {uinput.KeyO, true},
	'P': {uinput.KeyP, true}, 'Q': {uinput.KeyQ, true}, 'R': {uinput.KeyR, true},
	'S': {uinput.KeyS, true}, 'T': {uinput.KeyT, true}, 'U': {uinput.KeyU, true},
	'V': {uinput.KeyV, true}, 'W': {uinput.KeyW, true}, 'X': {uinput.KeyX, true},
	'Y': {uinput.KeyY, true}, 'Z': {uinput.KeyZ, true},

	// Numbers
	'1': {uinput.Key1, false}, '2': {uinput.Key2, false}, '3': {uinput.Key3, false},
	'4': {uinput.Key4, false}, '5': {uinput.Key5, false}, '6': {uinput.Key6, false},
	'7': {uinput.Key7, false}, '8': {uinput.Key8, false}, '9': {uinput.Key9, false},
	'0': {uinput.Key0, false},

	// Special characters (US layout - may differ on Hungarian layout)
	' ':  {uinput.KeySpace, false},
	'\n': {uinput.KeyEnter, false},
	'\t': {uinput.KeyTab, false},
	'.':  {uinput.KeyDot, false},
	',':  {uinput.KeyComma, false},
	'-':  {uinput.KeyMinus, false},
	'=':  {uinput.KeyEqual, false},
	'[':  {uinput.KeyLeftbrace, false},
	']':  {uinput.KeyRightbrace, false},
	';':  {uinput.KeySemicolon, false},
	'\'': {uinput.KeyApostrophe, false},
	'`':  {uinput.KeyGrave, false},
	'\\': {uinput.KeyBackslash, false},
	'/':  {uinput.KeySlash, false},

	// Shifted special characters
	'!': {uinput.Key1, true},
	'@': {uinput.Key2, true},
	'#': {uinput.Key3, true},
	'$': {uinput.Key4, true},
	'%': {uinput.Key5, true},
	'^': {uinput.Key6, true},
	'&': {uinput.Key7, true},
	'*': {uinput.Key8, true},
	'(': {uinput.Key9, true},
	')': {uinput.Key0, true},
	'_': {uinput.KeyMinus, true},
	'+': {uinput.KeyEqual, true},
	'{': {uinput.KeyLeftbrace, true},
	'}': {uinput.KeyRightbrace, true},
	':': {uinput.KeySemicolon, true},
	'"': {uinput.KeyApostrophe, true},
	'~': {uinput.KeyGrave, true},
	'|': {uinput.KeyBackslash, true},
	'<': {uinput.KeyComma, true},
	'>': {uinput.KeyDot, true},
	'?': {uinput.KeySlash, true},
}

// InitUinputKeyboard initializes the global uinput keyboard
func InitUinputKeyboard() error {
	uinputMutex.Lock()
	defer uinputMutex.Unlock()

	if uinputInitialized {
		return uinputInitError
	}

	keyboard, err := uinput.CreateKeyboard("/dev/uinput", []byte("ai-dictating-keyboard"))
	if err != nil {
		uinputInitError = fmt.Errorf("failed to create uinput keyboard: %w (try: sudo chmod 666 /dev/uinput)", err)
		uinputInitialized = true
		return uinputInitError
	}

	globalUinputKeyboard = &UinputKeyboard{
		keyboard: keyboard,
	}
	uinputInitialized = true
	fmt.Println("✅ uinput keyboard initialized successfully")
	return nil
}

// CloseUinputKeyboard closes the global uinput keyboard
func CloseUinputKeyboard() {
	uinputMutex.Lock()
	defer uinputMutex.Unlock()

	if globalUinputKeyboard != nil {
		globalUinputKeyboard.keyboard.Close()
		globalUinputKeyboard = nil
	}
	uinputInitialized = false
}

// IsUinputAvailable returns true if uinput keyboard is available
func IsUinputAvailable() bool {
	uinputMutex.Lock()
	defer uinputMutex.Unlock()
	return globalUinputKeyboard != nil
}

// UinputTypeText types text using uinput
// Returns false if the character couldn't be typed (e.g., accented character)
func UinputTypeText(text string) (bool, error) {
	if globalUinputKeyboard == nil {
		return false, fmt.Errorf("uinput keyboard not initialized")
	}

	globalUinputKeyboard.mu.Lock()
	defer globalUinputKeyboard.mu.Unlock()

	for _, r := range text {
		keyInfo, ok := charToKey[r]
		if !ok {
			// Character not in our mapping - can't type it with uinput
			// This includes accented characters like á, é, ő, ű, etc.
			return false, nil
		}

		if keyInfo.shift {
			// Press shift, type key, release shift
			globalUinputKeyboard.keyboard.KeyDown(uinput.KeyLeftshift)
			globalUinputKeyboard.keyboard.KeyPress(keyInfo.keycode)
			globalUinputKeyboard.keyboard.KeyUp(uinput.KeyLeftshift)
		} else {
			globalUinputKeyboard.keyboard.KeyPress(keyInfo.keycode)
		}
	}

	return true, nil
}

// UinputPressBackspace presses backspace N times using uinput
func UinputPressBackspace(count int) error {
	if globalUinputKeyboard == nil {
		return fmt.Errorf("uinput keyboard not initialized")
	}

	globalUinputKeyboard.mu.Lock()
	defer globalUinputKeyboard.mu.Unlock()

	for i := 0; i < count; i++ {
		globalUinputKeyboard.keyboard.KeyPress(uinput.KeyBackspace)
	}

	return nil
}

// UinputPressEnter presses Enter using uinput
func UinputPressEnter() error {
	if globalUinputKeyboard == nil {
		return fmt.Errorf("uinput keyboard not initialized")
	}

	globalUinputKeyboard.mu.Lock()
	defer globalUinputKeyboard.mu.Unlock()

	globalUinputKeyboard.keyboard.KeyPress(uinput.KeyEnter)
	return nil
}

// UinputPressTab presses Tab using uinput
func UinputPressTab() error {
	if globalUinputKeyboard == nil {
		return fmt.Errorf("uinput keyboard not initialized")
	}

	globalUinputKeyboard.mu.Lock()
	defer globalUinputKeyboard.mu.Unlock()

	globalUinputKeyboard.keyboard.KeyPress(uinput.KeyTab)
	return nil
}

// UinputPressDelete presses Delete N times using uinput
func UinputPressDelete(count int) error {
	if globalUinputKeyboard == nil {
		return fmt.Errorf("uinput keyboard not initialized")
	}

	globalUinputKeyboard.mu.Lock()
	defer globalUinputKeyboard.mu.Unlock()

	for i := 0; i < count; i++ {
		globalUinputKeyboard.keyboard.KeyPress(uinput.KeyDelete)
	}

	return nil
}

// HasAccentedChars checks if text contains characters that can't be typed with uinput
func HasAccentedChars(text string) bool {
	for _, r := range text {
		if _, ok := charToKey[r]; !ok {
			// Character not in our mapping
			if !unicode.IsSpace(r) && r != '\n' && r != '\t' {
				return true
			}
		}
	}
	return false
}
