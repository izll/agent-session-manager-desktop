package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusRefreshUsesBoundedWorkerPool(t *testing.T) {
	instances := make([]*Instance, maxConcurrentStatusRefreshes*4)
	for i := range instances {
		instances[i] = &Instance{ID: "session"}
	}
	var active, maximum, completed atomic.Int32
	refreshInstanceStatusesWith(context.Background(), instances, func(context.Context, *Instance) {
		current := active.Add(1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
		completed.Add(1)
	})
	if got := maximum.Load(); got > maxConcurrentStatusRefreshes {
		t.Fatalf("status refresh concurrency = %d, limit = %d", got, maxConcurrentStatusRefreshes)
	}
	if got := completed.Load(); got != int32(len(instances)) {
		t.Fatalf("completed refreshes = %d, want %d", got, len(instances))
	}
}
