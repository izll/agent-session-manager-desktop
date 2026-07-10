package dictation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/maxhawkins/go-webrtcvad"
)

// AudioCapture handles audio recording from microphone
type AudioCapture struct {
	stream          *portaudio.Stream
	audioBuffer     *bytes.Buffer
	isRecording     bool
	streamClosing   bool // Flag to indicate stream is being closed (prevents SIGSEGV)
	readInProgress  bool // Flag to indicate Read() is currently blocking
	silenceDetector *SilenceDetector
	vad             *webrtcvad.VAD
	vadMu           sync.Mutex
	mu              sync.Mutex
	recordDone      chan bool             // Signal that recordLoop has exited
	selectedDevice  *portaudio.DeviceInfo // Selected input device (nil = default)
	onVoiceLevel    func(level float64)   // Callback for voice level (0.0-1.0)
}

// AudioDevice represents an audio input device
type AudioDevice struct {
	Index int
	Name  string
}

// SilenceDetector detects silence in audio
type SilenceDetector struct {
	threshold       float64
	timeoutDuration time.Duration
	lastSoundTime   time.Time
	noiseFloor      float64   // Adaptive noise floor for better silence detection
	noiseFloorTime  time.Time // Last time noise floor was updated
	mu              sync.Mutex
}

const (
	sampleRate        = 16000
	channels          = 1
	framesPerBuffer   = 1024
	silenceThreshold  = 0.002 // Lower = more sensitive to sound (0.01 was too high)
	preSpeechBufferMs = 300   // Pre-speech buffer in milliseconds (to catch word beginnings)
)

// NewAudioCapture creates a new AudioCapture instance
func NewAudioCapture() *AudioCapture {
	// Initialize WebRTC VAD
	vad, err := webrtcvad.New()
	if err != nil {
		fmt.Printf("⚠️  Failed to initialize WebRTC VAD: %v (falling back to RMS only)\n", err)
		vad = nil
	} else {
		// Set mode 3 (most aggressive filtering, best for noisy environments)
		// Mode 0-3: 0=most permissive, 3=most aggressive
		err = vad.SetMode(3)
		if err != nil {
			fmt.Printf("⚠️  Failed to set VAD mode: %v\n", err)
		}
	}

	return &AudioCapture{
		audioBuffer: new(bytes.Buffer),
		silenceDetector: &SilenceDetector{
			threshold:       silenceThreshold,
			timeoutDuration: 60 * time.Second, // Default 60 seconds
			lastSoundTime:   time.Now(),
			noiseFloor:      0.0, // Will be calibrated during first few seconds
			noiseFloorTime:  time.Now(),
		},
		vad: vad,
	}
}

// Initialize initializes PortAudio
func (ac *AudioCapture) Initialize() error {
	return portaudio.Initialize()
}

// Terminate terminates PortAudio
func (ac *AudioCapture) Terminate() error {
	return portaudio.Terminate()
}

// GetAvailableInputDevices returns a list of available input devices
func (ac *AudioCapture) GetAvailableInputDevices() ([]AudioDevice, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("failed to get audio devices: %w", err)
	}

	var inputDevices []AudioDevice
	for _, device := range devices {
		// Only include devices with input channels
		if device.MaxInputChannels > 0 {
			// On Linux, skip direct hardware devices (hw:X,Y) as they often don't work
			// when PulseAudio/PipeWire is running. We use PulseAudio API instead.
			// On Windows/macOS, this check is harmless (no hw: devices exist).
			if strings.Contains(device.Name, "(hw:") {
				continue // Skip direct ALSA hardware devices
			}

			// Try to get a better (more user-friendly) name (Linux only, no-op on other platforms)
			displayName := getBetterDeviceName(device.Name)
			inputDevices = append(inputDevices, AudioDevice{
				Index: device.Index,
				Name:  displayName,
			})
		}
	}

	return inputDevices, nil
}

