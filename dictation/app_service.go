package dictation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Package-level variables for voice level monitoring (used by UI)
var (
	lastVoiceLevel  float64
	voiceLevelMutex sync.Mutex
)

// AppService handles the application logic
type AppService struct {
	settings         *Settings
	isListening      bool
	speechRecognizer *SpeechRecognizer
	audioCapture     *AudioCapture
	keyboard         *KeyboardSimulatorSimple
	hotkeyManager    *HotkeyManagerReal
	audioMuteManager *AudioMuteManager
	mu               sync.Mutex
	onStateChange    func(bool)                  // Callback for UI updates
	onError          func(title, message string) // Callback for error dialogs
	onUploading      func(bool)                  // Callback for uploading state (free/api mode)
	onPopupDictate   func()                      // Callback for popup dictation hotkey
	onVoiceLevel     func(float64)               // Callback for voice level updates
	onInterimText    func(string)                // Callback for interim text display (streaming mode overlay)
	initErrors       []string                    // Initialization errors to show after UI is ready

	// Auto-stop on silence
	lastSpeechTime     int64 // Unix timestamp in seconds
	silenceCheckActive bool
	silenceMonitorDone chan bool

	// PulseAudio source management (Linux)
	originalPulseSource  string // Original PulseAudio source before recording
	selectedPulseSource  string // User-selected PulseAudio source for recording
}

// Settings represents application settings
type Settings struct {
	Enabled                   bool    `json:"enabled"` // Whether dictation is enabled
	HotkeyCtrl                bool    `json:"hotkey_ctrl"`
	HotkeyAlt                 bool    `json:"hotkey_alt"`
	HotkeyShift               bool    `json:"hotkey_shift"`
	HotkeyKey                 string  `json:"hotkey_key"`
	GoogleAPIKey              string  `json:"google_api_key"`
	Language                  string  `json:"language"`
	Mode                      string  `json:"mode"` // "free", "api", or "streaming"
	MuteOutputDuringRecording bool    `json:"mute_output_during_recording"`
	AutoStopOnSilence         bool    `json:"auto_stop_on_silence"`
	SilenceTimeoutSeconds     int     `json:"silence_timeout_seconds"`
	SilenceThreshold          float64 `json:"silence_threshold"`    // 0-100%, noise level threshold percentage
	SilenceDuration           float64 `json:"silence_duration"`     // 0.1-2.0, seconds of silence required
	CountFreeTier             bool    `json:"count_free_tier"`      // Whether to count 60 min free tier in stats
	EnableLogging             bool    `json:"enable_logging"`       // Whether to enable file logging
	EnableDebugLogging        bool    `json:"enable_debug_logging"` // Whether to enable debug logging (console output)
	InputDeviceIndex          int     `json:"input_device_index"`   // Audio input device index (-1 for default)
	PulseAudioSource          string  `json:"pulse_audio_source"`   // PulseAudio source name (Linux only)
	// Instant send hotkey (for free/api mode - forces immediate audio processing)
	InstantSendCtrl  bool   `json:"instant_send_ctrl"`
	InstantSendAlt   bool   `json:"instant_send_alt"`
	InstantSendShift bool   `json:"instant_send_shift"`
	InstantSendKey   string `json:"instant_send_key"`
	// Recording mode: "popup" (window-based) or "direct" (type directly to active window)
	RecordingMode string `json:"recording_mode"`
	BufferMode         bool `json:"buffer_mode"`          // Show text in editable buffer before sending (streaming mode, default: true)
	BufferCloseOnSend  bool `json:"buffer_close_on_send"` // Close buffer window after sending text (default: true)
}

// UsageStats represents API usage statistics
type UsageStats struct {
	TotalRequests     int     `json:"total_requests"`
	TotalAudioSeconds float64 `json:"total_audio_seconds"`
}

// Translation represents a translation map
type Translation map[string]map[string]string

// PunctuationCommands represents punctuation command mappings
type PunctuationCommands map[string]map[string]string

// SpeechContext represents speech context phrases
type SpeechContext map[string][]string

