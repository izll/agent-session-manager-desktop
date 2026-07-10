package dictation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SpeechRecognizer handles speech recognition
type SpeechRecognizer struct {
	app                 *AppService
	isRunning           bool
	stopChannel         chan bool
	mu                  sync.Mutex
	recognitionMux      sync.Mutex
	streamingRecognizer *StreamingRecognizer // For streaming mode
	httpClient          *http.Client
	requestCtx          context.Context
	cancelRequests      context.CancelFunc
}

// GoogleCloudSpeechRequest represents the request structure for Google Cloud Speech API
type GoogleCloudSpeechRequest struct {
	Config struct {
		Encoding                   string              `json:"encoding"`
		SampleRateHertz            int                 `json:"sampleRateHertz"`
		LanguageCode               string              `json:"languageCode"`
		EnableAutomaticPunctuation bool                `json:"enableAutomaticPunctuation"`
		Model                      string              `json:"model"`
		SpeechContexts             []SpeechContextItem `json:"speechContexts,omitempty"`
	} `json:"config"`
	Audio struct {
		Content string `json:"content"`
	} `json:"audio"`
}

// SpeechContextItem represents a speech context in the request
type SpeechContextItem struct {
	Phrases []string `json:"phrases"`
}

// GoogleCloudSpeechResponse represents the response from Google Cloud Speech API
type GoogleCloudSpeechResponse struct {
	Results []struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"results"`
}

// NewSpeechRecognizer creates a new SpeechRecognizer
func NewSpeechRecognizer(app *AppService) *SpeechRecognizer {
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	return &SpeechRecognizer{
		app:            app,
		isRunning:      false,
		stopChannel:    make(chan bool, 1), // Buffered channel to prevent blocking
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		requestCtx:     requestCtx,
		cancelRequests: cancelRequests,
	}
}

// Start starts the speech recognition
func (sr *SpeechRecognizer) Start() error {
	logToFile("DEBUG: Start() called\n")
	sr.mu.Lock()
	if sr.isRunning {
		sr.mu.Unlock()
		logToFile("DEBUG: Already running, returning error\n")
		return fmt.Errorf("speech recognition already running")
	}
	sr.isRunning = true
	sr.mu.Unlock()

	logToFile("DEBUG: Launching recognitionLoop goroutine...\n")
	// Start recognition in goroutine
	go sr.recognitionLoop()

	logToFile("DEBUG: Start() returning nil\n")
	return nil
}

// Stop stops the speech recognition
func (sr *SpeechRecognizer) Stop() {
	sr.mu.Lock()
	if sr.isRunning {
		select {
		case sr.stopChannel <- true:
		default:
		}
		sr.isRunning = false
	}
	cancelRequests := sr.cancelRequests
	sr.mu.Unlock()

	if cancelRequests != nil {
		cancelRequests()
	}
}

// ForceProcess forces immediate processing of the audio buffer
// This is called when the user presses the instant send hotkey (Ctrl+Alt+S)
// to avoid waiting for silence detection
func (sr *SpeechRecognizer) ForceProcess() {
	sr.mu.Lock()
	isRunning := sr.isRunning
	sr.mu.Unlock()

	if !isRunning {
		logToFile("⚠️ ForceProcess: Not running, ignoring\n")
		return
	}

	logToFile("⚡ ForceProcess: Forcing immediate audio processing (Ctrl+Alt+S)\n")

	// Process the current audio chunk immediately
	sr.processAudioChunk()
}

// recognitionLoop is the main recognition loop with streaming
func (sr *SpeechRecognizer) recognitionLoop() {
	logToFile("DEBUG: recognitionLoop started\n")
	settings := sr.app.GetSettings()

	// Check if we're in streaming mode
	if settings.Mode == "streaming" {
		logToFile("🌊 Using STREAMING mode (real-time gRPC streaming)\n")
		sr.streamingRecognitionLoop()
		return
	}

	// Get silence detection settings
	// threshold: stored as percentage (0-100), convert to actual value (0.0001-0.01)
	// duration: how long audio must stay below threshold to process (0.1-2.0 seconds)
	thresholdPercent := settings.SilenceThreshold
	threshold := 0.0001 + (thresholdPercent / 100.0 * 0.0099)
	minSilenceDuration := settings.SilenceDuration
	sr.app.audioCapture.SetSilenceThreshold(threshold)

	// Warn if silence_duration is too short (can cause words to be cut in half)
	if minSilenceDuration < 0.3 {
		logToFile("⚠️  WARNING: silence_duration is very short (%.2fs) - words may be cut in half!\n", minSilenceDuration)
		logToFile("⚠️  Recommended: 0.4-0.6 seconds for best results. Edit ~/.config/agent-session-manager-desktop/dictation/settings.json\n")
	}

	logToFile("🎚️  Silence detection: threshold=%.6f (%.0f%%), duration=%.2fs\n",
		threshold, thresholdPercent, minSilenceDuration)

	// Start audio recording
	logToFile("DEBUG: Calling StartRecording...\n")
	err := sr.app.audioCapture.StartRecording(settings.SilenceTimeoutSeconds)
	logToFile("DEBUG: StartRecording returned, err=%v\n", err)
	if err != nil {
		logToFile("Error starting audio recording: %v\n", err)
		sr.Stop()
		return
	}

	logToFile("Recording started with batch recognition...\n")

	// Streaming recognition parameters
	// Check more frequently for faster, more fluid recognition (like Android)
	chunkInterval := 500 * time.Millisecond // Check every 0.5 seconds
	logToFile("Creating ticker with interval: %v\n", chunkInterval)
	ticker := time.NewTicker(chunkInterval)
	defer ticker.Stop()

	logToFile("Entering main streaming loop...\n")
	// Main streaming loop
	for {
		select {
		case <-sr.stopChannel:
			// Stop recording
			sr.app.audioCapture.StopRecording()
			logToFile("Recording stopped by user\n")
			// Process any remaining audio
			sr.processAudioChunk()
			return

		case <-ticker.C:
			// Periodically check and process audio chunks
			logToFile("Ticker fired - checking for audio chunks...\n")
			if sr.app.audioCapture.IsRecording() {
				// Check if we've had sustained silence for the required duration
				silenceDuration := sr.app.audioCapture.GetTimeSinceLastSound()
				requiredDuration := time.Duration(minSilenceDuration*1000) * time.Millisecond
				logToFile("Silence duration: %.2f seconds (required: %.2fs)\n",
					silenceDuration.Seconds(), minSilenceDuration)

				if silenceDuration >= requiredDuration {
					logToFile("Sustained silence detected, processing chunk...\n")
					sr.processAudioChunk()
				} else {
					logToFile("Still speaking or silence too brief, waiting...\n")
				}
			} else {
				logToFile("Recording stopped automatically\n")
				// Process any remaining audio
				sr.processAudioChunk()
				sr.Stop()
				return
			}
		}
	}
}

// processAudioChunk processes a chunk of audio during streaming
func (sr *SpeechRecognizer) processAudioChunk() {
	sr.recognitionMux.Lock()

	// Skip if chunk is too small
	// Reduced minimum for faster, more fluid recognition (Android-like behavior)
	// Free mode: 0.2s minimum (very responsive)
	// API mode: 0.5s minimum (balance between speed and API costs)
	settings := sr.app.GetSettings()
	minSeconds := 0.5
	if settings.Mode == "free" || settings.GoogleAPIKey == "" {
		minSeconds = 0.2 // Very responsive in free mode
	}
	minChunkSize := int(minSeconds * 16000 * 2) // at 16kHz, 2 bytes per sample
	audioDataCopy := sr.app.audioCapture.GetAndClearAudioDataIfAtLeast(minChunkSize)
	if audioDataCopy == nil {
		sr.recognitionMux.Unlock()
		return
	}
	// Buffer ownership has already transferred; concurrent processing may now
	// select the next chunk while this one is analyzed/uploaded.
	sr.recognitionMux.Unlock()

	// Check if buffer actually contains sound (not just silence)
	hasSound := sr.app.audioCapture.HasSound(audioDataCopy)
	if !hasSound {
		logToFile("⚪ Buffer contains only silence, skipping API call to save costs\n")
		return
	}

	// Update last speech time since we detected sound in the buffer
	// This prevents auto-stop from triggering while user is speaking
	sr.app.UpdateLastSpeechTime()

	// Trim excessive pre-speech silence (keep only last 1 second before speech)
	// This reduces API costs and processing time while still catching word beginnings
	audioDataCopy = sr.trimPreSpeechSilence(audioDataCopy)

	logToFile("Processing audio chunk: %d bytes (%.1f seconds)...\n",
		len(audioDataCopy), float64(len(audioDataCopy))/(16000*2))

	// Notify UI that we're uploading
	sr.app.NotifyUploading(true)

	// Recognize speech (this happens in parallel with ongoing recording)
	// The buffer is already cleared, so new audio can accumulate while we wait for API
	transcript, err := sr.RecognizeAudio(audioDataCopy, settings.Language)

	// Notify UI that upload is complete
	sr.app.NotifyUploading(false)

	if err != nil {
		// Check if it's a "no recognition results" error (audio was processed but no speech found)
		if err.Error() == "no recognition results" {
			logToFile("⚪ No speech detected in this chunk (silence or unclear audio)\n")
			return
		}
		// For other errors (network issues, etc.), still report them
		logToFile("Speech recognition error: %v\n", err)
		return
	}

	if transcript == "" {
		logToFile("⚪ Empty transcript returned\n")
		return
	}

	// Update last speech time for auto-stop silence monitoring
	sr.app.UpdateLastSpeechTime()

	logToFile("Recognized: %s\n", transcript)

	// Process text (handle commands, transformations)
	processedText, deleteCount := sr.ProcessText(transcript, settings.Language)
	logToFile("Processed text: '%s' (delete count: %d)\n", processedText, deleteCount)

	// Handle delete/undo commands
	if deleteCount > 0 && sr.app.keyboard != nil {
		for i := 0; i < deleteCount; i++ {
			err = sr.app.keyboard.DeleteLastWord()
			if err != nil {
				logToFile("Error deleting word: %v\n", err)
			}
		}
	}

	// Type the text using keyboard simulation (if any text remains after processing)
	if processedText != "" && sr.app.keyboard != nil {
		logToFile("Typing text via keyboard simulator...\n")
		err = sr.app.keyboard.TypeText(processedText)
		if err != nil {
			logToFile("Error typing text: %v\n", err)
		} else {
			logToFile("Successfully typed text\n")
		}
	} else if sr.app.keyboard == nil {
		logToFile("WARNING: Keyboard simulator is nil!\n")
	}
}

// trimPreSpeechSilence trims excessive silence from the beginning of audio data
// Keeps only the last 1 second of silence before speech starts to catch word beginnings
// while reducing API costs and processing time
func (sr *SpeechRecognizer) trimPreSpeechSilence(audioData []byte) []byte {
	if len(audioData) == 0 {
		return audioData
	}

	// Convert bytes to int16 samples for analysis
	sampleCount := len(audioData) / 2
	samples := make([]int16, sampleCount)
	buf := bytes.NewReader(audioData)
	err := binary.Read(buf, binary.LittleEndian, &samples)
	if err != nil {
		logToFile("⚠️  Failed to parse audio for silence trimming: %v\n", err)
		return audioData
	}

	// Find where speech starts by analyzing audio in frames
	const frameSize = 160          // 10ms at 16kHz (160 samples)
	const silenceThreshold = 0.002 // Same as audio_capture.go silenceThreshold
	const preSpeechSeconds = 1.0   // Keep 1 second of silence before speech
	const samplesPerSecond = 16000

	speechStartFrame := -1

	// Scan through audio to find first frame with speech
	for frameStart := 0; frameStart+frameSize <= len(samples); frameStart += frameSize {
		frameEnd := frameStart + frameSize
		frame := samples[frameStart:frameEnd]

		// Calculate RMS for this frame
		var sum float64
		for _, sample := range frame {
			normalized := float64(sample) / 32768.0
			sum += normalized * normalized
		}
		rms := math.Sqrt(sum / float64(len(frame)))

		// Check if this frame contains speech (above threshold)
		if rms >= silenceThreshold {
			// Also verify with VAD if available
			isSpeech := true
			if sr.app.audioCapture.vad != nil {
				// Convert frame to bytes for VAD
				frameBytes := make([]byte, frameSize*2)
				for i, sample := range frame {
					binary.LittleEndian.PutUint16(frameBytes[i*2:], uint16(sample))
				}
				isVoice, vadErr := sr.app.audioCapture.vad.Process(16000, frameBytes)
				if vadErr == nil && !isVoice {
					isSpeech = false // VAD says no speech
				}
			}

			if isSpeech {
				speechStartFrame = frameStart / frameSize
				break
			}
		}
	}

	// If no speech found, return original audio (shouldn't happen as HasSoundInBuffer already checked)
	if speechStartFrame == -1 {
		logToFile("📊 No speech detected in audio, keeping all data\n")
		return audioData
	}

	// Calculate how many samples of silence to keep (1 second)
	preSpeechSamples := int(preSpeechSeconds * samplesPerSecond)
	speechStartSample := speechStartFrame * frameSize

	// Calculate trim point: speech start - 1 second (but never negative)
	trimSample := speechStartSample - preSpeechSamples
	if trimSample < 0 {
		trimSample = 0
	}

	// Convert back to byte offset
	trimByteOffset := trimSample * 2

	// Calculate how much silence we're trimming
	trimmedSeconds := float64(trimByteOffset) / (samplesPerSecond * 2)
	totalSeconds := float64(len(audioData)) / (samplesPerSecond * 2)

	if trimByteOffset > 0 {
		logToFile("✂️  Trimming %.2fs of pre-speech silence (keeping 1s before speech, total: %.2fs -> %.2fs)\n",
			trimmedSeconds, totalSeconds, totalSeconds-trimmedSeconds)
		return audioData[trimByteOffset:]
	}

	logToFile("📊 Speech starts within first 1 second, no trimming needed\n")
	return audioData
}

// RecognizeAudio sends audio to Google Cloud Speech-to-Text API
func (sr *SpeechRecognizer) RecognizeAudio(audioData []byte, language string) (string, error) {
	settings := sr.app.GetSettings()

	if settings.Mode == "api" && settings.GoogleAPIKey != "" {
		return sr.recognizeWithGoogleCloud(audioData, language, settings.GoogleAPIKey)
	}

	// Fallback to free mode (would use local recognition)
	return sr.recognizeWithFreeMode(audioData, language)
}

// recognizeWithGoogleCloud uses Google Cloud Speech-to-Text API
func (sr *SpeechRecognizer) recognizeWithGoogleCloud(audioData []byte, language string, apiKey string) (string, error) {
	logToFile("📤 [API] Sending request to Google Cloud Speech API\n")
	logToFile("📤 [API] Audio size: %d bytes (%.2f seconds)\n", len(audioData), float64(len(audioData))/(16000*2))
	logToFile("📤 [API] Language: %s\n", language)

	// Load speech context
	speechContext, err := sr.app.LoadSpeechContext()
	if err != nil {
		return "", err
	}

	// Prepare request
	request := GoogleCloudSpeechRequest{}
	request.Config.Encoding = "LINEAR16"
	request.Config.SampleRateHertz = 16000
	request.Config.LanguageCode = language
	request.Config.EnableAutomaticPunctuation = true
	request.Config.Model = "default"

	// Add speech context if available
	if phrases, ok := speechContext[language]; ok && len(phrases) > 0 {
		// Limit to 500 phrases (Google Cloud limit)
		maxPhrases := 500
		if len(phrases) > maxPhrases {
			phrases = phrases[:maxPhrases]
		}
		request.Config.SpeechContexts = []SpeechContextItem{
			{Phrases: phrases},
		}
	}

	// Encode audio as base64
	request.Audio.Content = base64.StdEncoding.EncodeToString(audioData)

	// Marshal request to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	// Make API request
	url := fmt.Sprintf("https://speech.googleapis.com/v1/speech:recognize?key=%s", apiKey)
	logToFile("📤 [API] Sending POST request...\n")

	req, err := http.NewRequestWithContext(sr.requestCtx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := sr.httpClient.Do(req)
	if err != nil {
		logToFile("❌ [API] Request failed: %v\n", err)
		return "", err
	}
	defer resp.Body.Close()

	logToFile("📥 [API] Response status: %d\n", resp.StatusCode)

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logToFile("❌ [API] Failed to read response: %v\n", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		logToFile("❌ [API] Error response: %s\n", string(body))

		// Check for authentication/billing errors
		// Status codes: 400 (Bad Request), 401 (Unauthorized), 403 (Forbidden)
		if resp.StatusCode == http.StatusBadRequest ||
			resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden {

			bodyStr := string(body)

			// Check for billing error
			if strings.Contains(bodyStr, "billing") || strings.Contains(bodyStr, "BILLING_NOT_ENABLED") {
				// Notify user about billing requirement
				if sr.app.onError != nil {
					go sr.app.onError("API Billing Required",
						"Speech-to-Text API requires billing to be enabled. Please enable billing at Google Cloud Console, or switch to FREE mode in settings.")
				}
				// Stop recording
				go sr.Stop()
				return "", fmt.Errorf("API billing required: %d", resp.StatusCode)
			}

			// Check if error message contains API key related error
			isAPIKeyError := strings.Contains(bodyStr, "API key") ||
				strings.Contains(bodyStr, "API_KEY_INVALID") ||
				strings.Contains(bodyStr, "INVALID_ARGUMENT")

			if isAPIKeyError {
				// Notify user about invalid API key
				if sr.app.onError != nil {
					go sr.app.onError("Invalid API Key",
						"The Google Cloud API key is invalid or expired. Please check your API key in settings.")
				}
				// Stop recording
				go sr.Stop()
				return "", fmt.Errorf("API authentication failed: invalid or expired API key")
			}
		}

		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response GoogleCloudSpeechResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		logToFile("❌ [API] Failed to parse response: %v\n", err)
		return "", err
	}

	// Extract transcript
	if len(response.Results) > 0 && len(response.Results[0].Alternatives) > 0 {
		transcript := response.Results[0].Alternatives[0].Transcript
		confidence := response.Results[0].Alternatives[0].Confidence

		logToFile("✅ [API] Success! Transcript: '%s' (confidence: %.2f)\n", transcript, confidence)

		// Update usage stats
		go sr.updateUsageStats(len(audioData))

		return transcript, nil
	}

	logToFile("⚠️  [API] No results in response (probably silence/noise)\n")
	return "", fmt.Errorf("no recognition results")
}

// recognizeWithFreeMode uses Google Web Speech API (same as Android and Python's speech_recognition library)
func (sr *SpeechRecognizer) recognizeWithFreeMode(audioData []byte, language string) (string, error) {
	logToFile("📤 [FREE] Using Google Web Speech API (no API key required)\n")
	logToFile("📤 [FREE] Audio size: %d bytes (%.2f seconds)\n", len(audioData), float64(len(audioData))/(16000*2))
	logToFile("📤 [FREE] Language: %s\n", language)

	// Google Web Speech API has a duration limit of ~15 seconds
	// If audio is longer, split it into chunks and process separately
	const maxDuration = 15.0         // seconds
	const bytesPerSecond = 16000 * 2 // 16kHz, 2 bytes per sample
	maxBytes := int(maxDuration * bytesPerSecond)

	if len(audioData) > maxBytes {
		logToFile("⚠️  [FREE] Audio too long (%.1f sec), splitting into chunks...\n", float64(len(audioData))/bytesPerSecond)

		// Process only the first 15 seconds for now
		// In a future enhancement, we could process multiple chunks and concatenate results
		audioData = audioData[:maxBytes]
		logToFile("📤 [FREE] Using first %.1f seconds only\n", maxDuration)
	}

	// Google Web Speech API endpoint (same as used by speech_recognition Python library)
	// This is the public, free API used by Android devices and Chrome browser
	url := fmt.Sprintf("https://www.google.com/speech-api/v2/recognize?client=chromium&lang=%s&key=AIzaSyBOti4mM-6x9WDnZIjIeyEU21OpBXqWBgw", language)

	logToFile("📤 [FREE] Sending POST request to Web Speech API (raw PCM)...\n")

	// Create HTTP request with proper headers
	// Send raw PCM data directly (no WAV header needed with L16 content type)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(audioData))
	if err != nil {
		logToFile("❌ [FREE] Failed to create request: %v\n", err)
		return "", err
	}

	// Set headers - L16 (Linear PCM) format with rate specification
	// This tells the API we're sending raw 16-bit PCM data at 16kHz
	req.Header.Set("Content-Type", "audio/l16; rate=16000")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	// Send request
	resp, err := sr.httpClient.Do(req.WithContext(sr.requestCtx))
	if err != nil {
		logToFile("❌ [FREE] Request failed: %v\n", err)
		return "", err
	}
	defer resp.Body.Close()

	logToFile("📥 [FREE] Response status: %d\n", resp.StatusCode)

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logToFile("❌ [FREE] Failed to read response: %v\n", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		logToFile("❌ [FREE] Error response: %s\n", string(body))
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	logToFile("📥 [FREE] Response body: %s\n", string(body))

	// Parse response - Google Web Speech API returns multiple JSON objects separated by newlines
	// Format: {"result":[]}
	//         {"result":[{"alternative":[{"transcript":"text","confidence":0.95}],"final":true}],"result_index":0}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{\"result\":[]}" {
			continue
		}

		var result struct {
			Result []struct {
				Alternative []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternative"`
				Final bool `json:"final"`
			} `json:"result"`
		}

		err = json.Unmarshal([]byte(line), &result)
		if err != nil {
			logToFile("⚠️  [FREE] Failed to parse line: %s\n", line)
			continue
		}

		if len(result.Result) > 0 && len(result.Result[0].Alternative) > 0 {
			transcript := result.Result[0].Alternative[0].Transcript
			confidence := result.Result[0].Alternative[0].Confidence

			logToFile("✅ [FREE] Success! Transcript: '%s' (confidence: %.2f)\n", transcript, confidence)
			return transcript, nil
		}
	}

	logToFile("⚠️  [FREE] No results in response (probably silence/noise)\n")
	return "", fmt.Errorf("no recognition results")
}

