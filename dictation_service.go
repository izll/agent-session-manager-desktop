package main

import (
	"asmgr-desktop/dictation"
	"encoding/json"
	"fmt"
	"sync"
)

// PtyTextHandler implements dictation.PopupTextHandler
// It writes recognized text directly to the PTY for minimal latency.
// In streaming mode, interim results are shown in a UI overlay instead of the PTY,
// and only final results are written to the PTY (avoiding correction-related over-deletion).
type PtyTextHandler struct {
	mu         sync.Mutex
	sessionID  string
	windowIdx  int
	termServer *TerminalServer
}

func (h *PtyTextHandler) AppendText(text string) {
	h.mu.Lock()
	sid := h.sessionID
	wIdx := h.windowIdx
	ts := h.termServer
	h.mu.Unlock()

	fmt.Printf("[Dictation] PtyTextHandler.AppendText: %q (sid=%s, wIdx=%d)\n", text, sid, wIdx)

	if sid == "" || ts == nil {
		return
	}

	ts.WriteToTerminal(sid, wIdx, text)
}

func (h *PtyTextHandler) DeleteChars(count int) {
	h.mu.Lock()
	sid := h.sessionID
	wIdx := h.windowIdx
	ts := h.termServer
	h.mu.Unlock()

	if sid == "" || ts == nil || count <= 0 {
		return
	}

	ts.SendBackspace(sid, wIdx, count)
}

func (h *PtyTextHandler) GetText() string {
	return ""
}

func (h *PtyTextHandler) SetText(text string) {
	// Not applicable for PTY mode
}

func (h *PtyTextHandler) SetActiveSession(sessionID string, windowIdx int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionID = sessionID
	h.windowIdx = windowIdx
}

// BufferTextHandler implements dictation.PopupTextHandler
// It accumulates recognized text in a buffer for review/edit before sending.
type BufferTextHandler struct {
	mu           sync.Mutex
	text         string
	onTextChange func(string)
}

func (h *BufferTextHandler) AppendText(text string) {
	fmt.Printf("[Buffer] AppendText: %q\n", text)
	h.mu.Lock()
	h.text += text
	t := h.text
	cb := h.onTextChange
	h.mu.Unlock()
	if cb != nil {
		cb(t)
	}
}

func (h *BufferTextHandler) DeleteChars(count int) {
	h.mu.Lock()
	runes := []rune(h.text)
	if count > 0 && len(runes) >= count {
		runes = runes[:len(runes)-count]
		h.text = string(runes)
	}
	t := h.text
	cb := h.onTextChange
	h.mu.Unlock()
	if cb != nil {
		cb(t)
	}
}

func (h *BufferTextHandler) GetText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.text
}

func (h *BufferTextHandler) SetText(text string) {
	h.mu.Lock()
	h.text = text
	cb := h.onTextChange
	h.mu.Unlock()
	if cb != nil {
		cb(text)
	}
}

// FieldTextHandler implements dictation.PopupTextHandler
// It emits Wails events so the frontend can insert text into focused form fields.
type FieldTextHandler struct {
	onAppendText  func(string)
	onDeleteChars func(int)
}

func (h *FieldTextHandler) AppendText(text string) {
	fmt.Printf("[Dictation] FieldTextHandler.AppendText: %q (callback=%v)\n", text, h.onAppendText != nil)
	if h.onAppendText != nil {
		h.onAppendText(text)
	}
}

func (h *FieldTextHandler) DeleteChars(count int) {
	if h.onDeleteChars != nil && count > 0 {
		h.onDeleteChars(count)
	}
}

func (h *FieldTextHandler) GetText() string {
	return ""
}

func (h *FieldTextHandler) SetText(text string) {
	// Not applicable for field mode
}

// DictationService wraps the dictation package for Wails binding
type DictationService struct {
	app               *dictation.AppService
	mu                sync.Mutex
	onStateChange     func(bool)
	onText            func(string)
	onError           func(string, string)
	onVoiceLevel      func(float64)
	onInterimText     func(string)
	onBufferText      func(string)
	initialized       bool
	ptyHandler        *PtyTextHandler
	bufferHandler     *BufferTextHandler
	fieldHandler      *FieldTextHandler
	currentTarget     string // "terminal" or "field"
	currentVoiceLevel float64
}

