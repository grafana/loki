package distributor

import (
	"context"
	"sync"
)

// pushTracker tracks the status of pushes and waits on their completion.
type pushTracker struct {
	mtx      sync.Mutex    // protects the fields below.
	n        int32         // the number of pushes.
	doneCh   chan struct{} // closed when all pushes are done.
	errCh    chan struct{} // closed when an error is reported.
	done     bool          // fast path, equivalent to select { case <-t.doneCh: default: }
	firstErr error         // the first reported error from [Done].
}

// newPushTracker returns a new, initialized [pushTracker].
func newPushTracker() *pushTracker {
	return &pushTracker{
		// Both doneCh and errCh are unbuffered as they are used for signalling only.
		doneCh: make(chan struct{}),
		errCh:  make(chan struct{}),
	}
}

// Add increments the number of pushes. It must not be called after the
// last call to [Done] has completed.
func (t *pushTracker) Add(n int32) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.done {
		// We don't need to unlock the mutex as defer is run even when a
		// panic occurs.
		panic("Add called after last call to Done")
	}
	t.n += n
	if t.n < 0 {
		// We panic on negative counters just like [sync.WaitGroup].
		panic("Negative counter")
	}
}

// Done decrements the number of pushes. It accepts an optional error
// if the push failed. If two or more calls to [Done] have an error then
// the first error is used.
func (t *pushTracker) Done(err error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.n <= 0 {
		// We panic here just like [sync.WaitGroup]. We don't need to unlock
		// the mutex as defer is run even when a panic occurs.
		panic("Done called more times than Add")
	}
	if err != nil && t.firstErr == nil {
		// errCh can never be closed twice as t.firstErr is nil just once.
		t.firstErr = err
		close(t.errCh)
	}
	t.n--
	if t.n == 0 {
		t.done = true
		close(t.doneCh)
	}
}

// Wait until all pushes are done or a push fails, whichever happens
// first.
func (t *pushTracker) Wait(ctx context.Context) error {
	t.mtx.Lock()
	// We need to hold the mutex here as t.n can be incremented until doneCh is
	// closed, while t.firstErr can be modified as neither doneCh nor errCh
	// have been closed.
	if t.firstErr != nil || t.n == 0 {
		// We need to store firstErr before releasing the mutex in case
		// t.firstErr is nil.
		res := t.firstErr
		t.mtx.Unlock()
		return res
	}
	t.mtx.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.doneCh:
		// Must return t.firstErr as doneCh is closed if the last push failed.
		// We don't need the mutex here as t.firstErr is never modified after
		// doneCh is closed.
		return t.firstErr
	case <-t.errCh:
		// We don't need the mutex here either as t.firstErr is never modified
		// after errCh is closed.
		return t.firstErr
	}
}