// SetInputDevice sets the input device by index (-1 for default)
func (ac *AudioCapture) SetInputDevice(deviceIndex int) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.isRecording {
		return fmt.Errorf("cannot change device while recording")
	}

	if deviceIndex == -1 {
		// Use default device
		ac.selectedDevice = nil
		logToFile("🎤 Set to default input device\n")
		return nil
	}

	// Get all devices and find the one with the specified index
	devices, err := portaudio.Devices()
	if err != nil {
		return fmt.Errorf("failed to get audio devices: %w", err)
	}

	for _, device := range devices {
		if device.Index == deviceIndex {
			if device.MaxInputChannels == 0 {
				return fmt.Errorf("device %d (%s) has no input channels", deviceIndex, device.Name)
			}
			ac.selectedDevice = device
			logToFile("🎤 Set input device to: %s (index: %d)\n", device.Name, device.Index)
			return nil
		}
	}

	return fmt.Errorf("device with index %d not found", deviceIndex)
}

// SetVoiceLevelCallback sets a callback function to receive voice level updates (0.0-1.0)
//
// The callback is called during audio recording with the calculated RMS (Root Mean Square)
// value normalized to 0.0-1.0 range, where:
// - 0.0 = complete silence
// - 1.0 = maximum detected voice level (0.1 RMS or higher)
//
// This callback is used for real-time voice activity indication, such as
// pulsing the taskbar icon when speech is detected.
func (ac *AudioCapture) SetVoiceLevelCallback(callback func(level float64)) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.onVoiceLevel = callback
}

// GetCurrentInputDevice returns the current input device info
func (ac *AudioCapture) GetCurrentInputDevice() (AudioDevice, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.selectedDevice != nil {
		return AudioDevice{
			Index: ac.selectedDevice.Index,
			Name:  ac.selectedDevice.Name,
		}, nil
	}

	// Return default device
	defaultDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		return AudioDevice{}, fmt.Errorf("failed to get default input device: %w", err)
	}

	return AudioDevice{
		Index: defaultDevice.Index,
		Name:  defaultDevice.Name,
	}, nil
}

// StartRecording starts recording audio
func (ac *AudioCapture) StartRecording(silenceTimeout int) error {
	ac.mu.Lock()
	if ac.isRecording {
		ac.mu.Unlock()
		return fmt.Errorf("already recording")
	}
	ac.audioBuffer.Reset()
	ac.silenceDetector.timeoutDuration = time.Duration(silenceTimeout) * time.Second
	ac.silenceDetector.lastSoundTime = time.Now()
	ac.isRecording = true
	ac.streamClosing = false           // Reset flag for new recording session
	ac.recordDone = make(chan bool, 1) // Create new channel for this recording session
	selectedDevice := ac.selectedDevice
	ac.mu.Unlock()

	// Create input stream
	inputBuffer := make([]int16, framesPerBuffer)
	var stream *portaudio.Stream
	var err error

	if selectedDevice == nil {
		// Use default device
		stream, err = portaudio.OpenDefaultStream(
			channels, // input channels
			0,        // output channels
			float64(sampleRate),
			framesPerBuffer,
			inputBuffer,
		)
	} else {
		// Use selected device
		streamParams := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   selectedDevice,
				Channels: channels,
				Latency:  selectedDevice.DefaultLowInputLatency,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: framesPerBuffer,
		}
		stream, err = portaudio.OpenStream(streamParams, inputBuffer)
	}

	if err != nil {
		ac.mu.Lock()
		ac.isRecording = false
		ac.streamClosing = false // Reset on error
		ac.mu.Unlock()
		return fmt.Errorf("failed to open audio stream: %w", err)
	}

	ac.stream = stream

	err = stream.Start()
	if err != nil {
		// OpenStream succeeded, so we own the stream even if Start fails.
		// Closing it here avoids leaking the native PortAudio resources.
		_ = stream.Close()
		ac.mu.Lock()
		ac.stream = nil
		ac.isRecording = false
		ac.streamClosing = false // Reset on error
		ac.mu.Unlock()
		return fmt.Errorf("failed to start audio stream: %w", err)
	}

	// Start recording in goroutine
	go ac.recordLoop(inputBuffer)

	return nil
}

