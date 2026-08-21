package session

import (
	"sync"
	"testing"
	"time"
)

// Two starts for one session must not run at the same time. The existence
// check and the new-session that follows it are not atomic, so overlapping
// starts both see "no session" and create one — leaving two servers under a
// single name, which only a manual kill resolves.
func TestLockSessionStartSerialisesSameSession(t *testing.T) {
	const name = "asm_test_serialise"
	var active, maxActive int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := lockSessionStart(name)
			defer unlock()

			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond) // hold it long enough to overlap

			mu.Lock()
			active--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if maxActive != 1 {
		t.Fatalf("%d starts ran concurrently; they must be serialised", maxActive)
	}
}

// Different sessions must not block each other: starting one session should
// never wait on an unrelated one.
func TestLockSessionStartAllowsDifferentSessions(t *testing.T) {
	unlockA := lockSessionStart("asm_test_a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := lockSessionStart("asm_test_b")
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a different session blocked on an unrelated lock")
	}
}

// The mark is what lets a later start wait for a slow registration instead of
// creating a duplicate.
func TestRecentlyStarted(t *testing.T) {
	const name = "asm_test_recent"

	if recentlyStarted(name) {
		t.Fatal("an unknown session must not count as recently started")
	}

	markStarted(name)
	if !recentlyStarted(name) {
		t.Fatal("a session just marked must count as recently started")
	}

	// Past the window it must stop counting, or restarting a session that
	// really is gone would pay the wait forever.
	recentStarts.Store(name, time.Now().Add(-startSettleWindow-time.Second))
	if recentlyStarted(name) {
		t.Fatal("a stale mark must expire")
	}
	if _, ok := recentStarts.Load(name); ok {
		t.Fatal("an expired mark must be dropped, or the map grows forever")
	}
}

// A garbage value must not be trusted as a timestamp.
func TestRecentlyStartedIgnoresBadValue(t *testing.T) {
	const name = "asm_test_bad"
	recentStarts.Store(name, "not a time")
	defer recentStarts.Delete(name)

	if recentlyStarted(name) {
		t.Fatal("a non-time value must not count as a recent start")
	}
}

func TestMarkStartedReapsUnqueriedExpiredSessions(t *testing.T) {
	const oldName = "asm_test_expired_deleted_session"
	const newName = "asm_test_new_session"
	recentStarts.Store(oldName, time.Now().Add(-startSettleWindow-time.Second))
	recentStartsPrune.Lock()
	recentStartsPrune.last = time.Time{}
	recentStartsPrune.Unlock()
	t.Cleanup(func() {
		recentStarts.Delete(oldName)
		recentStarts.Delete(newName)
	})

	markStarted(newName)
	if _, ok := recentStarts.Load(oldName); ok {
		t.Fatal("an expired start mark for a deleted session was retained")
	}
	if !recentlyStarted(newName) {
		t.Fatal("pruning removed the newly published start mark")
	}
}
