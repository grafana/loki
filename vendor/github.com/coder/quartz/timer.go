package quartz

import (
	"time"
)

// The Timer type represents a single event. When the Timer expires, the current time will be sent
// on C, unless the Timer was created by AfterFunc. A Timer must be created with NewTimer or
// AfterFunc.
type Timer struct {
	C <-chan time.Time
	//nolint: revive
	c       chan time.Time
	timer   *time.Timer // realtime impl, if set
	nxt     time.Time   // next tick time
	mock    *Mock       // mock clock, if set
	fn      func()      // AfterFunc function, if set
	stopped bool        // True if stopped, false if running

	// As of Go 1.23, timer channels are unbuffered and guaranteed to block forever after a call to stop.
	//
	// When a mocked timer fires, we don't want to block on a channel write, because it's fine for the code under test
	// not to be reading. That means we need to start a new goroutine to do the channel write if we are a channel-based
	// timer.
	//
	// They also are not supposed to leak even if they are never read or stopped (Go runtime can garbage collect them).
	// We can't garbage-collect because we can't check if any other code besides the mock references, but we can ensure
	// that we don't leak goroutines so that the garbage collector can do its job when the mock is no longer
	// referenced. The channels below allow us to interrupt the channel write goroutine.
	interrupt chan struct{}
}

func (t *Timer) fire(tt time.Time) {
	t.mock.mu.Lock()
	t.mock.removeTimerLocked(t)
	if t.fn != nil {
		t.mock.mu.Unlock()
		t.fn()
		return
	} else {
		interrupt := make(chan struct{})
		// Prevents the goroutine from leaking beyond the test. Side effect is that timer channels cannot be read
		// after the test exits.
		t.mock.tb.Cleanup(func() {
			<-interrupt
		})
		t.interrupt = interrupt
		t.mock.mu.Unlock()
		go func() {
			defer close(interrupt)
			select {
			case t.c <- tt:
			case interrupt <- struct{}{}:
			}
		}()
	}
}

func (t *Timer) next() time.Time {
	return t.nxt
}

// Stop prevents the Timer from firing. It returns true if the call stops the timer, false if the
// timer has already expired or been stopped. Stop does not close the channel, to prevent a read
// from the channel succeeding incorrectly.
//
// See https://pkg.go.dev/time#Timer.Stop for more information.
func (t *Timer) Stop(tags ...string) bool {
	if t.timer != nil {
		return t.timer.Stop()
	}
	t.mock.mu.Lock()
	defer t.mock.mu.Unlock()
	c := newCall(clockFunctionTimerStop, tags)
	t.mock.matchCallLocked(c)
	defer close(c.complete)
	result := !t.stopped
	t.mock.removeTimerLocked(t)
	// check if we've already fired, and if so, interrupt it.
	if t.interrupt != nil {
		<-t.interrupt
		t.interrupt = nil
	}
	return result
}

// Reset changes the timer to expire after duration d. It returns true if the timer had been active,
// false if the timer had expired or been stopped.
//
// See https://pkg.go.dev/time#Timer.Reset for more information.
func (t *Timer) Reset(d time.Duration, tags ...string) bool {
	if t.timer != nil {
		return t.timer.Reset(d)
	}
	t.mock.mu.Lock()
	defer t.mock.mu.Unlock()
	c := newCall(clockFunctionTimerReset, tags, withDuration(d))
	t.mock.matchCallLocked(c)
	defer close(c.complete)
	result := !t.stopped
	// check if we've already fired, and if so, interrupt it.
	if t.interrupt != nil {
		<-t.interrupt
		t.interrupt = nil
	}
	if d <= 0 {
		// zero or negative duration timer means we should immediately re-fire
		// it, rather than remove and re-add it.
		t.stopped = false
		go t.fire(t.mock.cur)
		return result
	}
	t.mock.removeTimerLocked(t)
	t.stopped = false
	t.nxt = t.mock.cur.Add(d)
	t.mock.addEventLocked(t)
	return result
}
