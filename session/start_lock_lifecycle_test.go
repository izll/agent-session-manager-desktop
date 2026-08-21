package session

import (
	"sync"
	"testing"
)

func TestSessionStartLocksAreReleasedWithoutSplittingWaiters(t *testing.T) {
	const workers = 32
	var wg sync.WaitGroup
	inside := 0
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := lockSessionStart("same-session")
			inside++
			if inside != 1 {
				t.Errorf("%d concurrent holders entered session start", inside)
			}
			inside--
			unlock()
		}()
	}
	wg.Wait()
	startLocks.Lock()
	defer startLocks.Unlock()
	if len(startLocks.entries) != 0 {
		t.Fatalf("released session start locks retained %d keys", len(startLocks.entries))
	}
}