// recordLoop is the main recording loop
func (ac *AudioCapture) recordLoop(buffer []int16) {
	defer func() {
		// Recover from any panic (e.g., if stream was closed while reading)
		if r := recover(); r != nil {
			logToFile("📡 recordLoop: recovered from panic: %v\n", r)
		}

		// Signal that we've exited
		ac.mu.Lock()
		if ac.recordDone != nil {
			select {
			case ac.recordDone <- true:
			default:
			}
		}
		ac.mu.Unlock()
		logToFile("📡 recordLoop: exited\n")
	}()

	for {
		// First check: Are we still recording?
		ac.mu.Lock()
		if !ac.isRecording {
			ac.mu.Unlock()
			logToFile("📡 recordLoop: isRecording=false, exiting\n")
			return
		}
		stream := ac.stream
		ac.mu.Unlock()

		// Second check: Is stream still valid?
		if stream == nil {
			logToFile("📡 recordLoop: stream is nil, exiting\n")
			return
		}

		// Third check: Double-check recording status AND get current stream reference
		// CRITICAL: We set readInProgress=true while holding the lock, so StopRecording knows
		// we're about to enter Read() and must wait for us to finish
		ac.mu.Lock()
		stillRecording := ac.isRecording
		streamClosing := ac.streamClosing
		currentStream := ac.stream
		if !stillRecording || streamClosing || currentStream == nil {
			ac.mu.Unlock()
			logToFile("📡 recordLoop: recording stopped, stream closing, or stream nil before Read(), exiting\n")
			return
		}
		ac.readInProgress = true
		ac.mu.Unlock()

		// Read from stream using the reference we got while holding the lock
		// If StopRecording() runs now, it will wait for readInProgress to become false
		err := currentStream.Read()

		// Clear readInProgress flag
		ac.mu.Lock()
		ac.readInProgress = false
		ac.mu.Unlock()
		if err != nil {
			logToFile("⚠️  Error reading audio stream: %v\n", err)
			return
		}

		// Convert and append the whole callback buffer while holding the same
		// mutex used by the consumers. This makes bytes.Buffer ownership explicit.
		audioBytes := make([]byte, len(buffer)*2)
		for i, sample := range buffer {
			binary.LittleEndian.PutUint16(audioBytes[i*2:], uint16(sample))
		}
		ac.mu.Lock()
		_, _ = ac.audioBuffer.Write(audioBytes)
		ac.mu.Unlock()

		// Check for silence
		if ac.detectSilence(buffer) {
			ac.silenceDetector.mu.Lock()
			if time.Since(ac.silenceDetector.lastSoundTime) > ac.silenceDetector.timeoutDuration {
				ac.silenceDetector.mu.Unlock()
				logToFile("Silence timeout reached, stopping recording\n")
				ac.StopRecording()
				return
			}
			ac.silenceDetector.mu.Unlock()
		} else {
			ac.silenceDetector.mu.Lock()
			ac.silenceDetector.lastSoundTime = time.Now()
			ac.silenceDetector.mu.Unlock()
		}
	}
}

