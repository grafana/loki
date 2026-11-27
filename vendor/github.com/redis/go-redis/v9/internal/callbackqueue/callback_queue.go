package callbackqueue

import (
	"context"
	"sync"

	"github.com/redis/go-redis/v9/internal"
)

// CallbackQueue runs queued callbacks on a single goroutine, in FIFO order.
// The drain goroutine is spawned on demand and exits when the queue empties,
// so an idle queue holds no goroutine.
//
// Convention: name fields of this type `cbq` in every struct that holds one.
type CallbackQueue struct {
	mu       sync.Mutex
	queue    []func()
	draining bool
}

func (q *CallbackQueue) Dispatch(fn func()) {
	q.mu.Lock()
	q.queue = append(q.queue, fn)
	if q.draining {
		q.mu.Unlock()
		return
	}
	q.draining = true
	q.mu.Unlock()
	go q.drain()
}

func (q *CallbackQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.queue) == 0 {
			q.draining = false
			q.mu.Unlock()
			return
		}
		fn := q.queue[0]
		q.queue[0] = nil // release the callback for GC; don't pin it via the slice
		q.queue = q.queue[1:]
		if len(q.queue) == 0 {
			// Drop the (now-empty) backing array so a burst does not pin memory
			// for the queue's lifetime.
			q.queue = nil
		}
		q.mu.Unlock()
		RunSafely(fn)
	}
}

// RunSafely keeps a panicking callback from crashing the process (callbacks
// run on a library-owned goroutine) and, when called from drain, from
// wedging the queue in the draining state. Exported so callers that fan a
// single dispatched item out to multiple independent callbacks (e.g. a
// StateChangeCallback loop) can isolate each one without duplicating this
// recovery logic.
func RunSafely(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(), "callback queue error: the callback panicked: %v", r)
		}
	}()
	fn()
}
