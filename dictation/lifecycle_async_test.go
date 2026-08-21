package dictation

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestShutdownDrainsOwnedAsyncWorkAndRejectsLateLaunch(t *testing.T) {
	service := &AppService{settings: &Settings{}}
	started := make(chan struct{})
	release := make(chan struct{})
	if !service.runAsync(func() {
		close(started)
		<-release
	}) {
		t.Fatal("service rejected work before shutdown")
	}
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		service.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned while service-owned work was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after owned work exited")
	}

	var ran atomic.Bool
	if service.runAsync(func() { ran.Store(true) }) {
		t.Fatal("service accepted work after shutdown started")
	}
	time.Sleep(10 * time.Millisecond)
	if ran.Load() {
		t.Fatal("late background work ran after shutdown")
	}
	if err := service.ToggleListening(); err == nil {
		t.Fatal("dictation restarted after shutdown")
	}
}
