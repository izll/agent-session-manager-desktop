package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventThrottleSerializesConcurrentCallbacks(t *testing.T) {
	throttle := eventThrottle{interval: 80 * time.Millisecond}
	now := time.Now()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if throttle.allow(now) {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 1 {
		t.Fatalf("concurrent callbacks allowed = %d, want exactly 1", got)
	}
	if !throttle.allow(now.Add(80 * time.Millisecond)) {
		t.Fatal("callback at the throttle boundary was rejected")
	}
}