// convertToFLAC converts raw PCM audio data to FLAC format
// FLAC is Google's preferred format for the Web Speech API
func (sr *SpeechRecognizer) convertToFLAC(pcmData []byte) ([]byte, error) {
	// For simplicity, we'll use WAV format wrapped as FLAC
	// Google's Web Speech API actually accepts both WAV and FLAC
	// But it expects the audio/x-flac content type

	// Convert to WAV first (which is essentially PCM with header)
	return sr.convertToWAV(pcmData)
}

// convertToWAV converts raw PCM audio data to WAV format
func (sr *SpeechRecognizer) convertToWAV(pcmData []byte) ([]byte, error) {
	// WAV header structure
	var buf bytes.Buffer

	// RIFF header
	buf.WriteString("RIFF")
	// File size - 8 (will be filled at the end)
	fileSize := uint32(36 + len(pcmData))
	buf.Write([]byte{
		byte(fileSize), byte(fileSize >> 8), byte(fileSize >> 16), byte(fileSize >> 24),
	})
	buf.WriteString("WAVE")

	// fmt chunk
	buf.WriteString("fmt ")
	buf.Write([]byte{16, 0, 0, 0}) // Chunk size
	buf.Write([]byte{1, 0})        // Audio format (1 = PCM)
	buf.Write([]byte{1, 0})        // Number of channels (1 = mono)

	// Sample rate (16000 Hz)
	sampleRate := uint32(16000)
	buf.Write([]byte{
		byte(sampleRate), byte(sampleRate >> 8), byte(sampleRate >> 16), byte(sampleRate >> 24),
	})

	// Byte rate (SampleRate * NumChannels * BitsPerSample/8)
	byteRate := uint32(16000 * 1 * 2)
	buf.Write([]byte{
		byte(byteRate), byte(byteRate >> 8), byte(byteRate >> 16), byte(byteRate >> 24),
	})

	// Block align (NumChannels * BitsPerSample/8)
	buf.Write([]byte{2, 0})

	// Bits per sample
	buf.Write([]byte{16, 0})

	// data chunk
	buf.WriteString("data")
	dataSize := uint32(len(pcmData))
	buf.Write([]byte{
		byte(dataSize), byte(dataSize >> 8), byte(dataSize >> 16), byte(dataSize >> 24),
	})

	// PCM data
	buf.Write(pcmData)

	return buf.Bytes(), nil
}