// DictationSettings represents the settings exposed to the frontend
type DictationSettings struct {
	Enabled                   bool    `json:"enabled"`
	GoogleAPIKey              string  `json:"googleApiKey"`
	Language                  string  `json:"language"`
	Mode                      string  `json:"mode"` // "free", "api", "streaming"
	HotkeyCtrl                bool    `json:"hotkeyCtrl"`
	HotkeyAlt                 bool    `json:"hotkeyAlt"`
	HotkeyShift               bool    `json:"hotkeyShift"`
	HotkeyKey                 string  `json:"hotkeyKey"`
	MuteOutputDuringRecording bool    `json:"muteOutputDuringRecording"`
	AutoStopOnSilence         bool    `json:"autoStopOnSilence"`
	SilenceThreshold          float64 `json:"silenceThreshold"`
	SilenceDuration           float64 `json:"silenceDuration"`
	EnableLogging             bool    `json:"enableLogging"`
	EnableDebugLogging        bool    `json:"enableDebugLogging"`
	InputDevice               string  `json:"inputDevice"`
	BufferMode                bool    `json:"bufferMode"`
	BufferCloseOnSend         bool    `json:"bufferCloseOnSend"`
}

// InputDevice represents an audio input device
type InputDevice struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"isDefault"`
}

// DictationStatus represents the current dictation state
type DictationStatus struct {
	IsListening bool    `json:"isListening"`
	VoiceLevel  float64 `json:"voiceLevel"`
	IsUploading bool    `json:"isUploading"`
}

// NewDictationService creates a new dictation service
func NewDictationService() *DictationService {
	return &DictationService{
		ptyHandler:    &PtyTextHandler{},
		bufferHandler: &BufferTextHandler{},
		fieldHandler:  &FieldTextHandler{},
		currentTarget: "terminal",
	}
}

// Initialize initializes the dictation service
func (d *DictationService) Initialize() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.initialized {
		return nil
	}

	app := dictation.NewAppService()
	d.app = app

	// Set callbacks
	d.app.SetStateChangeCallback(func(listening bool) {
		if d.onStateChange != nil {
			d.onStateChange(listening)
		}
	})

	d.app.SetErrorCallback(func(title, message string) {
		if d.onError != nil {
			d.onError(title, message)
		}
	})

	d.app.SetVoiceLevelCallback(func(level float64) {
		d.mu.Lock()
		d.currentVoiceLevel = level
		d.mu.Unlock()
		if d.onVoiceLevel != nil {
			d.onVoiceLevel(level)
		}
	})

	d.app.SetInterimTextCallback(func(text string) {
		if d.onInterimText != nil {
			d.onInterimText(text)
		}
	})

	// Set buffer handler callback
	d.bufferHandler.onTextChange = func(text string) {
		if d.onBufferText != nil {
			d.onBufferText(text)
		}
	}

	// Apply handler based on buffer mode setting
	settings := d.app.GetSettings()
	d.applyBufferMode(settings.Mode, settings.BufferMode)

	d.initialized = true
	return nil
}

// ToggleDictation toggles the listening state
func (d *DictationService) ToggleDictation() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Auto-initialize if needed
	if d.app == nil {
		app := dictation.NewAppService()
		d.app = app

		// Set callbacks
		d.app.SetStateChangeCallback(func(listening bool) {
			if d.onStateChange != nil {
				d.onStateChange(listening)
			}
		})

		d.app.SetErrorCallback(func(title, message string) {
			if d.onError != nil {
				d.onError(title, message)
			}
		})

		d.app.SetVoiceLevelCallback(func(level float64) {
			if d.onVoiceLevel != nil {
				d.onVoiceLevel(level)
			}
		})

		d.app.SetInterimTextCallback(func(text string) {
			if d.onInterimText != nil {
				d.onInterimText(text)
			}
		})

		// Set buffer handler callback
		d.bufferHandler.onTextChange = func(text string) {
			if d.onBufferText != nil {
				d.onBufferText(text)
			}
		}

		// Apply handler based on current target and buffer mode setting
		fmt.Printf("[Dictation] ToggleDictation auto-init: currentTarget=%q\n", d.currentTarget)
		if d.currentTarget == "field" {
			fmt.Println("[Dictation] ToggleDictation auto-init: setting fieldHandler")
			d.app.SetKeyboardPopupHandler(d.fieldHandler)
			d.app.SetKeyboardPopupDirect(false)
		} else {
			settings := d.app.GetSettings()
			d.applyBufferMode(settings.Mode, settings.BufferMode)
		}

		d.initialized = true
	}

	// Only apply buffer mode if target is terminal (don't override field handler)
	fmt.Printf("[Dictation] ToggleDictation pre-toggle: currentTarget=%q\n", d.currentTarget)
	if d.currentTarget != "field" {
		fmt.Println("[Dictation] ToggleDictation: applying buffer mode (target is not field)")
		settings := d.app.GetSettings()
		d.applyBufferMode(settings.Mode, settings.BufferMode)
	} else {
		fmt.Println("[Dictation] ToggleDictation: SKIPPING applyBufferMode (target is field)")
	}

	// Clear buffer when starting a new recording
	if !d.app.IsListening() {
		d.bufferHandler.SetText("")
	}

	err := d.app.ToggleListening()
	if err != nil {
		return false, err
	}
	return d.app.IsListening(), nil
}

// IsListening returns whether the service is currently listening
func (d *DictationService) IsListening() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.app == nil {
		return false
	}

	return d.app.IsListening()
}

// GetDictationSettings returns the current dictation settings
func (d *DictationService) GetDictationSettings() (*DictationSettings, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Create temp app just to load settings if not initialized
	if d.app == nil {
		tempApp := dictation.NewAppService()
		settings := tempApp.GetSettings()
		return &DictationSettings{
			Enabled:                   settings.Enabled,
			GoogleAPIKey:              settings.GoogleAPIKey,
			Language:                  settings.Language,
			Mode:                      settings.Mode,
			HotkeyCtrl:                settings.HotkeyCtrl,
			HotkeyAlt:                 settings.HotkeyAlt,
			HotkeyShift:               settings.HotkeyShift,
			HotkeyKey:                 settings.HotkeyKey,
			MuteOutputDuringRecording: settings.MuteOutputDuringRecording,
			AutoStopOnSilence:         settings.AutoStopOnSilence,
			SilenceThreshold:          settings.SilenceThreshold,
			SilenceDuration:           settings.SilenceDuration,
			EnableLogging:             settings.EnableLogging,
			EnableDebugLogging:        settings.EnableDebugLogging,
			InputDevice:               settings.PulseAudioSource,
			BufferMode:                settings.BufferMode,
			BufferCloseOnSend:         settings.BufferCloseOnSend,
		}, nil
	}

	settings := d.app.GetSettings()
	return &DictationSettings{
		Enabled:                   settings.Enabled,
		GoogleAPIKey:              settings.GoogleAPIKey,
		Language:                  settings.Language,
		Mode:                      settings.Mode,
		HotkeyCtrl:                settings.HotkeyCtrl,
		HotkeyAlt:                 settings.HotkeyAlt,
		HotkeyShift:               settings.HotkeyShift,
		HotkeyKey:                 settings.HotkeyKey,
		MuteOutputDuringRecording: settings.MuteOutputDuringRecording,
		AutoStopOnSilence:         settings.AutoStopOnSilence,
		SilenceThreshold:          settings.SilenceThreshold,
		SilenceDuration:           settings.SilenceDuration,
		EnableLogging:             settings.EnableLogging,
		EnableDebugLogging:        settings.EnableDebugLogging,
		InputDevice:               settings.PulseAudioSource,
		BufferMode:                settings.BufferMode,
		BufferCloseOnSend:         settings.BufferCloseOnSend,
	}, nil
}

// SetDictationSettings updates the dictation settings
func (d *DictationService) SetDictationSettings(settingsJSON string) error {
	var newSettings DictationSettings
	if err := json.Unmarshal([]byte(settingsJSON), &newSettings); err != nil {
		return fmt.Errorf("failed to parse settings: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.app == nil {
		// Initialize if needed
		d.app = dictation.NewAppService()
		d.initialized = true
	}

	// Get current settings and update
	settings := d.app.GetSettings()
	settings.Enabled = newSettings.Enabled
	settings.GoogleAPIKey = newSettings.GoogleAPIKey
	settings.Language = newSettings.Language
	settings.Mode = newSettings.Mode
	settings.HotkeyCtrl = newSettings.HotkeyCtrl
	settings.HotkeyAlt = newSettings.HotkeyAlt
	settings.HotkeyShift = newSettings.HotkeyShift
	settings.HotkeyKey = newSettings.HotkeyKey
	settings.MuteOutputDuringRecording = newSettings.MuteOutputDuringRecording
	settings.AutoStopOnSilence = newSettings.AutoStopOnSilence
	settings.SilenceThreshold = newSettings.SilenceThreshold
	settings.SilenceDuration = newSettings.SilenceDuration
	settings.EnableLogging = newSettings.EnableLogging
	settings.EnableDebugLogging = newSettings.EnableDebugLogging
	settings.PulseAudioSource = newSettings.InputDevice
	settings.BufferMode = newSettings.BufferMode
	settings.BufferCloseOnSend = newSettings.BufferCloseOnSend

	// Apply logging settings
	dictation.ApplyLoggingSettings(settings.EnableLogging, settings.EnableDebugLogging)

	// Switch handler based on buffer mode
	d.applyBufferMode(settings.Mode, settings.BufferMode)

	// Save settings
	return d.app.SaveSettings(settings)
}

// GetAvailableLanguages returns the list of available languages
func (d *DictationService) GetAvailableLanguages() []map[string]string {
	return []map[string]string{
		{"code": "hu", "name": "Magyar"},
		{"code": "en", "name": "English"},
		{"code": "de", "name": "Deutsch"},
		{"code": "fr", "name": "Français"},
		{"code": "es", "name": "Español"},
		{"code": "it", "name": "Italiano"},
		{"code": "pt", "name": "Português"},
		{"code": "ru", "name": "Русский"},
		{"code": "zh", "name": "中文"},
		{"code": "ja", "name": "日本語"},
		{"code": "ko", "name": "한국어"},
	}
}

// Shutdown cleans up the dictation service
func (d *DictationService) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.app != nil {
		d.app.Shutdown()
		d.app = nil
	}
	d.initialized = false
}

// SetStateChangeCallback sets the callback for state changes
func (d *DictationService) SetStateChangeCallback(callback func(bool)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onStateChange = callback
}

// SetErrorCallback sets the callback for errors
func (d *DictationService) SetErrorCallback(callback func(string, string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onError = callback
}

// SetActiveTmuxSession sets the active session for dictation text output
func (d *DictationService) SetActiveTmuxSession(sessionName string, windowIdx int) {
	d.ptyHandler.SetActiveSession(sessionName, windowIdx)
}

// SetTerminalServer sets the terminal server reference for direct PTY writes
func (d *DictationService) SetTerminalServer(ts *TerminalServer) {
	d.ptyHandler.mu.Lock()
	defer d.ptyHandler.mu.Unlock()
	d.ptyHandler.termServer = ts
}

// GetInputDevices returns the list of available input devices
func (d *DictationService) GetInputDevices() []InputDevice {
	devices := []InputDevice{
		{Name: "", Description: "Default", IsDefault: true},
	}

	// Get PulseAudio devices on Linux
	pulseDevices := dictation.GetPulseAudioInputDevices()
	defaultSource := dictation.GetPulseAudioDefaultSource()

	for _, pd := range pulseDevices {
		devices = append(devices, InputDevice{
			Name:        pd.Name,
			Description: pd.Description,
			IsDefault:   pd.Name == defaultSource,
		})
	}

	return devices
}

// SetInputDevice sets the input device
func (d *DictationService) SetInputDevice(deviceName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.app == nil {
		return fmt.Errorf("dictation service not initialized")
	}

	d.app.SetSelectedPulseSource(deviceName)

	// Update settings
	settings := d.app.GetSettings()
	settings.PulseAudioSource = deviceName
	return d.app.SaveSettings(settings)
}

// AudioTest performs audio test (record and playback)
func (d *DictationService) AudioTest() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.app == nil {
		return fmt.Errorf("dictation service not initialized")
	}

	return d.app.AudioTest()
}

// SetVoiceLevelCallback sets the callback for voice level updates
func (d *DictationService) SetVoiceLevelCallback(callback func(float64)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onVoiceLevel = callback
}

// GetVoiceLevel returns the current voice level (0.0-1.0) for frontend polling
func (d *DictationService) GetVoiceLevel() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentVoiceLevel
}

// SetInterimTextCallback sets the callback for interim text display (streaming overlay)
func (d *DictationService) SetInterimTextCallback(callback func(string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onInterimText = callback
}

// SetBufferTextCallback sets the callback for buffer text updates
func (d *DictationService) SetBufferTextCallback(callback func(string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onBufferText = callback
}

// applyBufferMode switches the popup handler based on mode and bufferMode setting
func (d *DictationService) applyBufferMode(mode string, bufferMode bool) {
	if d.app == nil {
		return
	}
	fmt.Printf("[Buffer] applyBufferMode mode=%s bufferMode=%v\n", mode, bufferMode)
	if mode == "streaming" && bufferMode {
		fmt.Println("[Buffer] Using BufferTextHandler (popup mode - final only)")
		d.app.SetKeyboardPopupHandler(d.bufferHandler)
		d.app.SetKeyboardPopupDirect(false) // popup mode: only FINAL results go to buffer
	} else {
		fmt.Println("[Buffer] Using PtyTextHandler")
		d.app.SetKeyboardPopupHandler(d.ptyHandler)
		d.app.SetKeyboardPopupDirect(false)
	}
}

// SendBufferText sends the buffer text to the terminal and clears the buffer
func (d *DictationService) SendBufferText() error {
	text := d.bufferHandler.GetText()
	if text == "" {
		return nil
	}

	d.ptyHandler.AppendText(text)
	d.bufferHandler.SetText("")
	return nil
}

// ClearBuffer clears the buffer text
func (d *DictationService) ClearBuffer() {
	d.bufferHandler.SetText("")
}

// GetBufferText returns the current buffer text (for frontend polling)
func (d *DictationService) GetBufferText() string {
	return d.bufferHandler.GetText()
}

// SetBufferText updates the buffer text (from frontend edits)
func (d *DictationService) SetBufferText(text string) {
	d.bufferHandler.mu.Lock()
	d.bufferHandler.text = text
	d.bufferHandler.mu.Unlock()
}

// SetDictationTarget switches the active text handler target
// "terminal" → PtyTextHandler or BufferTextHandler (based on settings)
// "field" → FieldTextHandler (emits events for frontend form fields)
func (d *DictationService) SetDictationTarget(target string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fmt.Printf("[Dictation] SetDictationTarget: %q (app=%v)\n", target, d.app != nil)
	d.currentTarget = target

	if d.app == nil {
		fmt.Println("[Dictation] SetDictationTarget: app is nil, only setting currentTarget")
		return
	}

	switch target {
	case "field":
		fmt.Println("[Dictation] SetDictationTarget: setting fieldHandler")
		d.app.SetKeyboardPopupHandler(d.fieldHandler)
		d.app.SetKeyboardPopupDirect(false)
	default:
		fmt.Println("[Dictation] SetDictationTarget: restoring terminal/buffer mode")
		// Restore terminal/buffer mode based on settings
		settings := d.app.GetSettings()
		d.applyBufferMode(settings.Mode, settings.BufferMode)
	}
}

// SetFieldTextCallback sets the callback for field text append events
func (d *DictationService) SetFieldTextCallback(callback func(string)) {
	d.fieldHandler.onAppendText = callback
}

// SetFieldDeleteCallback sets the callback for field text delete events
func (d *DictationService) SetFieldDeleteCallback(callback func(int)) {
	d.fieldHandler.onDeleteChars = callback
}
