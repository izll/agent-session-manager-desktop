package dictation

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSaveSettingsFailureDoesNotChangeLiveSettings(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(homeFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile) // os.UserHomeDir reads this on Windows
	service := &AppService{settings: &Settings{Mode: "free", Language: "en"}}

	err := service.SaveSettings(Settings{Mode: "api", Language: "hu"})
	if err == nil {
		t.Fatal("expected settings persistence failure")
	}
	got := service.GetSettings()
	if got.Mode != "free" || got.Language != "en" {
		t.Fatalf("failed save changed live settings: %+v", got)
	}
}

func TestSaveSettingsDisablesGlobalHotkey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	manager := &HotkeyManagerReal{
		isEnabled: true,
		isRunning: true,
		stopChan:  make(chan bool, 1),
	}
	service := &AppService{
		settings:      &Settings{Enabled: true, Mode: "free"},
		hotkeyManager: manager,
	}
	if err := service.SaveSettings(Settings{Enabled: false, Mode: "free"}); err != nil {
		t.Fatal(err)
	}
	if manager.isEnabled || manager.isRunning {
		t.Fatal("global hotkey remained active after dictation was disabled")
	}
	select {
	case <-manager.stopChan:
	default:
		t.Fatal("hotkey listener was not signalled to stop")
	}
}

func TestSilenceMonitorStopDoesNotRaceOrLeak(t *testing.T) {
	service := &AppService{
		settings:    &Settings{AutoStopOnSilence: true, SilenceTimeoutSeconds: 60},
		isListening: true,
	}
	for i := 0; i < 50; i++ {
		service.mu.Lock()
		service.startSilenceMonitor()
		service.mu.Unlock()
		service.stopSilenceMonitor()
	}
	// Let the monitor goroutines observe their closed, locally captured channel.
	time.Sleep(20 * time.Millisecond)
	service.silenceMu.Lock()
	active := service.silenceCheckActive
	done := service.silenceMonitorDone
	service.silenceMu.Unlock()
	if active || done != nil {
		t.Fatalf("silence monitor still active after stop: active=%v done=%v", active, done != nil)
	}
}

func TestShutdownStopsSilenceMonitor(t *testing.T) {
	service := &AppService{
		settings:    &Settings{AutoStopOnSilence: true, SilenceTimeoutSeconds: 60},
		isListening: true,
	}
	service.mu.Lock()
	service.startSilenceMonitor()
	service.mu.Unlock()

	service.Shutdown()
	if service.IsListening() {
		t.Fatal("service still reports listening after shutdown")
	}
	service.silenceMu.Lock()
	active := service.silenceCheckActive
	service.silenceMu.Unlock()
	if active {
		t.Fatal("silence monitor still active after shutdown")
	}
}

func TestForceProcessSnapshotsRecognizerUnderLock(t *testing.T) {
	service := &AppService{
		settings:         &Settings{Mode: "free"},
		isListening:      true,
		speechRecognizer: &SpeechRecognizer{},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			service.ForceProcessAudio()
		}
	}()
	for i := 0; i < 200; i++ {
		service.mu.Lock()
		service.speechRecognizer = &SpeechRecognizer{}
		service.mu.Unlock()
	}
	<-done
}

func TestAddUsageCrossProcessWorker(t *testing.T) {
	loopsText := os.Getenv("ASMGR_DICTATION_USAGE_WORKER")
	if loopsText == "" {
		t.Skip("worker only")
	}
	loops, err := strconv.Atoi(loopsText)
	if err != nil {
		t.Fatal(err)
	}
	service := &AppService{settings: &Settings{}}
	for i := 0; i < loops; i++ {
		if err := service.AddUsage(1, 0.5); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddUsageSerializesAcrossProcesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	if _, err := getConfigDir(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const workers = 3
	const loops = 30
	commands := make([]*exec.Cmd, workers)
	buffers := make([]bytes.Buffer, workers)
	for i := range commands {
		commands[i] = exec.Command(executable, "-test.run=^TestAddUsageCrossProcessWorker$")
		commands[i].Env = append(os.Environ(), fmt.Sprintf("ASMGR_DICTATION_USAGE_WORKER=%d", loops))
		commands[i].Stdout = &buffers[i]
		commands[i].Stderr = &buffers[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i := range commands {
		if err := commands[i].Wait(); err != nil {
			t.Fatalf("worker %d: %v\n%s", i, err, buffers[i].String())
		}
	}
	service := &AppService{settings: &Settings{}}
	stats, err := service.LoadUsageStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != workers*loops || stats.TotalAudioSeconds != float64(workers*loops)/2 {
		t.Fatalf("usage = %+v, want %d requests and %.1f seconds", stats, workers*loops, float64(workers*loops)/2)
	}
}