// updateUsageStats updates API usage statistics
func (sr *SpeechRecognizer) updateUsageStats(audioDataLen int) {
	// Estimate audio duration (2 bytes per sample, 16kHz)
	audioDuration := float64(audioDataLen) / (2 * 16000)

	// Calculate cost for this request
	// Batch API pricing: $0.006 per 15 seconds = $0.024 per minute
	costPerMinute := 0.024
	estimatedCost := (audioDuration / 60.0) * costPerMinute

	logToFile("💰 [API] Audio: %.2fs, Estimated cost: $%.6f\n", audioDuration, estimatedCost)

	if err := sr.app.AddUsage(1, audioDuration); err != nil {
		fmt.Printf("Error saving usage stats: %v\n", err)
	}
}

// ProcessText processes recognized text with commands and transformations
// Returns: (processedText string, deleteCount int)
// deleteCount > 0 means this is a delete command that should delete N words from history
func (sr *SpeechRecognizer) ProcessText(text string, language string) (string, int) {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	if len(words) == 0 {
		return "", 0
	}

	// Load delete commands
	deleteCommands, err := sr.app.LoadDeleteCommands()
	if err != nil {
		fmt.Printf("⚠️ Error loading delete commands: %v\n", err)
		// Use default fallback
		deleteCommands = map[string]map[string]string{
			"en": {
				"szusi":  "buffer",
				"sushi":  "buffer",
				"vegeta": "ctrl_backspace",
				"goku":   "ctrl_alt_backspace",
			},
			"hu": {
				"szusi":  "buffer",
				"szushi": "buffer",
				"sushi":  "buffer",
				"vegeta": "ctrl_backspace",
				"goku":   "ctrl_alt_backspace",
			},
		}
	}

	// Get delete commands for current language
	langDeleteCommands := deleteCommands[language]
	if langDeleteCommands == nil {
		// Fallback to English if no commands for current language
		langDeleteCommands = deleteCommands["en"]
	}

	// Count ALL delete commands in the text
	// e.g., "goku goku goku" = 3 delete commands = delete 3 words
	deleteCount := 0
	for _, word := range words {
		if _, ok := langDeleteCommands[word]; ok {
			deleteCount++
			fmt.Printf("🗑️  Delete command detected: '%s'\n", word)
		}
	}

	if deleteCount > 0 {
		fmt.Printf("🗑️  Total delete count: %d\n", deleteCount)
		return "", deleteCount
	}

	// Load punctuation commands
	punctuationCommands, err := sr.app.LoadPunctuationCommands()
	if err == nil {
		if langCommands, ok := punctuationCommands[language]; ok {
			// Check if entire text is a punctuation command
			if punct, ok := langCommands[text]; ok {
				return punct, 0
			}
		}
	}

	// Check for capitalize command
	if words[0] == "capitalize" && len(words) > 1 {
		capitalized := strings.Title(words[1])
		remaining := strings.Join(words[2:], " ")
		if remaining != "" {
			return capitalized + " " + remaining + " ", 0
		}
		return capitalized + " ", 0
	}

	// Return the recognized text as-is (with trailing space for natural typing flow)
	// Google's speech API already handles number recognition appropriately
	return text + " ", 0
}

