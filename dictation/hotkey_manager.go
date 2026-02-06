package dictation

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	hook "github.com/robotn/gohook"
)

// Global singleton hotkey manager
var (
	globalToggleHotkeyManager *HotkeyManagerReal
	globalHotkeyMutex         sync.Mutex
)

// HotkeyManagerReal manages global hotkeys using gohook
type HotkeyManagerReal struct {
	isEnabled        bool
	callbacks        map[string]func() // Map of hotkey ID to callback
	hotkeyConfigs    map[string]HotkeyConfig
	stopChan         chan bool
	isRunning        bool
	configMutex      sync.RWMutex
	lastTriggerTimes map[string]int64 // Map of hotkey ID to last trigger time for debouncing
	debounceMutex    sync.Mutex
}

// NewHotkeyManagerReal creates a new real hotkey manager (or returns existing one)
// hotkeyID should be unique (e.g., "toggle" or "delete")
func NewHotkeyManagerReal(config HotkeyConfig, callback func(), hotkeyID string) *HotkeyManagerReal {
	globalHotkeyMutex.Lock()
	defer globalHotkeyMutex.Unlock()

	// If a manager already exists, add this hotkey to it
	if globalToggleHotkeyManager != nil {
		globalToggleHotkeyManager.configMutex.Lock()
		globalToggleHotkeyManager.hotkeyConfigs[hotkeyID] = config
		globalToggleHotkeyManager.callbacks[hotkeyID] = callback
		globalToggleHotkeyManager.configMutex.Unlock()
		fmt.Printf("♻️ Added hotkey '%s' to existing manager\n", hotkeyID)
		return globalToggleHotkeyManager
	}

	// Create new manager
	globalToggleHotkeyManager = &HotkeyManagerReal{
		isEnabled:        false,
		callbacks:        make(map[string]func()),
		hotkeyConfigs:    make(map[string]HotkeyConfig),
		lastTriggerTimes: make(map[string]int64),
		stopChan:         make(chan bool, 1),
		isRunning:        false,
	}

	globalToggleHotkeyManager.hotkeyConfigs[hotkeyID] = config
	globalToggleHotkeyManager.callbacks[hotkeyID] = callback

	return globalToggleHotkeyManager
}

// Enable enables the global hotkey
func (hm *HotkeyManagerReal) Enable() error {
	if hm.isRunning {
		fmt.Println("⚠️ Hotkey listener already running")
		return nil
	}

	// Check if we're on Wayland
	sessionType := getSessionType()
	if sessionType == "wayland" {
		fmt.Println("⚠️ WARNING: Running on Wayland")
		fmt.Println("   Global hotkeys may not work reliably on Wayland due to security restrictions.")
		fmt.Println("   If hotkey doesn't work, please use the UI button or switch to X11/XWayland.")
		fmt.Println("   Session type detected:", sessionType)
	} else {
		debugLog("ℹ️  Session type: %s (hotkeys should work)\n", sessionType)
	}

	hm.configMutex.RLock()
	numHotkeys := len(hm.hotkeyConfigs)
	hm.configMutex.RUnlock()

	debugLog("🎹 Enabling global hotkey manager with %d hotkey(s)\n", numHotkeys)

	hm.isEnabled = true
	hm.isRunning = true

	// Start listening in a goroutine
	go hm.listenForHotkey()

	debugLog("✅ Global hotkey enabled successfully\n")
	return nil
}

// getSessionType detects if we're running on X11 or Wayland
func getSessionType() string {
	// Check XDG_SESSION_TYPE environment variable
	sessionType := strings.ToLower(strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE")))
	if sessionType != "" {
		return sessionType
	}

	// Check WAYLAND_DISPLAY
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return "wayland"
	}

	// Check DISPLAY (X11)
	if os.Getenv("DISPLAY") != "" {
		return "x11"
	}

	return "unknown"
}

