package dictation

import ()

// HotkeyConfig represents hotkey configuration
type HotkeyConfig struct {
	Ctrl  bool
	Alt   bool
	Shift bool
	Key   string
}

// HotkeyManagerSimple is a placeholder for hotkey management
// Global hotkeys on Linux require complex X11 integration
// For now, users can toggle via the UI button
type HotkeyManagerSimple struct {
	isEnabled    bool
	callback     func()
	hotkeyConfig HotkeyConfig
}

// NewHotkeyManagerSimple creates a new HotkeyManagerSimple
func NewHotkeyManagerSimple(config HotkeyConfig, callback func()) *HotkeyManagerSimple {
	logToFile("Note: Global hotkeys not yet implemented. Use the UI button to toggle." + "\n")
	return &HotkeyManagerSimple{
		isEnabled:    false,
		callback:     callback,
		hotkeyConfig: config,
	}
}

// Enable enables the global hotkey (placeholder)
func (hm *HotkeyManagerSimple) Enable() error {
	logToFile("Global hotkey support will be added in a future update." + "\n")
	logToFile("Configured hotkey: Ctrl=%v Alt=%v Shift=%v %s\n",
		hm.hotkeyConfig.Ctrl, hm.hotkeyConfig.Alt, hm.hotkeyConfig.Shift, hm.hotkeyConfig.Key)
	hm.isEnabled = true
	return nil
}

// Disable disables the global hotkey (placeholder)
func (hm *HotkeyManagerSimple) Disable() {
	hm.isEnabled = false
}

// UpdateConfig updates the hotkey configuration (placeholder)
func (hm *HotkeyManagerSimple) UpdateConfig(config HotkeyConfig) error {
	hm.hotkeyConfig = config
	logToFile("Hotkey config updated: Ctrl=%v Alt=%v Shift=%v %s\n",
		config.Ctrl, config.Alt, config.Shift, config.Key)
	return nil
}