// streamingRecognitionLoop handles real-time streaming recognition
func (sr *SpeechRecognizer) streamingRecognitionLoop() {
	settings := sr.app.GetSettings()

	// Validate API key
	if settings.GoogleAPIKey == "" {
		logToFile("❌ ERROR: API key is required for streaming mode\n")
		sr.Stop()
		return
	}

	// Create streaming recognizer
	streamingRecognizer, err := NewStreamingRecognizer(sr.app, settings.GoogleAPIKey)
	if err != nil {
		logToFile("❌ Failed to create streaming recognizer: %v\n", err)
		sr.Stop()
		return
	}
	sr.streamingRecognizer = streamingRecognizer

	// Start streaming recognizer
	err = streamingRecognizer.Start()
	if err != nil {
		logToFile("❌ Failed to start streaming recognizer: %v\n", err)
		sr.Stop()
		return
	}

	// Start audio recording
	err = sr.app.audioCapture.StartRecording(settings.SilenceTimeoutSeconds)
	if err != nil {
		logToFile("❌ Failed to start audio recording: %v\n", err)
		streamingRecognizer.Stop()
		sr.Stop()
		return
	}

	logToFile("✅ Streaming mode active - sending audio continuously...\n")

	// Stream audio continuously (every 50ms for faster response)
	// Faster streaming = more responsive recognition (like Android)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sr.stopChannel:
			logToFile("🛑 Stopping streaming mode...\n")
			ticker.Stop()
			sr.app.audioCapture.StopRecording()
			streamingRecognizer.Stop()
			return

		case <-ticker.C:
			// Atomically transfer buffer ownership to the streaming sender.
			audioData := sr.app.audioCapture.GetAndClearAudioData()

			// Send even small chunks to ensure low latency
			// Google's API can handle frequent small chunks
			if len(audioData) > 0 {
				// Send to streaming recognizer
				err := streamingRecognizer.SendAudio(audioData)
				if err != nil {
					logToFile("⚠️  Error sending audio to stream: %v\n", err)
				}

			}
		}
	}
}

