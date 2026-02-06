package dictation

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"google.golang.org/api/option"
)

// StreamingRecognizer handles real-time streaming speech recognition using Google Cloud Speech API
type StreamingRecognizer struct {
	app                      *AppService
	client                   *speech.Client
	stream                   speechpb.Speech_StreamingRecognizeClient
	isRunning                bool
	stopChan                 chan bool
	audioChan                chan []byte
	mu                       sync.Mutex
	ctx                      context.Context
	cancel                   context.CancelFunc
	lastFinalText            string // Track cumulative finalized text
	lastInterimText          string // Last interim for quick typing corrections
	typedTextOnScreen        string // EXACT text currently on screen (PROCESSED - with punctuation substitutions)
	originalTranscriptOnScreen string // Original transcript (BEFORE processing - for comparison with new interim)
}

// NewStreamingRecognizer creates a new streaming recognizer
func NewStreamingRecognizer(app *AppService, apiKey string) (*StreamingRecognizer, error) {
	ctx := context.Background()

	// Create client with API key
	client, err := speech.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create speech client: %w", err)
	}

	// Create context with cancellation for this recognizer
	streamCtx, cancel := context.WithCancel(ctx)

	return &StreamingRecognizer{
		app:       app,
		client:    client,
		isRunning: false,
		stopChan:  make(chan bool, 1),
		audioChan: make(chan []byte, 200), // Larger buffer for faster streaming (200 chunks)
		ctx:       streamCtx,
		cancel:    cancel,
	}, nil
}

// Start begins streaming recognition
func (sr *StreamingRecognizer) Start() error {
	sr.mu.Lock()
	if sr.isRunning {
		sr.mu.Unlock()
		return fmt.Errorf("streaming recognizer already running")
	}
	sr.isRunning = true
	sr.mu.Unlock()

	logToFile("🌊 Starting streaming recognition...\n")

	// Start the streaming goroutine
	go sr.streamingLoop()

	return nil
}

// Stop stops streaming recognition
func (sr *StreamingRecognizer) Stop() {
	sr.mu.Lock()
	if !sr.isRunning {
		sr.mu.Unlock()
		return
	}
	sr.isRunning = false
	sr.mu.Unlock()

	logToFile("🛑 Stopping streaming recognition...\n")

	// Signal stop
	select {
	case sr.stopChan <- true:
	default:
	}

	// Cancel context
	sr.cancel()

	// Close audio channel
	close(sr.audioChan)

	// Close client
	if sr.client != nil {
		sr.client.Close()
	}

	logToFile("✅ Streaming recognition stopped\n")
}

// SendAudio sends audio data to the streaming recognizer
func (sr *StreamingRecognizer) SendAudio(audioData []byte) error {
	sr.mu.Lock()
	isRunning := sr.isRunning
	sr.mu.Unlock()

	if !isRunning {
		return fmt.Errorf("streaming recognizer not running")
	}

	// Send to audio channel (non-blocking)
	select {
	case sr.audioChan <- audioData:
		return nil
	default:
		logToFile("⚠️  Audio channel full, dropping audio chunk\n")
		return fmt.Errorf("audio channel full")
	}
}

