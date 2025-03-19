// Package quartz is a library for testing time related code.  It exports an interface Clock that
// mimics the standard library time package functions.  In production, an implementation that calls
// thru to the standard library is used.  In testing, a Mock clock is used to precisely control and
// intercept time functions.
package quartz

import (
	"context"
	"time"
)

type Clock interface {
	// NewTicker returns a new Ticker containing a channel that will send the current time on the
	// channel after each tick. The period of the ticks is specified by the duration argument. The
	// ticker will adjust the time interval or drop ticks to make up for slow receivers. The
	// duration d must be greater than zero; if not, NewTicker will panic. Stop the ticker to
	// release associated resources.
	NewTicker(d time.Duration, tags ...string) *Ticker
	// TickerFunc is a convenience function that calls f on the interval d until either the given
	// context expires or f returns an error.  Callers may call Wait() on the returned Waiter to
	// wait until this happens and obtain the error. The duration d must be greater than zero; if
	// not, TickerFunc will panic.
	TickerFunc(ctx context.Context, d time.Duration, f func() error, tags ...string) Waiter
	// NewTimer creates a new Timer that will send the current time on its channel after at least
	// duration d.
	NewTimer(d time.Duration, tags ...string) *Timer
	// AfterFunc waits for the duration to elapse and then calls f in its own goroutine. It returns
	// a Timer that can be used to cancel the call using its Stop method. The returned Timer's C
	// field is not used and will be nil.
	AfterFunc(d time.Duration, f func(), tags ...string) *Timer

	// Now returns the current local time.
	Now(tags ...string) time.Time
	// Since returns the time elapsed since t. It is shorthand for Clock.Now().Sub(t).
	Since(t time.Time, tags ...string) time.Duration
	// Until returns the duration until t. It is shorthand for t.Sub(Clock.Now()).
	Until(t time.Time, tags ...string) time.Duration
}

// Waiter can be waited on for an error.
type Waiter interface {
	Wait(tags ...string) error
}