// Disable disables the global hotkey
func (hm *HotkeyManagerReal) Disable() {
	if !hm.isRunning {
		return
	}

	fmt.Println("Disabling global hotkey...")
	hm.isEnabled = false
	hm.isRunning = false

	// Signal to stop listener goroutine
	select {
	case hm.stopChan <- true:
	default:
	}

	// DON'T call hook.End() - it causes crashes when restarting
	// The listener goroutine will exit cleanly via stopChan
	// hook.End() is not necessary for cleanup
	fmt.Println("✅ Hotkey disabled")
}

// UpdateConfig updates the hotkey configuration for a specific hotkey ID
func (hm *HotkeyManagerReal) UpdateConfig(config HotkeyConfig, hotkeyID string) error {
	hm.configMutex.Lock()
	hm.hotkeyConfigs[hotkeyID] = config
	hm.configMutex.Unlock()

	debugLog("Hotkey '%s' config updated: Ctrl=%v Alt=%v Shift=%v %s\n",
		hotkeyID, config.Ctrl, config.Alt, config.Shift, config.Key)

	// The listener automatically uses the new config
	// since it reads from hm.hotkeyConfigs with mutex protection
	if hm.isRunning {
		fmt.Printf("✅ Hotkey '%s' configuration updated immediately\n", hotkeyID)
	}

	return nil
}