// executeDeleteAction executes a delete action based on the command type
func (sr *SpeechRecognizer) executeDeleteAction(action string, originalText string) {
	if sr.app.keyboard == nil {
		fmt.Println("⚠️  Keyboard simulator not available")
		return
	}

	switch action {
	case "buffer":
		// Delete using buffer (word history based deletion)
		// This is the existing DeleteLastWord functionality
		err := sr.app.keyboard.DeleteLastWord()
		if err != nil {
			fmt.Printf("❌ Error executing buffer delete: %v\n", err)
		} else {
			fmt.Printf("🗑️  Deleted last word (buffer/szusi)\n")
		}

	case "ctrl_backspace":
		// Press Ctrl+Backspace ONCE to delete the previous word
		// Note: The command word itself is NOT typed (ProcessText returns "", 0)
		// so we only need to delete ONE word (the target word)
		err := sr.app.keyboard.PressCtrlBackspace()
		if err != nil {
			fmt.Printf("❌ Error executing Ctrl+Backspace: %v\n", err)
		} else {
			fmt.Println("🗑️  Executed Ctrl+Backspace (vegeta - deleted previous word)")
		}

	case "ctrl_alt_backspace":
		// Press Ctrl+Alt+Backspace ONCE to delete the line/content
		// Note: The command word itself is NOT typed (ProcessText returns "", 0)
		// so we only need to delete ONE line (the target line)
		err := sr.app.keyboard.PressCtrlAltBackspace()
		if err != nil {
			fmt.Printf("❌ Error executing Ctrl+Alt+Backspace: %v\n", err)
		} else {
			fmt.Println("🗑️  Executed Ctrl+Alt+Backspace (goku - deleted line)")
		}

	default:
		fmt.Printf("⚠️  Unknown delete action: %s\n", action)
	}
}