// streamingLoop is the main streaming recognition loop
func (sr *StreamingRecognizer) streamingLoop() {
	defer func() {
		sr.mu.Lock()
		sr.isRunning = false
		sr.mu.Unlock()
	}()

	settings := sr.app.GetSettings()

	// Load speech context
	speechContext, err := sr.app.LoadSpeechContext()
	if err != nil {
		logToFile("⚠️  Failed to load speech context: %v\n", err)
	}

	// Prepare streaming config
	config := &speechpb.RecognitionConfig{
		Encoding:                   speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:            16000,
		LanguageCode:               settings.Language,
		EnableAutomaticPunctuation: true,
		Model:                      "default",
	}

	// Add speech context if available
	if phrases, ok := speechContext[settings.Language]; ok && len(phrases) > 0 {
		// Limit to 500 phrases (Google Cloud limit)
		maxPhrases := 500
		if len(phrases) > maxPhrases {
			phrases = phrases[:maxPhrases]
		}
		config.SpeechContexts = []*speechpb.SpeechContext{
			{
				Phrases: phrases,
			},
		}
	}

	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config:              config,
		InterimResults:      true, // Enable interim results for real-time feedback
		SingleUtterance:     false,
		EnableVoiceActivityEvents: false,
	}

	// Create streaming recognize call
	stream, err := sr.client.StreamingRecognize(sr.ctx)
	if err != nil {
		logToFile("❌ Failed to create streaming recognize: %v\n", err)
		return
	}
	sr.stream = stream

	// Send initial streaming config
	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: streamingConfig,
		},
	}); err != nil {
		logToFile("❌ Failed to send streaming config: %v\n", err)
		return
	}

	logToFile("✅ Streaming recognize initialized\n")

	// Start goroutine to receive results
	go sr.receiveResults(stream)

	// Main loop: send audio chunks
	for {
		select {
		case <-sr.stopChan:
			logToFile("📡 Streaming loop stopped by signal\n")
			// Close the send direction
			if err := stream.CloseSend(); err != nil {
				logToFile("⚠️  Error closing stream send: %v\n", err)
			}
			return

		case audioData, ok := <-sr.audioChan:
			if !ok {
				logToFile("📡 Audio channel closed\n")
				return
			}

			// Send audio data to stream
			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: audioData,
				},
			}); err != nil {
				logToFile("❌ Error sending audio to stream: %v\n", err)
				return
			}
		}
	}
}

// receiveResults receives and processes streaming results
func (sr *StreamingRecognizer) receiveResults(stream speechpb.Speech_StreamingRecognizeClient) {
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			logToFile("📡 Stream ended (EOF)\n")
			return
		}
		if err != nil {
			logToFile("❌ Error receiving from stream: %v\n", err)
			return
		}

		// Process results
		if resp.Error != nil {
			logToFile("❌ Recognition error: %v\n", resp.Error)
			continue
		}

		// Log response details
		logToFile("📦 API Response: %d result(s), SpeechEventType: %v\n", len(resp.Results), resp.SpeechEventType)

		// When multiple results come together, they represent parts of the SAME utterance:
		// - Result #0: Stable portion (high stability) - the beginning that's settled
		// - Result #1: New/unstable portion (low stability) - the end that's still changing
		// IMPORTANT: The FULL transcript = result[0] + result[1] (concatenated)
		// Example from Google docs:
		//   Result #0: "to be" (stability: 0.9)
		//   Result #1: " or not to be" (stability: 0.01)
		//   → Full transcript: "to be or not to be"

		// Log all results first
		for idx, result := range resp.Results {
			if len(result.Alternatives) == 0 {
				continue
			}

			transcript := result.Alternatives[0].Transcript
			confidence := result.Alternatives[0].Confidence
			isFinal := result.IsFinal
			stability := result.Stability
			resultEndTime := result.ResultEndTime

			endTimeStr := "N/A"
			if resultEndTime != nil {
				endTimeStr = fmt.Sprintf("%.2fs", float64(resultEndTime.Seconds)+float64(resultEndTime.Nanos)/1e9)
			}

			if isFinal {
				logToFile("✅ [FINAL #%d] Transcript: '%s' (confidence: %.2f, endTime: %s)\n",
					idx, transcript, confidence, endTimeStr)
			} else {
				logToFile("⏳ [INTERIM #%d] Transcript: '%s' (stability: %.2f, endTime: %s)\n",
					idx, transcript, stability, endTimeStr)
			}
		}

		// Process the response
		// If there are results, we need to combine them for the full transcript
		if len(resp.Results) > 0 {
			firstResult := resp.Results[0]

			if len(firstResult.Alternatives) == 0 {
				continue
			}

			isFinal := firstResult.IsFinal

			if isFinal {
				// Final results - combine all transcripts
				fullTranscript := ""
				for _, result := range resp.Results {
					if len(result.Alternatives) > 0 {
						fullTranscript += result.Alternatives[0].Transcript
					}
				}
				logToFile("✅ [FINAL COMBINED] Full transcript: '%s'\n", fullTranscript)
				sr.processFinalResult(fullTranscript)
				sr.lastInterimText = ""
			} else {
				// Interim results - combine all transcripts for the full interim
				fullTranscript := ""
				var maxStability float32 = 0.0
				for _, result := range resp.Results {
					if len(result.Alternatives) > 0 {
						fullTranscript += result.Alternatives[0].Transcript
						// Track the highest stability value
						if result.Stability > maxStability {
							maxStability = result.Stability
						}
					}
				}
				logToFile("⏳ [INTERIM COMBINED] Full transcript: '%s' (max stability: %.2f)\n", fullTranscript, maxStability)
				sr.processInterimResult(fullTranscript, maxStability)
			}
		}
	}
}