// listenForHotkey listens for the configured hotkey combination
func (hm *HotkeyManagerReal) listenForHotkey() {
	var ctrlPressed, altPressed, shiftPressed bool

	evChan := hook.Start()

	for {
		select {
		case <-hm.stopChan:
			debugLog("Hotkey listener stopped\n")
			return
		case ev := <-evChan:
			// Skip events while typing is in progress (xdotool generates key events)
			if IsTypingInProgress() {
				continue
			}

			if ev.Kind == hook.KeyDown {
				// Track modifier keys based on rawcode
				isCtrl := false
				isAlt := false
				isShift := false

				switch runtime.GOOS {
				case "windows":
					switch ev.Rawcode {
					case 162, 163: // Ctrl
						isCtrl = true
					case 164, 165: // Alt
						isAlt = true
					case 160, 161: // Shift
						isShift = true
					}
				case "darwin":
					switch ev.Rawcode {
					case 162, 163: // Ctrl
						isCtrl = true
					case 164, 165: // Alt
						isAlt = true
					case 160, 161: // Shift
						isShift = true
					}
				default:
					// Linux X11 - check both keysym and keycode
					switch ev.Rawcode {
					case 65507, 65508, 37, 105: // Ctrl (keysym: 65507/65508, keycode: 37/105)
						isCtrl = true
					case 65513, 65514, 64, 108: // Alt (keysym: 65513/65514, keycode: 64/108)
						isAlt = true
					case 65505, 65506, 50, 62: // Shift (keysym: 65505/65506, keycode: 50/62)
						isShift = true
					}
				}

				if isCtrl {
					ctrlPressed = true
					logToFile("🔑 Ctrl pressed\n")
				} else if isAlt {
					altPressed = true
					logToFile("🔑 Alt pressed\n")
				} else if isShift {
					shiftPressed = true
					logToFile("🔑 Shift pressed\n")
				} else {
					// Log the rawcode for debugging
					logToFile("🔑 Key pressed: rawcode=%d, char=%s, modifiers: ctrl=%v alt=%v shift=%v\n",
						ev.Rawcode, hook.RawcodetoKeychar(ev.Rawcode), ctrlPressed, altPressed, shiftPressed)
					// Check all registered hotkeys
					hm.configMutex.RLock()
					configs := make(map[string]HotkeyConfig)
					for id, config := range hm.hotkeyConfigs {
						configs[id] = config
					}
					hm.configMutex.RUnlock()

					// For each hotkey configuration, check if it matches
					for hotkeyID, config := range configs {
						targetKey := strings.ToLower(config.Key)
						needCtrl := config.Ctrl
						needAlt := config.Alt
						needShift := config.Shift

						isTargetKey := false

						if runtime.GOOS == "windows" {
							switch ev.Rawcode {
							case 68: // D
								isTargetKey = (targetKey == "d")
							case 65: // A
								isTargetKey = (targetKey == "a")
							case 83: // S
								isTargetKey = (targetKey == "s")
							case 70: // F
								isTargetKey = (targetKey == "f")
							case 69: // E
								isTargetKey = (targetKey == "e")
							default:
								keyChar := strings.ToLower(hook.RawcodetoKeychar(ev.Rawcode))
								if keyChar != "" && len(keyChar) == 1 {
									isTargetKey = (keyChar == targetKey)
								}
							}
						} else {
							switch ev.Rawcode {
							case 100: // d
								isTargetKey = (targetKey == "d")
							case 97: // a
								isTargetKey = (targetKey == "a")
							case 115: // s
								isTargetKey = (targetKey == "s")
							case 102: // f
								isTargetKey = (targetKey == "f")
							case 101: // e
								isTargetKey = (targetKey == "e")
							case 120: // x
								isTargetKey = (targetKey == "x")
							case 103: // g
								isTargetKey = (targetKey == "g")
							case 65288, 22: // BackSpace (X11 keysym and keycode)
								isTargetKey = (targetKey == "backspace")
							case 32, 65: // Space (keycode 65 is Space on X11, 32 is ASCII)
								isTargetKey = (targetKey == "space")
							default:
								keyChar := strings.ToLower(hook.RawcodetoKeychar(ev.Rawcode))
								if keyChar != "" {
									isTargetKey = (keyChar == targetKey)
								}
							}
						}

						if isTargetKey {
							// Check if modifiers match
							ctrlOk := ctrlPressed == needCtrl
							altOk := altPressed == needAlt
							shiftOk := shiftPressed == needShift

							if ctrlOk && altOk && shiftOk {
								// Debounce check
								hm.debounceMutex.Lock()
								now := time.Now().UnixMilli()
								lastTrigger := hm.lastTriggerTimes[hotkeyID]
								timeSinceLastTrigger := now - lastTrigger
								shouldTrigger := timeSinceLastTrigger > 300

								if shouldTrigger {
									hm.lastTriggerTimes[hotkeyID] = now
									hm.debounceMutex.Unlock()

									logToFile("🎹 Hotkey '%s' triggered!\n", hotkeyID)

									hm.configMutex.RLock()
									callback := hm.callbacks[hotkeyID]
									hm.configMutex.RUnlock()

									if callback != nil {
										callback()
									}
								} else {
									hm.debounceMutex.Unlock()
								}
								break // Don't check other hotkeys once one is triggered
							}
						}
					}
				}
			} else if ev.Kind == hook.KeyUp {
				// Release modifier keys
				isCtrlRelease := false
				isAltRelease := false
				isShiftRelease := false

				switch runtime.GOOS {
				case "windows":
					switch ev.Rawcode {
					case 162, 163: // Ctrl
						isCtrlRelease = true
					case 164, 165: // Alt
						isAltRelease = true
					case 160, 161: // Shift
						isShiftRelease = true
					}
				case "darwin":
					switch ev.Rawcode {
					case 162, 163: // Ctrl
						isCtrlRelease = true
					case 164, 165: // Alt
						isAltRelease = true
					case 160, 161: // Shift
						isShiftRelease = true
					}
				default:
					// Linux X11 - check both keysym and keycode
					switch ev.Rawcode {
					case 65507, 65508, 37, 105: // Ctrl (keysym: 65507/65508, keycode: 37/105)
						isCtrlRelease = true
					case 65513, 65514, 64, 108: // Alt (keysym: 65513/65514, keycode: 64/108)
						isAltRelease = true
					case 65505, 65506, 50, 62: // Shift (keysym: 65505/65506, keycode: 50/62)
						isShiftRelease = true
					}
				}

				if isCtrlRelease {
					ctrlPressed = false
				} else if isAltRelease {
					altPressed = false
				} else if isShiftRelease {
					shiftPressed = false
				}
			}
		}
	}
}
