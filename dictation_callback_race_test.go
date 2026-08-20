package main

import (
	"sync"
	"testing"
)

func TestDictationServiceCallbacksCanBeReplacedWhileNotifying(t *testing.T) {
	service := NewDictationService()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				service.SetStateChangeCallback(func(bool) {})
				service.SetErrorCallback(func(string, string) {})
				service.SetVoiceLevelCallback(func(float64) {})
				service.SetInterimTextCallback(func(string) {})
				service.SetBufferTextCallback(func(string) {})
				service.SetFieldTextCallback(func(string) {})
				service.SetFieldDeleteCallback(func(int) {})
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				if callback := service.stateChangeCallback(); callback != nil {
					callback(true)
				}
				if callback := service.errorCallback(); callback != nil {
					callback("title", "message")
				}
				if callback := service.voiceLevelCallback(); callback != nil {
					callback(0.5)
				}
				if callback := service.interimTextCallback(); callback != nil {
					callback("text")
				}
				if callback := service.bufferTextCallback(); callback != nil {
					callback("buffer")
				}
				service.fieldHandler.AppendText("field")
				service.fieldHandler.DeleteChars(1)
			}
		}()
	}
	wg.Wait()
}

func TestDictationServiceCallbackCanReplaceItself(t *testing.T) {
	service := NewDictationService()
	service.SetBufferTextCallback(func(string) {
		service.SetBufferTextCallback(nil)
	})
	callback := service.bufferTextCallback()
	done := make(chan struct{})
	go func() {
		callback("text")
		close(done)
	}()
	<-done
}