// DeleteCommands represents delete command mappings (command -> action type)
type DeleteCommands map[string]map[string]string

// NewAppService creates a new AppService
func NewAppService() *AppService {
	app := &AppService{
		settings: &Settings{
			HotkeyCtrl:                true,
			HotkeyAlt:                 true,
			HotkeyShift:               false,
			HotkeyKey:                 "d",
			Language:                  "en",
			Mode:                      "free",
			MuteOutputDuringRecording: true,
			AutoStopOnSilence:         true,
			SilenceTimeoutSeconds:     60,
			SilenceThreshold:          30,     // Default noise threshold (30% = ~0.003 actual value)
			SilenceDuration:           0.5,    // Default: 0.5 seconds of silence needed
			CountFreeTier:             true,   // Count 60 min free tier by default
			EnableLogging:             false,  // Disable logging by default
			EnableDebugLogging:        false,  // Disable debug logging by default
			InputDeviceIndex:          -1,     // Use default input device
			// Default instant send hotkey: Alt+S
			InstantSendCtrl:  false,
			InstantSendAlt:   true,
			InstantSendShift: false,
			InstantSendKey:   "s",
			// Default recording mode: popup (window-based)
			RecordingMode: "popup",
			BufferMode:         true,
			BufferCloseOnSend:  true,
		},
		isListening: false,
	}

	// Copy default config files to home directory (first run)
	copyDefaultConfigFiles()

	// Load settings from file
	app.LoadSettings()

	// Apply logging settings (this flushes or discards the log buffer)
	ApplyLoggingSettings(app.settings.EnableLogging, app.settings.EnableDebugLogging)

	// Initialize audio capture
	app.audioCapture = NewAudioCapture()
	err := app.audioCapture.Initialize()
	if err != nil {
		fmt.Printf("Warning: Failed to initialize audio: %v\n", err)
	}

	// Set voice level callback for taskbar icon pulsing
	app.audioCapture.SetVoiceLevelCallback(func(level float64) {
		voiceLevelMutex.Lock()
		lastVoiceLevel = level
		voiceLevelMutex.Unlock()
		// Notify UI if callback is set
		if app.onVoiceLevel != nil {
			app.onVoiceLevel(level)
		}
	})

	// Set initial silence threshold from settings (convert percentage to actual value)
	thresholdPercent := app.settings.SilenceThreshold
	actualThreshold := 0.0001 + (thresholdPercent / 100.0 * 0.0099)
	app.audioCapture.SetSilenceThreshold(actualThreshold)

	// Set input device from settings
	if app.settings.InputDeviceIndex != -1 {
		err = app.audioCapture.SetInputDevice(app.settings.InputDeviceIndex)
		if err != nil {
			fmt.Printf("Warning: Failed to set input device: %v (using default)\n", err)
			app.settings.InputDeviceIndex = -1 // Reset to default on error
		}
	}

	// Load saved PulseAudio source from settings (Linux)
	if app.settings.PulseAudioSource != "" {
		// Verify the source still exists
		pulseDevices := GetPulseAudioInputDevices()
		sourceExists := false
		for _, device := range pulseDevices {
			if device.Name == app.settings.PulseAudioSource {
				sourceExists = true
				break
			}
		}
		if sourceExists {
			app.selectedPulseSource = app.settings.PulseAudioSource
			debugLog("🎤 Loaded saved PulseAudio source: %s\n", app.selectedPulseSource)
		} else {
			debugLog("⚠️ Saved PulseAudio source '%s' no longer exists, using default\n", app.settings.PulseAudioSource)
			app.settings.PulseAudioSource = ""
		}
	}

	// Initialize keyboard simulator
	app.keyboard, err = NewKeyboardSimulatorSimple()
	if err != nil {
		fmt.Printf("Warning: Failed to initialize keyboard simulator: %v\n", err)
		// Store error for UI notification - xdotool is required for text input on Linux X11
		app.initErrors = append(app.initErrors, fmt.Sprintf("xdotool not found.\n\nThe application cannot type text without xdotool.\n\nPlease install it:\nsudo apt-get install xdotool"))
	}

	// Initialize audio mute manager
	app.audioMuteManager = NewAudioMuteManager()

	// Initialize hotkey manager with toggle callback
	hotkeyConfig := HotkeyConfig{
		Ctrl:  app.settings.HotkeyCtrl,
		Alt:   app.settings.HotkeyAlt,
		Shift: app.settings.HotkeyShift,
		Key:   app.settings.HotkeyKey,
	}
	app.hotkeyManager = NewHotkeyManagerReal(hotkeyConfig, func() {
		go func() {
			// Check recording mode
			if app.settings.RecordingMode == "popup" && app.onPopupDictate != nil {
				// Popup mode: delegate to popup handler
				app.onPopupDictate()
			} else {
				// Direct mode: toggle listening directly
				err := app.ToggleListening()
				if err != nil {
					fmt.Printf("Error toggling from hotkey: %v\n", err)
				}
			}
		}()
	}, "toggle")

	// Add instant send hotkey - forces immediate audio processing (for free/api mode)
	// Uses settings from loaded config, defaults to Alt+S
	sendHotkeyConfig := HotkeyConfig{
		Ctrl:  app.settings.InstantSendCtrl,
		Alt:   app.settings.InstantSendAlt,
		Shift: app.settings.InstantSendShift,
		Key:   app.settings.InstantSendKey,
	}
	// Set default if key is empty (first run or old config)
	if sendHotkeyConfig.Key == "" {
		sendHotkeyConfig.Alt = true
		sendHotkeyConfig.Key = "s"
	}
	NewHotkeyManagerReal(sendHotkeyConfig, func() {
		go func() {
			app.ForceProcessAudio()
		}()
	}, "send")

	// Enable hotkey
	err = app.hotkeyManager.Enable()
	if err != nil {
		fmt.Printf("Warning: Failed to enable hotkey: %v\n", err)
	}

	return app
}