// processFinalResult handles final recognition results
// NOW we know EXACTLY what's on screen via typedTextOnScreen!
func (sr *StreamingRecognizer) processFinalResult(transcript string) {
	if transcript == "" {
		return
	}

	// Update last speech time
	sr.app.UpdateLastSpeechTime()

	// Trim whitespace
	transcript = strings.TrimSpace(transcript)

	logToFile("📋 FINAL: '%s'\n", transcript)
	logToFile("📺 Screen has (typed): '%s', (original): '%s'\n", sr.typedTextOnScreen, sr.originalTranscriptOnScreen)

	// POPUP MODE: No interim text was typed to PTY, so just type the full final text directly
	// This completely avoids the backspace correction issues in terminal environments
	if sr.app.keyboard != nil && sr.app.keyboard.IsPopupMode() && sr.typedTextOnScreen == "" {
		settings := sr.app.GetSettings()
		processedText, deleteCount := sr.app.speechRecognizer.ProcessText(transcript, settings.Language)

		if deleteCount > 0 {
			logToFile("🗑️  [FINAL-POPUP] Delete command: '%s' (count: %d)\n", transcript, deleteCount)
			historySize := sr.app.keyboard.GetHistorySize()
			if historySize > 0 {
				actualDeleteCount := deleteCount
				if actualDeleteCount > historySize {
					actualDeleteCount = historySize
				}
				for i := 0; i < actualDeleteCount; i++ {
					err := sr.app.keyboard.DeleteLastWord()
					if err != nil {
						logToFile("❌ Error deleting word: %v\n", err)
					}
				}
				logToFile("✅ [FINAL-POPUP] Deleted %d word(s)\n", actualDeleteCount)
			}
		} else if processedText != "" {
			err := sr.app.keyboard.TypeText(processedText)
			if err != nil {
				logToFile("❌ [FINAL-POPUP] Error typing: %v\n", err)
			} else {
				logToFile("✅ [FINAL-POPUP] Typed: '%s'\n", processedText)
			}
		}

		// Clear overlay
		sr.app.NotifyInterimText("")

		sr.lastFinalText = transcript
		sr.lastInterimText = ""
		sr.typedTextOnScreen = ""
		sr.originalTranscriptOnScreen = ""
		go sr.updateUsageStats(len(transcript))
		return
	}

	// First, check if this FINAL is a delete command
	settings := sr.app.GetSettings()
	_, deleteCount := sr.app.speechRecognizer.ProcessText(transcript, settings.Language)

	if deleteCount > 0 {
		logToFile("🗑️  [FINAL] Delete command: '%s' (count: %d)\n", transcript, deleteCount)

		// STEP 1: Delete any misrecognized interim words from screen (e.g., "go ku" before "goku" was recognized)
		if sr.typedTextOnScreen != "" {
			typedWords := strings.Fields(sr.typedTextOnScreen)
			logToFile("🧹 [FINAL] Cleaning up %d interim word(s) from screen: '%s'\n", len(typedWords), sr.typedTextOnScreen)
			for i := len(typedWords) - 1; i >= 0; i-- {
				wordToDelete := typedWords[i]
				backspaceCount := len([]rune(wordToDelete)) + 1 // +1 for space
				err := sr.app.keyboard.PressBackspace(backspaceCount)
				if err != nil {
					logToFile("❌ Error cleaning up '%s': %v\n", wordToDelete, err)
				}
			}
		}

		// STEP 2: Execute the actual delete command
		historySize := sr.app.keyboard.GetHistorySize()
		if historySize > 0 {
			actualDeleteCount := deleteCount
			if actualDeleteCount > historySize {
				actualDeleteCount = historySize
			}
			logToFile("🗑️  [FINAL] Deleting %d word(s) from history (size: %d)\n", actualDeleteCount, historySize)

			for i := 0; i < actualDeleteCount; i++ {
				err := sr.app.keyboard.DeleteLastWord()
				if err != nil {
					logToFile("❌ Error deleting word: %v\n", err)
				}
			}
			logToFile("✅ Deleted %d word(s), remaining: %d words\n", actualDeleteCount, sr.app.keyboard.GetHistorySize())
		} else {
			logToFile("⚠️  Keyboard history is empty - delete command ignored\n")
		}

		// Clear screen tracking
		sr.originalTranscriptOnScreen = ""
		sr.typedTextOnScreen = ""
		sr.lastFinalText = transcript

		go sr.updateUsageStats(len(transcript))
		return
	}

	// Check if final matches what's on screen (compare ORIGINAL transcripts, not processed)
	if transcript == sr.originalTranscriptOnScreen {
		logToFile("✅ [FINAL] Perfect match! Screen already has this\n")

		// Add the FINAL words to history for future delete commands
		processedText, _ := sr.app.speechRecognizer.ProcessText(transcript, settings.Language)
		if processedText != "" {
			words := strings.Fields(processedText)
			sr.app.keyboard.AddToHistory(words)
			logToFile("📝 Added %d word(s) to history: %v\n", len(words), words)
		}

		sr.lastFinalText = transcript
		sr.typedTextOnScreen = "" // Reset for next utterance
		sr.originalTranscriptOnScreen = "" // Reset original tracking too

		go sr.updateUsageStats(len(transcript))
		return
	}

	// Check if screen text is PREFIX of final (incomplete)
	if strings.HasPrefix(transcript, sr.originalTranscriptOnScreen) {
		// Type the missing part (based on ORIGINAL transcript)
		missing := transcript[len(sr.originalTranscriptOnScreen):]
		missing = strings.TrimSpace(missing)

		if missing != "" {
			logToFile("📝 [FINAL] Screen has prefix, typing missing: '%s'\n", missing)

			// NEW STRATEGY: In FINAL, we handle delete commands!
			// Since we skipped them in INTERIM, they were never typed, so we process them here.
			settings := sr.app.GetSettings()
			processedText, deleteCount := sr.app.speechRecognizer.ProcessText(missing, settings.Language)

			if deleteCount > 0 {
				// The missing part contains delete commands - execute them now!
				logToFile("🗑️  [FINAL] Delete command detected in missing part (count: %d)\n", deleteCount)

				historySize := sr.app.keyboard.GetHistorySize()
				if historySize > 0 {
					actualDeleteCount := deleteCount
					if actualDeleteCount > historySize {
						actualDeleteCount = historySize
						logToFile("⚠️  Capping delete count from %d to history size %d\n", deleteCount, historySize)
					}

					logToFile("🗑️  Deleting %d word(s) from keyboard history (size: %d)\n", actualDeleteCount, historySize)

					// Delete from both screen AND history
					for i := 0; i < actualDeleteCount; i++ {
						err := sr.app.keyboard.DeleteLastWord()
						if err != nil {
							logToFile("❌ Error deleting word: %v\n", err)
						}
					}

					logToFile("✅ Deleted %d word(s), remaining: %d words\n", actualDeleteCount, sr.app.keyboard.GetHistorySize())
				} else {
					logToFile("⚠️  Keyboard history is empty - delete command ignored\n")
				}
			} else if processedText != "" && sr.app.keyboard != nil {
				// Only type if it's NOT a delete command
				if !strings.HasPrefix(processedText, " ") {
					processedText = " " + processedText
				}
				err := sr.app.keyboard.TypeText(processedText)
				if err != nil {
					logToFile("❌ Error typing: %v\n", err)
				} else {
					logToFile("✅ Typed missing part: '%s'\n", processedText)
				}
			}
		}
	} else {
		// Screen has something different - Google recognition changed between interim and final!
		// This happens when interim was wrong, but final is correct
		// We need to CORRECT the screen text
		logToFile("⚠️  [FINAL] Screen mismatch - recognition changed!\n")
		logToFile("    Expected: '%s'\n", transcript)
		logToFile("    Have:     '%s'\n", sr.typedTextOnScreen)

		// FIRST: Delete ALL interim text from screen using backspaces
		// Interim words are NOT in history (TypeTextNoHistory was used), so we MUST use backspaces
		if sr.typedTextOnScreen != "" {
			typedWords := strings.Fields(sr.typedTextOnScreen)
			logToFile("🧹 [FINAL] Deleting %d interim word(s) from screen using backspaces\n", len(typedWords))
			for i := len(typedWords) - 1; i >= 0; i-- {
				wordToDelete := typedWords[i]
				backspaceCount := len([]rune(wordToDelete)) + 1 // +1 for space
				err := sr.app.keyboard.PressBackspace(backspaceCount)
				if err != nil {
					logToFile("❌ Error deleting '%s': %v\n", wordToDelete, err)
				}
			}
		}

		// THEN: Type the FULL correct text from FINAL
		processedText, deleteCount := sr.app.speechRecognizer.ProcessText(transcript, settings.Language)

		// Handle delete commands
		if deleteCount > 0 && sr.app.keyboard != nil {
			historySize := sr.app.keyboard.GetHistorySize()
			actualDeleteCount := deleteCount
			if actualDeleteCount > historySize {
				actualDeleteCount = historySize
			}
			for i := 0; i < actualDeleteCount; i++ {
				sr.app.keyboard.DeleteLastWord()
			}
		}

		if processedText != "" {
			err := sr.app.keyboard.TypeText(processedText)
			if err != nil {
				logToFile("❌ Error typing corrected text: %v\n", err)
			} else {
				logToFile("✅ Typed corrected text: '%s'\n", processedText)
			}
		}
	}

	// Update state
	sr.lastFinalText = transcript
	sr.typedTextOnScreen = "" // Reset for next utterance
	sr.originalTranscriptOnScreen = "" // Reset original tracking too

	// NOTE: We do NOT clear keyboard history here anymore!
	// This allows delete commands in the next utterance to delete words from this utterance.
	// The keyboard history will be cleared when we start typing NEW regular words (not delete commands).
	logToFile("📝 Keyboard history NOT cleared - delete commands can still delete from this utterance\n")

	// Update usage stats
	go sr.updateUsageStats(len(transcript))
}