// detectSilence checks if the audio buffer contains silence
func (ac *AudioCapture) detectSilence(buffer []int16) bool {
	// Calculate RMS (Root Mean Square) of the audio samples
	var sum float64
	for _, sample := range buffer {
		normalized := float64(sample) / 32768.0 // Normalize to -1.0 to 1.0
		sum += normalized * normalized
	}
	rms := math.Sqrt(sum / float64(len(buffer)))

	// Notify voice level callback (0.0-1.0 range, clamped)
	ac.mu.Lock()
	onVoiceLevel := ac.onVoiceLevel
	ac.mu.Unlock()
	if onVoiceLevel != nil {
		// Normalize RMS to 0-1 range (typical speech is 0.01-0.3 RMS)
		// Scale it so 0.1 RMS = 1.0 level (100% indicator)
		level := math.Min(1.0, rms*10.0)
		onVoiceLevel(level)
	}

	// Adaptive noise floor: continuously track background noise level
	// Update noise floor with very low RMS values (likely background noise)
	// This helps filter out constant background noise like fans, AC, etc.
	// IMPORTANT: Only update noise floor when VAD confirms there's NO speech
	// This prevents calibrating to speech if recording starts during conversation
	ac.silenceDetector.mu.Lock()
	shouldUpdateNoiseFloor := false
	if time.Since(ac.silenceDetector.noiseFloorTime) > 100*time.Millisecond {
		// Only update if RMS is low (likely background noise, not speech)
		if rms < ac.silenceDetector.threshold*2 {
			// Double-check with VAD: only update if VAD confirms NO speech
			// This prevents noise floor from being corrupted by speech at startup
			if ac.vad != nil {
				// Convert buffer to bytes for VAD check
				audioBytes := make([]byte, len(buffer)*2)
				for i, sample := range buffer {
					binary.LittleEndian.PutUint16(audioBytes[i*2:], uint16(sample))
				}
				frameSize := 160 * 2 // 10ms at 16kHz
				if len(audioBytes) >= frameSize {
					isVoice, err := ac.vad.Process(sampleRate, audioBytes[:frameSize])
					// Only update noise floor if VAD confirms it's NOT speech
					if err == nil && !isVoice {
						shouldUpdateNoiseFloor = true
					}
				}
			} else {
				// No VAD available, trust RMS threshold
				shouldUpdateNoiseFloor = true
			}

			if shouldUpdateNoiseFloor {
				if ac.silenceDetector.noiseFloor == 0 {
					// Initialize noise floor on first SILENCE measurement
					ac.silenceDetector.noiseFloor = rms
					logToFile("🎚️  Noise floor initialized: %.6f\n", rms)
				} else {
					// Smooth update: 90% old value + 10% new value
					ac.silenceDetector.noiseFloor = 0.9*ac.silenceDetector.noiseFloor + 0.1*rms
				}
				ac.silenceDetector.noiseFloorTime = time.Now()
			}
		}
	}
	noiseFloor := ac.silenceDetector.noiseFloor
	threshold := ac.silenceDetector.threshold
	ac.silenceDetector.mu.Unlock()

	// Dynamic threshold: base threshold + safety margin above noise floor
	// This ensures we detect silence even with varying background noise
	dynamicThreshold := math.Max(threshold, noiseFloor*1.5)

	// First check: RMS threshold with dynamic adjustment
	if rms < dynamicThreshold {
		return true // Silence (below dynamic threshold)
	}

	// Second check: WebRTC VAD (intelligent voice activity detection)
	// This helps filter out background noise that has high RMS but isn't speech
	if ac.vad != nil {
		// Convert int16 buffer to bytes for VAD
		audioBytes := make([]byte, len(buffer)*2)
		for i, sample := range buffer {
			binary.LittleEndian.PutUint16(audioBytes[i*2:], uint16(sample))
		}

		// VAD requires specific frame sizes (10, 20, or 30ms at supported sample rates)
		// At 16kHz, 1024 samples = 64ms, which is not valid
		// We need to process in 10ms chunks (160 samples at 16kHz)
		// Process the first 10ms chunk only for quick detection
		frameSize := 160 * 2 // 160 samples * 2 bytes per sample = 320 bytes
		if len(audioBytes) >= frameSize {
			ac.vadMu.Lock()
			isVoice, err := ac.vad.Process(sampleRate, audioBytes[:frameSize])
			ac.vadMu.Unlock()
			if err != nil {
				logToFile("⚠️  VAD error: %v (falling back to RMS only)\n", err)
				return false // If VAD fails, assume there might be speech
			}

			if !isVoice {
				// VAD says no voice detected, even though RMS is above threshold
				// This is background noise, consider it as silence
				return true
			}
		}
	}

	// Either VAD detected voice, or VAD is not available
	// Trust the RMS result in this case
	return false
}