// SetStateChangeCallback sets the callback for state changes
func (a *AppService) SetStateChangeCallback(callback func(bool)) {
	a.onStateChange = callback
}

// SetErrorCallback sets the callback for error dialogs
func (a *AppService) SetErrorCallback(callback func(title, message string)) {
	a.onError = callback
}

// SetUploadingCallback sets the callback for uploading state changes
func (a *AppService) SetUploadingCallback(callback func(bool)) {
	a.onUploading = callback
}

// SetPopupDictateCallback sets the callback for popup dictation hotkey
func (a *AppService) SetPopupDictateCallback(callback func()) {
	a.onPopupDictate = callback
}

// SetVoiceLevelCallback sets the callback for voice level updates
func (a *AppService) SetVoiceLevelCallback(callback func(float64)) {
	a.onVoiceLevel = callback
}

// SetInterimTextCallback sets the callback for interim text display (streaming mode overlay)
func (a *AppService) SetInterimTextCallback(callback func(string)) {
	a.onInterimText = callback
}

// NotifyInterimText notifies UI about interim recognized text
func (a *AppService) NotifyInterimText(text string) {
	if a.onInterimText != nil {
		a.onInterimText(text)
	}
}

// SetKeyboardPopupHandler sets a popup handler on the keyboard simulator
// This redirects all typed text to the handler instead of xdotool
func (a *AppService) SetKeyboardPopupHandler(handler PopupTextHandler) {
	if a.keyboard != nil {
		a.keyboard.SetPopupHandler(handler)
	}
}

// SetKeyboardPopupDirect sets direct mode for popup handler (buffer mode)
func (a *AppService) SetKeyboardPopupDirect(direct bool) {
	if a.keyboard != nil {
		a.keyboard.SetPopupDirect(direct)
	}
}

// NotifyUploading notifies UI about uploading state
func (a *AppService) NotifyUploading(isUploading bool) {
	if a.onUploading != nil {
		a.onUploading(isUploading)
	}
}

// getConfigDir returns the configuration directory path (cross-platform)
func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".config", "ai-dictate")

	// Create directory if it doesn't exist
	err = os.MkdirAll(configDir, 0755)
	if err != nil {
		return "", err
	}

	return configDir, nil
}

