package session

import (
	"testing"
	"time"
)

func TestCaptureTargetCachePrunesExpiredDeletedSessions(t *testing.T) {
	now := time.Now()
	captureTargetCache.Store("deleted:0", captureTargetEntry{target: "deleted:0", expires: now.Add(-time.Second)})
	captureTargetCache.Store("active:0", captureTargetEntry{target: "active:0", expires: now.Add(time.Second)})
	captureTargetCachePrune.Lock()
	captureTargetCachePrune.last = time.Time{}
	captureTargetCachePrune.Unlock()
	t.Cleanup(func() {
		captureTargetCache.Delete("deleted:0")
		captureTargetCache.Delete("active:0")
	})

	pruneCaptureTargetCache(now)
	if _, ok := captureTargetCache.Load("deleted:0"); ok {
		t.Fatal("expired deleted-session cache entry was retained")
	}
	if _, ok := captureTargetCache.Load("active:0"); !ok {
		t.Fatal("live cache entry was pruned")
	}
}