// processInterimResult handles interim (temporary) results with real-time typing and correction
// The transcript parameter is the FULL combined transcript from all results
// We compare it directly with what's on screen and make corrections
// The stability parameter indicates how confident Google is (0.0-1.0)
func (sr *StreamingRecognizer) processInterimResult(transcript string, stability float32) {
	// Trim leading/trailing whitespace
	transcript = strings.TrimSpace(transcript)

	if transcript == "" {
		return
	}

	// CRITICAL: Skip if this is the same interim we already processed
	// This prevents duplicate delete command execution when Google sends the same interim multiple times with different stability
	if transcript == sr.lastInterimText {
		logToFile("⏭️  [INTERIM] Skipping duplicate interim (already processed): '%s'\n", transcript)
		return
	}

	// POPUP MODE: Don't type interim text to PTY - show in UI overlay instead
	// This prevents over-deletion caused by PTY latency in terminal environments
	if sr.app.keyboard != nil && sr.app.keyboard.IsPopupMode() {
		sr.lastInterimText = transcript
		// Process for display (apply punctuation commands etc.)
		settings := sr.app.GetSettings()
		processedText, deleteCount := sr.app.speechRecognizer.ProcessText(transcript, settings.Language)
		if deleteCount > 0 {
			sr.app.NotifyInterimText("[törlés]")
		} else {
			sr.app.NotifyInterimText(strings.TrimSpace(processedText))
		}
		logToFile("⏳ [INTERIM-POPUP] '%s' → overlay (stability: %.2f)\n", transcript, stability)
		return
	}

	// Split into words for comparison
	// IMPORTANT: Compare ORIGINAL transcripts (before punctuation processing)
	// because "vessző" becomes "," on screen, but we need to compare with "vessző"
	newWords := strings.Fields(transcript)
	screenOriginalWords := strings.Fields(sr.originalTranscriptOnScreen)

	// Find common prefix between original screen transcript and new interim
	// CRITICAL FIX: When comparing words, treat ALL delete commands as equivalent!
	// Google sends "sushi", "szusi", "töröl" interchangeably - they're all the SAME delete command
	settings := sr.app.GetSettings()
	commonPrefixLength := 0
	for i := 0; i < len(screenOriginalWords) && i < len(newWords); i++ {
		screenWord := screenOriginalWords[i]
		newWord := newWords[i]

		// Check if BOTH words are delete commands
		_, screenDeleteCount := sr.app.speechRecognizer.ProcessText(screenWord, settings.Language)
		_, newDeleteCount := sr.app.speechRecognizer.ProcessText(newWord, settings.Language)

		// If both are delete commands, consider them equal (even if spelling differs)
		if screenDeleteCount > 0 && newDeleteCount > 0 {
			commonPrefixLength++
		} else if screenWord == newWord {
			// Regular words must match exactly
			commonPrefixLength++
		} else {
			break
		}
	}

	// What new words need to be typed?
	wordsToType := newWords[commonPrefixLength:]

	// STABILITY-BASED STRATEGY:
	// - If stability >= 0.9: Google is very confident → process delete commands immediately
	// - If stability < 0.9: Google might change its mind → skip delete commands, wait for FINAL
	//
	// This provides a good balance between speed (fast deletion when confident)
	// and reliability (no false deletions when uncertain)
	containsDeleteCommand := false
	for _, word := range wordsToType {
		_, deleteCount := sr.app.speechRecognizer.ProcessText(word, settings.Language)
		if deleteCount > 0 {
			containsDeleteCommand = true
			break
		}
	}

	// If this interim contains delete commands but stability is LOW, DON'T process it!
	// Wait for the FINAL result to handle delete commands properly
	if containsDeleteCommand && stability < 0.9 {
		logToFile("⏸️  [INTERIM] Contains delete commands but stability too low (%.2f < 0.9) - SKIPPING (will handle in FINAL)\n", stability)
		return
	}

	// How many words from screen need to be deleted?
	wordsToDelete := len(screenOriginalWords) - commonPrefixLength

	logToFile("⏳ [INTERIM] '%s' (stability: %.2f, screen original: '%s', screen typed: '%s', common: %d, delete: %d, type: %d, hasDeleteCmd: %v)\n",
		transcript, stability, sr.originalTranscriptOnScreen, sr.typedTextOnScreen, commonPrefixLength, wordsToDelete, len(wordsToType), containsDeleteCommand)

	// Delete outdated words from screen using backspaces (NOT DeleteLastWord - history is not used for interim)
	if wordsToDelete > 0 {
		logToFile("🔄 [INTERIM] Deleting %d word(s) from screen using backspaces\n", wordsToDelete)

		// Get the typed words to delete (from typedTextOnScreen, not original)
		typedWords := strings.Fields(sr.typedTextOnScreen)

		if sr.app.keyboard != nil && len(typedWords) >= wordsToDelete {
			// Delete words from end, using backspace for each character + space
			for i := 0; i < wordsToDelete; i++ {
				wordIdx := len(typedWords) - 1 - i
				if wordIdx >= 0 {
					wordToDelete := typedWords[wordIdx]
					backspaceCount := len([]rune(wordToDelete)) + 1 // +1 for space
					logToFile("🔙 Deleting word '%s' (%d backspaces)\n", wordToDelete, backspaceCount)
					err := sr.app.keyboard.PressBackspace(backspaceCount)
					if err != nil {
						logToFile("❌ Error deleting word with backspace: %v\n", err)
					}
				}
			}
		}

		// Update screen tracking: remove deleted words from BOTH tracking variables
		if len(screenOriginalWords) >= wordsToDelete {
			screenOriginalWords = screenOriginalWords[:len(screenOriginalWords)-wordsToDelete]
			sr.originalTranscriptOnScreen = strings.Join(screenOriginalWords, " ")

			// Also update typed text tracking by re-processing the remaining original words
			// This ensures typedTextOnScreen stays in sync
			if sr.originalTranscriptOnScreen != "" {
				sr.typedTextOnScreen, _ = sr.app.speechRecognizer.ProcessText(sr.originalTranscriptOnScreen, settings.Language)
				sr.typedTextOnScreen = strings.TrimSpace(sr.typedTextOnScreen)
			} else {
				sr.typedTextOnScreen = ""
			}
		} else {
			sr.originalTranscriptOnScreen = ""
			sr.typedTextOnScreen = ""
		}
	}

	// Type new/corrected words - PROCESS WORD-BY-WORD!
	if len(wordsToType) > 0 && sr.app.keyboard != nil {
		// settings already defined above

		// Process each word separately to handle mixed delete commands and regular words
		for _, word := range wordsToType {
			// Process this single word
			processedWord, deleteCount := sr.app.speechRecognizer.ProcessText(word, settings.Language)

			// Handle delete commands
			if deleteCount > 0 {
				logToFile("🗑️  [INTERIM] Delete command detected: '%s' (count: %d, stability: %.2f ✅ HIGH)\n", word, deleteCount, stability)

				// STEP 1: Delete any misrecognized words from screen AND history
				// These are words that Google misrecognized before recognizing the delete command.
				// They are on screen AND in keyboard history (added by TypeText).
				//
				// Example: "SUP" → "sushi"
				//   - "SUP" was typed → screen: "sup", history: [..., "sup"]
				//   - "sushi" detected as delete command
				//   - We need to delete "sup" from BOTH screen AND history
				numMisrecognizedWords := 0
				if sr.originalTranscriptOnScreen != "" {
					screenWords := strings.Fields(sr.originalTranscriptOnScreen)
					numMisrecognizedWords = len(screenWords)
					if numMisrecognizedWords > 0 {
						logToFile("🧹 Deleting %d misrecognized word(s) from screen AND history\n", numMisrecognizedWords)
						for i := 0; i < numMisrecognizedWords; i++ {
							wordToDelete := screenWords[numMisrecognizedWords-1-i] // Delete in reverse order
							backspaceCount := len([]rune(wordToDelete)) + 1        // +1 for space
							err := sr.app.keyboard.PressBackspace(backspaceCount)
							if err != nil {
								logToFile("❌ Error deleting '%s' from screen: %v\n", wordToDelete, err)
							}
						}
						// Remove these words from history too (without backspace - already deleted from screen)
						sr.app.keyboard.RemoveFromHistory(numMisrecognizedWords)
						logToFile("🧹 Removed %d misrecognized word(s) from history\n", numMisrecognizedWords)
					}
				}

				// STEP 2: Now execute the actual delete command (delete previous words)
				// This is SEPARATE from the misrecognized words cleanup above!
				historySize := sr.app.keyboard.GetHistorySize()
				if historySize > 0 && deleteCount > 0 {
					actualDeleteCount := deleteCount
					if actualDeleteCount > historySize {
						actualDeleteCount = historySize
						logToFile("⚠️  Capping delete count from %d to history size %d\n", deleteCount, historySize)
					}
					logToFile("🗑️  Executing delete command: deleting %d word(s) from screen AND history (size: %d)\n",
						actualDeleteCount, historySize)

					// Delete from BOTH screen AND history using DeleteLastWord
					for i := 0; i < actualDeleteCount; i++ {
						err := sr.app.keyboard.DeleteLastWord()
						if err != nil {
							logToFile("❌ Error deleting word: %v\n", err)
						}
					}

					logToFile("✅ Deleted %d word(s), remaining: %d words\n", actualDeleteCount, sr.app.keyboard.GetHistorySize())
				} else if deleteCount > 0 {
					logToFile("⚠️  Keyboard history is empty - delete command ignored\n")
				}

				// Clear screen tracking
				sr.originalTranscriptOnScreen = ""
				sr.typedTextOnScreen = ""
				logToFile("📺 Screen tracking cleared\n")
			} else if processedWord != "" {
				// This is a regular word (or punctuation command) - type it

				// NOTE: We use TypeTextNoHistory here because interim words may change.
				// The FINAL result will add the correct words to history.

				// Add space after processed word (to separate words)
				if !strings.HasSuffix(processedWord, " ") {
					processedWord = processedWord + " "
				}

				// Use TypeTextNoHistory for interim - we'll add to history only in FINAL
				err := sr.app.keyboard.TypeTextNoHistory(processedWord)
				if err != nil {
					logToFile("❌ Error typing interim word: %v\n", err)
				} else {
					logToFile("⚡ Typed interim word: '%s' (original: '%s')\n", processedWord, word)

					// Update BOTH tracking variables
					// 1. originalTranscriptOnScreen: track the original word (for comparison)
					if sr.originalTranscriptOnScreen == "" {
						sr.originalTranscriptOnScreen = word
					} else {
						sr.originalTranscriptOnScreen = sr.originalTranscriptOnScreen + " " + word
					}

					// 2. typedTextOnScreen: track what's ACTUALLY on screen (processed)
					if sr.typedTextOnScreen == "" {
						sr.typedTextOnScreen = strings.TrimSpace(processedWord)
					} else {
						sr.typedTextOnScreen = sr.typedTextOnScreen + " " + strings.TrimSpace(processedWord)
					}

					logToFile("📺 Screen now has: '%s' (original: '%s')\n", sr.typedTextOnScreen, sr.originalTranscriptOnScreen)
				}
			}
		}
	}

	// Update last interim text for reference
	sr.lastInterimText = transcript
}