// getConfigPath returns the path for a config file (settings.json, etc.)
func getConfigPath(filename string) (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, filename), nil
}

// LoadSettings loads settings from file
func (a *AppService) LoadSettings() error {
	settingsPath, err := getConfigPath("settings.json")
	if err != nil {
		fmt.Printf("⚠️ Warning: Failed to get config path: %v\n", err)
		return nil // Use default settings
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Use default settings
		}
		return err
	}

	if err := json.Unmarshal(data, a.settings); err != nil {
		return err
	}

	// If buffer_mode key is missing from saved config, default to true
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		if _, exists := raw["buffer_mode"]; !exists {
			a.settings.BufferMode = true
		}
		if _, exists := raw["buffer_close_on_send"]; !exists {
			a.settings.BufferCloseOnSend = true
		}
	}

	return nil
}

// SaveSettings saves settings to file
func (a *AppService) SaveSettings(settings Settings) error {
	a.mu.Lock()
	oldMode := a.settings.Mode  // Save BEFORE updating settings
	wasListening := a.isListening
	newMode := settings.Mode    // Get new mode from incoming settings
	a.settings = &settings
	a.mu.Unlock()

	settingsPath, err := getConfigPath("settings.json")
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(settingsPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write settings: %w", err)
	}

	fmt.Printf("✅ Settings saved to: %s\n", settingsPath)

	// If mode changed while recording, restart recording with new mode
	if wasListening && oldMode != newMode {
		fmt.Printf("🔄 Mode changed from '%s' to '%s' while recording - restarting...\n", oldMode, newMode)
		// Run restart in goroutine to avoid blocking UI
		go func() {
			// Stop current recording
			a.ToggleListening()
			// Wait for clean stop - StopRecording can take up to 1 second
			time.Sleep(800 * time.Millisecond)
			// Start new recording with new mode
			a.ToggleListening()
		}()
	}

	return nil
}

// GetSettings returns current settings
func (a *AppService) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.settings
}

// copyDefaultConfigFiles copies default JSON files to config directory if they don't exist
func copyDefaultConfigFiles() error {
	// Note: languages.json is NOT copied - it stays in program directory (read-only)
	defaultFiles := []string{
		"punctuation_commands.json",
		"speech_context.json",
		"delete_commands.json",
	}

	for _, filename := range defaultFiles {
		// Check if file exists in config dir
		configPath, err := getConfigPath(filename)
		if err != nil {
			continue
		}

		// If file already exists, skip
		if _, err := os.Stat(configPath); err == nil {
			continue
		}

		// Read from current directory (bundled with app)
		sourceData, err := os.ReadFile(filename)
		if err != nil {
			// File doesn't exist in app directory, skip
			debugLog("⚠️ Default file not found: %s (will create on save)\n", filename)
			continue
		}

		// Write to config directory
		err = os.WriteFile(configPath, sourceData, 0644)
		if err != nil {
			debugLog("⚠️ Failed to copy %s: %v\n", filename, err)
			continue
		}

		debugLog("📋 Copied default config: %s → %s\n", filename, configPath)
	}

	return nil
}

// LoadTranslations loads translations from languages.json (from program directory only)
func (a *AppService) LoadTranslations() (Translation, error) {
	// languages.json stays in program directory (read-only)
	translationsPath := filepath.Join(".", "languages.json")

	data, err := os.ReadFile(translationsPath)
	if err != nil {
		return nil, err
	}

	var translations Translation
	err = json.Unmarshal(data, &translations)
	return translations, err
}

// LoadPunctuationCommands loads punctuation commands from file
func (a *AppService) LoadPunctuationCommands() (PunctuationCommands, error) {
	// Try config directory first
	configPath, err := getConfigPath("punctuation_commands.json")
	var punctuationPath string
	if err == nil {
		punctuationPath = configPath
	} else {
		// Fallback to current directory
		punctuationPath = filepath.Join(".", "punctuation_commands.json")
	}

	data, err := os.ReadFile(punctuationPath)
	if err != nil {
		return PunctuationCommands{}, nil // Return empty if not found
	}

	var commands PunctuationCommands
	err = json.Unmarshal(data, &commands)
	return commands, err
}

