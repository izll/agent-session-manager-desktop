package dictation

import (
	"sync"
	"testing"

	hook "github.com/robotn/gohook"
)

func TestHotkeyManagerConcurrentLifecycleAndConfig(t *testing.T) {
	originalStart := startHotkeyHook
	startHotkeyHook = func(...int) chan hook.Event {
		return make(chan hook.Event)
	}
	t.Cleanup(func() { startHotkeyHook = originalStart })

	manager := &HotkeyManagerReal{
		callbacks:        map[string]func(){"toggle": func() {}},
		hotkeyConfigs:    map[string]HotkeyConfig{"toggle": {Key: "d"}},
		lastTriggerTimes: make(map[string]int64),
		stopChan:         make(chan bool, 1),
	}
	const iterations = 100
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if worker%2 == 0 {
					_ = manager.Enable()
					manager.Disable()
				} else {
					_ = manager.UpdateConfig(HotkeyConfig{Ctrl: i%2 == 0, Key: "d"}, "toggle")
				}
			}
		}(worker)
	}
	wg.Wait()
	manager.Disable()
}

func TestHotkeyRecordingModeReadIsSynchronizedWithSettingsSwap(t *testing.T) {
	app := &AppService{settings: &Settings{RecordingMode: "popup"}}
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			app.mu.Lock()
			app.settings = &Settings{RecordingMode: "direct"}
			app.mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = app.popupRecordingMode()
		}
	}()
	wg.Wait()
}
