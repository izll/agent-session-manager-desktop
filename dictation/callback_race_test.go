package dictation

import (
	"sync"
	"testing"
)

func TestAppServiceCallbacksCanBeReplacedWhileNotifying(t *testing.T) {
	app := &AppService{}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				app.SetStateChangeCallback(func(bool) {})
				app.SetErrorCallback(func(string, string) {})
				app.SetUploadingCallback(func(bool) {})
				app.SetPopupDictateCallback(func() {})
				app.SetVoiceLevelCallback(func(float64) {})
				app.SetInterimTextCallback(func(string) {})
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				app.NotifyInterimText("text")
				app.NotifyUploading(true)
				app.ReportError("title", "message")
				_ = app.stateChangeCallback()
				_ = app.popupDictateCallback()
				_ = app.voiceLevelCallback()
			}
		}()
	}
	wg.Wait()
}

func TestAppServiceInvokesCallbackOutsideCallbackLock(t *testing.T) {
	app := &AppService{}
	app.SetInterimTextCallback(func(string) {
		app.SetInterimTextCallback(nil)
	})
	done := make(chan struct{})
	go func() {
		app.NotifyInterimText("text")
		close(done)
	}()
	<-done
}