// SavePunctuationCommands saves punctuation commands to file
func (a *AppService) SavePunctuationCommands(commands PunctuationCommands) error {
	punctuationPath, err := getConfigPath("punctuation_commands.json")
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(commands, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(punctuationPath, data, 0644)
}

// LoadSpeechContext loads speech context phrases from file
func (a *AppService) LoadSpeechContext() (SpeechContext, error) {
	contextPath, err := getConfigPath("speech_context.json")
	if err != nil {
		return SpeechContext{}, nil
	}

	data, err := os.ReadFile(contextPath)
	if err != nil {
		return SpeechContext{}, nil // Return empty if not found
	}

	var context SpeechContext
	err = json.Unmarshal(data, &context)
	return context, err
}

// SaveSpeechContext saves speech context phrases to file
func (a *AppService) SaveSpeechContext(context SpeechContext) error {
	contextPath, err := getConfigPath("speech_context.json")
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(context, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(contextPath, data, 0644)
}

// LoadDeleteCommands loads delete commands from file
func (a *AppService) LoadDeleteCommands() (DeleteCommands, error) {
	deleteCommandsPath, err := getConfigPath("delete_commands.json")
	if err != nil {
		return DeleteCommands{
			"en": {
				"szusi":  "buffer",
				"sushi":  "buffer",
				"vegeta": "ctrl_backspace",
				"goku":   "ctrl_alt_backspace",
			},
			"hu": {
				"szusi":   "buffer",
				"szushi":  "buffer",
				"sushi":   "buffer",
				"vegeta":  "ctrl_backspace",
				"goku":    "ctrl_alt_backspace",
			},
		}, nil
	}

	data, err := os.ReadFile(deleteCommandsPath)
	if err != nil {
		// Return default commands if file not found
		if os.IsNotExist(err) {
			return DeleteCommands{
				"en": {
					"szusi":  "buffer",
					"sushi":  "buffer",
					"vegeta": "ctrl_backspace",
					"goku":   "ctrl_alt_backspace",
				},
				"hu": {
					"szusi":   "buffer",
					"szushi":  "buffer",
					"sushi":   "buffer",
					"vegeta":  "ctrl_backspace",
					"goku":    "ctrl_alt_backspace",
				},
			}, nil
		}
		return DeleteCommands{}, nil
	}

	var commands DeleteCommands
	err = json.Unmarshal(data, &commands)
	return commands, err
}

// SaveDeleteCommands saves delete commands to file
func (a *AppService) SaveDeleteCommands(commands DeleteCommands) error {
	deleteCommandsPath, err := getConfigPath("delete_commands.json")
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(commands, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(deleteCommandsPath, data, 0644)
}

// LoadUsageStats loads API usage statistics
func (a *AppService) LoadUsageStats() (*UsageStats, error) {
	statsPath, err := getConfigPath("api_usage_log.json")
	if err != nil {
		return &UsageStats{}, nil
	}

	data, err := os.ReadFile(statsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &UsageStats{}, nil
		}
		return nil, err
	}

	var stats UsageStats
	err = json.Unmarshal(data, &stats)
	return &stats, err
}

// SaveUsageStats saves API usage statistics
func (a *AppService) SaveUsageStats(stats UsageStats) error {
	statsPath, err := getConfigPath("api_usage_log.json")
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statsPath, data, 0644)
}

// ToggleListening starts or stops speech recognition
func (a *AppService) ToggleListening() error {
	a.mu.Lock()

	var newState bool
	var err error

	if a.isListening {
		// Stop listening
		if a.speechRecognizer != nil {
			a.speechRecognizer.Stop()
		}

		// Stop silence monitor
		a.stopSilenceMonitor()

		// Restore audio output
		if a.settings.MuteOutputDuringRecording && a.audioMuteManager != nil {
			err = a.audioMuteManager.UnmuteOutput()
			if err != nil {
				fmt.Printf("⚠️ Failed to unmute output: %v\n", err)
			}
		}

		a.isListening = false
		newState = false
		fmt.Println("Stopped listening")

		// Restore original PulseAudio source AFTER stream is closed
		// Use goroutine with delay to ensure PortAudio has fully released resources
		if a.originalPulseSource != "" {
			sourceToRestore := a.originalPulseSource
			a.originalPulseSource = ""
			go func(originalSource string) {
				// Wait for PortAudio to fully release resources
				time.Sleep(500 * time.Millisecond)

				currentSource := GetPulseAudioDefaultSource()
				if currentSource != originalSource {
					logToFile("🔊 Restoring original PulseAudio source: %s\n", originalSource)
					err := SetPulseAudioDefaultSource(originalSource)
					if err != nil {
						logToFile("⚠️ Failed to restore PulseAudio source: %v\n", err)
					} else {
						logToFile("✅ PulseAudio source restored\n")
					}
				}
			}(sourceToRestore)
		}
	} else {
		// Start listening
		logToFile("▶️  Starting listening mode...\n")
		logToFile("   Mode: %s\n", a.settings.Mode)
		logToFile("   Mute output: %v\n", a.settings.MuteOutputDuringRecording)
		logToFile("   Auto-stop on silence: %v (timeout: %ds)\n", a.settings.AutoStopOnSilence, a.settings.SilenceTimeoutSeconds)

		// Set PulseAudio source if user selected one (Linux)
		if a.selectedPulseSource != "" {
			// Save current source to restore later
			a.originalPulseSource = GetPulseAudioDefaultSource()
			logToFile("🎤 Saving original PulseAudio source: %s\n", a.originalPulseSource)

			// Set the user-selected source
			if a.selectedPulseSource != a.originalPulseSource {
				logToFile("🎤 Setting PulseAudio source to: %s\n", a.selectedPulseSource)
				err = SetPulseAudioDefaultSource(a.selectedPulseSource)
				if err != nil {
					logToFile("⚠️ Failed to set PulseAudio source: %v\n", err)
				} else {
					logToFile("✅ PulseAudio source set\n")
				}
			}
		}

		// Validate API key if in API or streaming mode
		if a.settings.Mode == "api" || a.settings.Mode == "streaming" {
			if a.settings.GoogleAPIKey == "" {
				// API key is missing
				logToFile("❌ ERROR: API key is missing\n")
				a.mu.Unlock()
				if a.onError != nil {
					a.onError("api_key_missing_title", "api_key_missing_message")
				}
				return fmt.Errorf("API key is missing")
			}
			logToFile("✅ API key validated\n")
		}

		// Mute audio output if enabled
		if a.settings.MuteOutputDuringRecording && a.audioMuteManager != nil {
			logToFile("🔇 Muting audio output...\n")
			err = a.audioMuteManager.MuteOutput()
			if err != nil {
				logToFile("⚠️ Failed to mute output: %v\n", err)
				fmt.Printf("⚠️ Failed to mute output: %v\n", err)
				// Don't fail the recording, just warn
			} else {
				logToFile("✅ Audio output muted\n")
			}
		}

		// Always create a new speech recognizer to avoid channel issues
		logToFile("🎤 Creating speech recognizer...\n")
		a.speechRecognizer = NewSpeechRecognizer(a)

		logToFile("🎙️  Starting speech recognition...\n")
		err = a.speechRecognizer.Start()
		if err != nil {
			logToFile("❌ ERROR: Failed to start speech recognition: %v\n", err)
			// Restore audio if start fails
			if a.settings.MuteOutputDuringRecording && a.audioMuteManager != nil {
				a.audioMuteManager.UnmuteOutput()
			}
			a.mu.Unlock()
			return fmt.Errorf("failed to start speech recognition: %w", err)
		}
		logToFile("✅ Speech recognition started\n")

		// Start silence monitor if enabled
		if a.settings.AutoStopOnSilence {
			a.startSilenceMonitor()
		}

		a.isListening = true
		newState = true
		logToFile("✅ Listening started successfully\n")
		fmt.Println("Started listening")
	}

	callback := a.onStateChange
	a.mu.Unlock()

	// Notify UI of state change
	// When called from button: already on main thread
	// When called from hotkey: already in goroutine (see app_service.go line 95)
	if callback != nil {
		callback(newState)
	}

	return nil
}

// IsListening returns whether the app is currently listening
func (a *AppService) IsListening() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.isListening
}