// StopRecording stops recording audio
func (ac *AudioCapture) StopRecording() error {
	ac.mu.Lock()
	if !ac.isRecording {
		ac.mu.Unlock()
		return nil
	}

	logToFile("🛑 StopRecording: setting isRecording=false and streamClosing=true\n")
	ac.isRecording = false
	ac.streamClosing = true // CRITICAL: Set this BEFORE releasing mutex to prevent SIGSEGV
	stream := ac.stream
	// DON'T set ac.stream = nil yet - recordLoop might be using it
	recordDone := ac.recordDone
	readInProgress := ac.readInProgress
	ac.mu.Unlock()

	// CRITICAL: Wait for readInProgress to become false BEFORE any stream operations
	// PortAudio's Pa_ReadStream can SIGSEGV if we abort/close while it's running in CGo
	logToFile("🛑 StopRecording: waiting for Read() to complete (if in progress)...\n")
	for i := 0; i < 100; i++ { // 100 * 10ms = 1000ms max
		ac.mu.Lock()
		readInProgress = ac.readInProgress
		ac.mu.Unlock()
		if !readInProgress {
			logToFile("🛑 StopRecording: Read() not in progress, safe to proceed\n")
			break
		}
		if i%10 == 0 {
			logToFile("🛑 StopRecording: still waiting for Read() to complete... (%d)\n", i)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Double check - if still in progress after timeout, log warning but continue
	ac.mu.Lock()
	if ac.readInProgress {
		logToFile("⚠️  StopRecording: Read() still in progress after timeout, proceeding anyway (may crash)\n")
	}
	ac.stream = nil
	ac.mu.Unlock()

	logToFile("🛑 StopRecording: stopping stream...\n")

	// Use Stop() instead of Abort() - it waits for the current operation to complete
	// This is safer than Abort() which can cause SIGSEGV if Read() is still active
	if stream != nil {
		err := stream.Stop()
		if err != nil {
			logToFile("⚠️  Error stopping audio stream: %v\n", err)
			// Try abort as fallback
			stream.Abort()
		} else {
			logToFile("✅ Stream stopped cleanly\n")
		}
	}

	logToFile("🛑 StopRecording: waiting for recordLoop to exit...\n")

	// Wait for recordLoop to signal it has exited (with timeout)
	if recordDone != nil {
		select {
		case <-recordDone:
			logToFile("✅ recordLoop exited cleanly\n")
		case <-time.After(500 * time.Millisecond):
			logToFile("⚠️  recordLoop didn't exit within timeout (this is OK after Abort)\n")
		}
	}

	logToFile("🛑 StopRecording: closing stream...\n")

	// Finally close the stream
	if stream != nil {
		err := stream.Close()
		if err != nil {
			logToFile("⚠️  Error closing audio stream: %v\n", err)
			// Don't return error - we're already stopped
		} else {
			logToFile("✅ Stream closed successfully\n")
		}
	}

	// Reset streamClosing flag for next recording session
	ac.mu.Lock()
	ac.streamClosing = false
	ac.mu.Unlock()
	logToFile("✅ StopRecording completed, ready for next session\n")

	return nil
}

// GetAudioData returns the recorded audio data
func (ac *AudioCapture) GetAudioData() []byte {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return bytes.Clone(ac.audioBuffer.Bytes())
}

// GetAndClearAudioData returns the recorded audio data and clears the buffer
func (ac *AudioCapture) GetAndClearAudioData() []byte {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	data := bytes.Clone(ac.audioBuffer.Bytes())
	ac.audioBuffer.Reset()
	return data
}

// GetAndClearAudioDataIfAtLeast atomically transfers ownership of the current
// PCM buffer when it is large enough. A nil result leaves the buffer intact.
func (ac *AudioCapture) GetAndClearAudioDataIfAtLeast(minBytes int) []byte {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.audioBuffer.Len() < minBytes {
		return nil
	}
	data := bytes.Clone(ac.audioBuffer.Bytes())
	ac.audioBuffer.Reset()
	return data
}

// IsRecording returns whether recording is in progress
func (ac *AudioCapture) IsRecording() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.isRecording
}

// SetSilenceThreshold sets the silence detection threshold
func (ac *AudioCapture) SetSilenceThreshold(threshold float64) {
	ac.silenceDetector.mu.Lock()
	defer ac.silenceDetector.mu.Unlock()
	ac.silenceDetector.threshold = threshold
}

// GetTimeSinceLastSound returns the duration since last sound was detected
func (ac *AudioCapture) GetTimeSinceLastSound() time.Duration {
	ac.silenceDetector.mu.Lock()
	defer ac.silenceDetector.mu.Unlock()
	return time.Since(ac.silenceDetector.lastSoundTime)
}

// HasSoundInBuffer checks if the current audio buffer contains actual sound (not just silence)
func (ac *AudioCapture) HasSoundInBuffer() bool {
	ac.mu.Lock()
	audioData := bytes.Clone(ac.audioBuffer.Bytes())
	ac.mu.Unlock()
	return ac.HasSound(audioData)
}

// HasSound checks an immutable PCM chunk.
func (ac *AudioCapture) HasSound(audioData []byte) bool {
	if len(audioData) == 0 {
		return false
	}

	// Convert bytes to int16 samples
	samples := make([]int16, len(audioData)/2)
	buf := bytes.NewReader(audioData)
	err := binary.Read(buf, binary.LittleEndian, &samples)
	if err != nil {
		return false
	}

	// Use the same RMS calculation as detectSilence
	var sum float64
	for _, sample := range samples {
		normalized := float64(sample) / 32768.0
		sum += normalized * normalized
	}
	rms := math.Sqrt(sum / float64(len(samples)))

	// First check: RMS threshold
	ac.silenceDetector.mu.Lock()
	threshold := ac.silenceDetector.threshold
	ac.silenceDetector.mu.Unlock()
	if rms < threshold {
		return false // Definitely silence
	}

	// Second check: Use WebRTC VAD if available
	// Check multiple frames throughout the buffer to ensure there's real speech
	if ac.vad != nil {
		ac.vadMu.Lock()
		defer ac.vadMu.Unlock()
		frameSize := 160 * 2 // 10ms at 16kHz = 320 bytes
		voiceFrameCount := 0
		totalFrames := 0

		// Check every 10ms frame in the buffer
		for offset := 0; offset+frameSize <= len(audioData); offset += frameSize {
			isVoice, err := ac.vad.Process(sampleRate, audioData[offset:offset+frameSize])
			if err == nil {
				totalFrames++
				if isVoice {
					voiceFrameCount++
				}
			}
		}

		// If we processed frames, require at least 30% to be voice
		if totalFrames > 0 {
			voiceRatio := float64(voiceFrameCount) / float64(totalFrames)
			return voiceRatio >= 0.3
		}
	}

	// Fallback to RMS result if VAD is not available
	return rms >= ac.silenceDetector.threshold
}

// AudioTest records and plays back audio for testing
func (ac *AudioCapture) AudioTest() error {
	fmt.Println("Starting audio test: recording for 5 seconds...")

	// Record for 5 seconds
	err := ac.StartRecording(5)
	if err != nil {
		return err
	}

	time.Sleep(5 * time.Second)

	err = ac.StopRecording()
	if err != nil {
		return err
	}

	audioData := ac.GetAudioData()
	fmt.Printf("Recorded %d bytes of audio\n", len(audioData))

	// Play back
	fmt.Println("Playing back recorded audio...")
	err = ac.playbackAudio(audioData)
	if err != nil {
		return err
	}

	fmt.Println("Audio test completed!")
	return nil
}

// playbackAudio plays back recorded audio
func (ac *AudioCapture) playbackAudio(audioData []byte) error {
	// Convert bytes back to int16 samples
	samples := make([]int16, len(audioData)/2)
	buf := bytes.NewReader(audioData)
	err := binary.Read(buf, binary.LittleEndian, &samples)
	if err != nil {
		return fmt.Errorf("failed to read audio data: %w", err)
	}

	// Open output stream
	stream, err := portaudio.OpenDefaultStream(
		0,        // input channels
		channels, // output channels
		float64(sampleRate),
		framesPerBuffer,
		samples,
	)
	if err != nil {
		return fmt.Errorf("failed to open playback stream: %w", err)
	}
	defer stream.Close()

	err = stream.Start()
	if err != nil {
		return fmt.Errorf("failed to start playback stream: %w", err)
	}

	err = stream.Write()
	if err != nil {
		return fmt.Errorf("failed to write to playback stream: %w", err)
	}

	err = stream.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop playback stream: %w", err)
	}

	return nil
}
