package distributor

import (
	"context"
	"sync"
)

// PushTracker is an interface to track the status of pushes and wait on
// their completion.
type PushTracker interface {
	// Add increments the number of pushes. It must not be called after the
	// last call to [Done] has completed.
	Add(int32)

	// Done decrements the number of pushes. It accepts an optional error
	// if the push failed.
	Done(err error)

	// Wait until all pushes are done or a push fails, whichever happens
	// first.
	Wait(ctx context.Context) error
}

type basicPushTracker struct {
	mtx      sync.Mutex    // protects the fields below.
	n        int32         // the number of pushes.
	firstErr error         // the first reported error from a push.
	doneCh   chan struct{} // closed when all pushes are done.
	errCh    chan struct{} // closed when an error is reported.
	done     bool          // fast path, equivalent to select { case <-t.doneCh: default: }
}

// newBasicPushTracker returns a new, initialized [newSimplePushTracker].
func newBasicPushTracker() *basicPushTracker {
	return &basicPushTracker{
		doneCh: make(chan struct{}),
		errCh:  make(chan struct{}),
	}
}

// Add implements the [PushTracker] interface.
func (t *basicPushTracker) Add(n int32) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.done {
		panic("Add called after last call to Done")
	}
	t.n += n
	if t.n < 0 {
		// We panic on negative counters just like [sync.WaitGroup].
		panic("Negative counter")
	}
}

// Done implements the [PushTracker] interface.
func (t *basicPushTracker) Done(err error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.n <= 0 {
		// We panic here just like [sync.WaitGroup].
		panic("Done called more times than Add")
	}
	if err != nil && t.firstErr == nil {
		// errCh can never be closed twice as t.firstErr can never be nil
		// more than once.
		t.firstErr = err
		close(t.errCh)
	}
	t.n--
	if t.n == 0 {
		close(t.doneCh)
		t.done = true
	}
}

// Wait implements the [PushTracker] interface.
func (t *basicPushTracker) Wait(ctx context.Context) error {
	t.mtx.Lock()
	// We need to have the mutex here as t.n can be modified as doneCh has
	// not been closed, while t.firstErr can still be modified as neither
	// doneCh nor errCh have been closed.
	if t.firstErr != nil || t.n == 0 {
		// We need to store the firstErr before releasing the mutex for the
		// same reason.
		res := t.firstErr
		t.mtx.Unlock()
		return res
	}
	t.mtx.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.doneCh:
		// Must return t.firstErr as done is also closed if the last push
		// failed. We don't need the mutex here as t.firstErr is never
		// modified after doneCh is closed.
		return t.firstErr
	case <-t.errCh:
		// We don't need the mutex here either as t.firstErr is never modified
		// after errCh is closed.
		return t.firstErr
	}
}