// SetSelectedPulseSource sets the PulseAudio source to use for recording (Linux)
// If empty string, the system default will be used
func (a *AppService) SetSelectedPulseSource(sourceName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.selectedPulseSource = sourceName
	logToFile("🎤 Selected PulseAudio source set to: '%s'\n", sourceName)
}

// GetSelectedPulseSource returns the currently selected PulseAudio source
func (a *AppService) GetSelectedPulseSource() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selectedPulseSource
}

// AudioTest performs audio test (record and playback)
func (a *AppService) AudioTest() error {
	if a.audioCapture == nil {
		return fmt.Errorf("audio capture not initialized")
	}
	return a.audioCapture.AudioTest()
}

// ForceProcessAudio forces immediate processing of current audio buffer
// This is triggered by Ctrl+Alt+S hotkey - allows user to send audio without waiting for silence
func (a *AppService) ForceProcessAudio() {
	a.mu.Lock()
	isListening := a.isListening
	mode := a.settings.Mode
	a.mu.Unlock()

	// Only works if we're listening and in free/api mode (batch modes)
	if !isListening {
		logToFile("⚠️ ForceProcessAudio: Not listening, ignoring\n")
		return
	}

	if mode == "streaming" {
		logToFile("⚠️ ForceProcessAudio: Streaming mode doesn't need this (already real-time)\n")
		return
	}

	logToFile("⚡ ForceProcessAudio: Forcing immediate audio processing (Ctrl+Alt+S)\n")

	// Tell the speech recognizer to process the current audio immediately
	if a.speechRecognizer != nil {
		a.speechRecognizer.ForceProcess()
	}
}