// updateUsageStats updates API usage statistics
func (sr *StreamingRecognizer) updateUsageStats(audioDataLen int) {
	stats, err := sr.app.LoadUsageStats()
	if err != nil {
		logToFile("⚠️  Error loading usage stats: %v\n", err)
		return
	}

	stats.TotalRequests++
	// Estimate audio duration (streaming sends in small chunks, so we accumulate)
	audioDuration := float64(audioDataLen) / 16000.0 // Rough estimate
	stats.TotalAudioSeconds += audioDuration

	// Calculate cost for this request
	// Streaming API pricing: ~$0.009 per 15 seconds = $0.036 per minute
	costPerMinute := 0.036
	estimatedCost := (audioDuration / 60.0) * costPerMinute

	logToFile("💰 [STREAMING] Audio: %.2fs, Estimated cost: $%.6f (Total: %.1fs, $%.4f)\n",
		audioDuration, estimatedCost, stats.TotalAudioSeconds, stats.TotalAudioSeconds/60.0*costPerMinute)

	err = sr.app.SaveUsageStats(*stats)
	if err != nil {
		logToFile("⚠️  Error saving usage stats: %v\n", err)
	}
}

// IsRunning returns whether streaming is active
func (sr *StreamingRecognizer) IsRunning() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.isRunning
}
