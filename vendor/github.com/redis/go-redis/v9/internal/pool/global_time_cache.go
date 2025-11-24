package pool

import (
	"sync"
	"sync/atomic"
	"time"
)

// Global time cache updated every 50ms by background goroutine.
// This avoids expensive time.Now() syscalls in hot paths like getEffectiveReadTimeout.
// Max staleness: 50ms, which is acceptable for timeout deadline checks (timeouts are typically 3-30 seconds).
var globalTimeCache struct {
	nowNs       atomic.Int64
	lock        sync.Mutex
	started     bool
	stop        chan struct{}
	subscribers int32
}

func subscribeToGlobalTimeCache() {
	globalTimeCache.lock.Lock()
	globalTimeCache.subscribers += 1
	globalTimeCache.lock.Unlock()
}

func unsubscribeFromGlobalTimeCache() {
	globalTimeCache.lock.Lock()
	globalTimeCache.subscribers -= 1
	globalTimeCache.lock.Unlock()
}

func startGlobalTimeCache() {
	globalTimeCache.lock.Lock()
	if globalTimeCache.started {
		globalTimeCache.lock.Unlock()
		return
	}

	globalTimeCache.started = true
	globalTimeCache.nowNs.Store(time.Now().UnixNano())
	globalTimeCache.stop = make(chan struct{})
	globalTimeCache.lock.Unlock()
	// Start background updater
	go func(stopChan chan struct{}) {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case <-stopChan:
				return
			default:
			}
			globalTimeCache.nowNs.Store(time.Now().UnixNano())
		}
	}(globalTimeCache.stop)
}

// stopGlobalTimeCache stops the global time cache if there are no subscribers.
// This should only be called when the last subscriber is removed.
func stopGlobalTimeCache() {
	globalTimeCache.lock.Lock()
	if !globalTimeCache.started || globalTimeCache.subscribers > 0 {
		globalTimeCache.lock.Unlock()
		return
	}
	globalTimeCache.started = false
	close(globalTimeCache.stop)
	globalTimeCache.lock.Unlock()
}

func init() {
	startGlobalTimeCache()
}
