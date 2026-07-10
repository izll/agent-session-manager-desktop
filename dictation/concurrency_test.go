package dictation

import (
	"bytes"
	"context"
	"sync"
	"testing"
)

func TestStreamingRecognizerConcurrentSendAndStop(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		ctx, cancel := context.WithCancel(context.Background())
		sr := &StreamingRecognizer{
			isRunning: true,
			audioChan: make(chan []byte, 4),
			ctx:       ctx,
			cancel:    cancel,
		}

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = sr.SendAudio([]byte{1, 2, 3, 4})
			}()
		}
		sr.Stop()
		wg.Wait()
	}
}

func TestAudioDataOwnership(t *testing.T) {
	ac := NewAudioCapture()
	ac.audioBuffer.Write([]byte{1, 2, 3, 4})

	data := ac.GetAndClearAudioData()
	ac.audioBuffer.Write([]byte{9, 9, 9, 9})
	if !bytes.Equal(data, []byte{1, 2, 3, 4}) {
		t.Fatalf("returned chunk changed after buffer reuse: %v", data)
	}

	if got := ac.GetAndClearAudioDataIfAtLeast(10); got != nil {
		t.Fatalf("short buffer was unexpectedly consumed: %v", got)
	}
	if got := ac.GetAudioData(); !bytes.Equal(got, []byte{9, 9, 9, 9}) {
		t.Fatalf("short buffer did not remain intact: %v", got)
	}
}