// Shutdown performs cleanup on application shutdown
func (a *AppService) Shutdown() {
	fmt.Println("Shutting down AppService...")

	// Stop listening if active
	a.mu.Lock()
	if a.isListening && a.speechRecognizer != nil {
		fmt.Println("Stopping speech recognizer...")
		a.speechRecognizer.Stop()
	}

	// Restore audio output if muted
	if a.audioMuteManager != nil {
		fmt.Println("Restoring audio output...")
		a.audioMuteManager.UnmuteOutput()
	}

	// Restore original PulseAudio source if we changed it (Linux)
	if a.originalPulseSource != "" {
		fmt.Printf("Restoring PulseAudio source to: %s\n", a.originalPulseSource)
		err := SetPulseAudioDefaultSource(a.originalPulseSource)
		if err != nil {
			fmt.Printf("⚠️ Failed to restore PulseAudio source: %v\n", err)
		} else {
			fmt.Println("✅ PulseAudio source restored")
		}
		a.originalPulseSource = ""
	}
	a.mu.Unlock()

	// Disable hotkey manager
	if a.hotkeyManager != nil {
		fmt.Println("Disabling hotkey manager...")
		a.hotkeyManager.Disable()
	}

	// Stop and cleanup audio
	if a.audioCapture != nil {
		fmt.Println("Stopping audio capture...")
		err := a.audioCapture.StopRecording()
		if err != nil {
			fmt.Printf("⚠️  Error stopping audio: %v\n", err)
		}

		// Give extra time for any remaining goroutines to finish
		// Increased from 500ms to 1500ms to ensure clean shutdown before Terminate()
		fmt.Println("Waiting for audio cleanup...")
		time.Sleep(1500 * time.Millisecond)

		fmt.Println("Terminating PortAudio...")
		err = a.audioCapture.Terminate()
		if err != nil {
			fmt.Printf("⚠️  Error terminating PortAudio: %v\n", err)
		}
	}

	fmt.Println("AppService shutdown complete.")
}

// UpdateLastSpeechTime updates the last speech time (called after successful recognition)
func (a *AppService) UpdateLastSpeechTime() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastSpeechTime = time.Now().Unix()
}

// startSilenceMonitor starts the silence monitoring goroutine
func (a *AppService) startSilenceMonitor() {
	a.silenceCheckActive = true
	a.lastSpeechTime = time.Now().Unix()
	a.silenceMonitorDone = make(chan bool)

	timeout := a.settings.SilenceTimeoutSeconds
	fmt.Printf("🔔 Auto-stop enabled (%d sec silence timeout)\n", timeout)

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-a.silenceMonitorDone:
				fmt.Println("Silence monitor stopped")
				return
			case <-ticker.C:
				// Check if auto-stop is still enabled
				a.mu.Lock()
				autoStopEnabled := a.settings.AutoStopOnSilence
				isListening := a.isListening
				lastSpeech := a.lastSpeechTime
				silenceTimeout := a.settings.SilenceTimeoutSeconds
				a.mu.Unlock()

				if !autoStopEnabled || !isListening {
					continue
				}

				// Calculate silence duration
				silenceDuration := time.Now().Unix() - lastSpeech

				// If silence timeout exceeded, stop listening
				if silenceDuration > int64(silenceTimeout) {
					fmt.Printf("⏱️ %d sec silence - auto-stopping\n", silenceTimeout)

					// Stop listening (this will also stop the silence monitor)
					go a.ToggleListening()
					return
				}
			}
		}
	}()
}

// stopSilenceMonitor stops the silence monitoring goroutine
func (a *AppService) stopSilenceMonitor() {
	if a.silenceCheckActive {
		a.silenceCheckActive = false
		if a.silenceMonitorDone != nil {
			close(a.silenceMonitorDone)
			a.silenceMonitorDone = nil
		}
	}
}

// UpdateHotkey updates the hotkey configuration
func (a *AppService) UpdateHotkey(ctrl, alt, shift bool, key string) {
	if a.hotkeyManager != nil {
		// Update configuration - changes take effect immediately
		// thanks to singleton pattern and mutex-protected config reading
		config := HotkeyConfig{
			Ctrl:  ctrl,
			Alt:   alt,
			Shift: shift,
			Key:   key,
		}

		err := a.hotkeyManager.UpdateConfig(config, "toggle")
		if err != nil {
			fmt.Printf("Warning: Failed to update hotkey config: %v\n", err)
		} else {
			fmt.Printf("✅ Hotkey updated: %v\n", config)
		}
	}
}

// UpdateInstantSendHotkey updates the instant send hotkey configuration
func (a *AppService) UpdateInstantSendHotkey(ctrl, alt, shift bool, key string) {
	if a.hotkeyManager != nil {
		config := HotkeyConfig{
			Ctrl:  ctrl,
			Alt:   alt,
			Shift: shift,
			Key:   key,
		}

		err := a.hotkeyManager.UpdateConfig(config, "send")
		if err != nil {
			fmt.Printf("Warning: Failed to update instant send hotkey config: %v\n", err)
		} else {
			fmt.Printf("✅ Instant send hotkey updated: %v\n", config)
		}
	}
}

// GetInitErrors returns any errors that occurred during initialization
func (a *AppService) GetInitErrors() []string {
	return a.initErrors
}

// UpdateSilenceSettings updates the silence detection settings
// threshold: percentage value (0-100)
// duration: seconds (0.1-2.0)
func (a *AppService) UpdateSilenceSettings(threshold, duration float64) {
	a.mu.Lock()
	a.settings.SilenceThreshold = threshold
	a.settings.SilenceDuration = duration
	a.mu.Unlock()

	if a.audioCapture != nil {
		// Convert percentage to actual threshold value
		actualThreshold := 0.0001 + (threshold / 100.0 * 0.0099)
		a.audioCapture.SetSilenceThreshold(actualThreshold)
		debugLog("🔊 Silence settings updated: threshold=%.0f%% (%.6f), duration=%.2fs\n", threshold, actualThreshold, duration)
	}
}
