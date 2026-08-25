package retry

import (
	"math/rand/v2"
	"sync"
	"time"
)

// Backoff is an interface that backs off.
type Backoff interface {
	// Next returns the time duration to wait and whether to stop.
	Next() (next time.Duration, stop bool)
}

var _ Backoff = (BackoffFunc)(nil)

// BackoffFunc is a backoff expressed as a function.
type BackoffFunc func() (time.Duration, bool)

// Next implements Backoff.
func (b BackoffFunc) Next() (time.Duration, bool) {
	return b()
}

// WithJitter wraps a backoff function and adds the specified jitter. j can be
// interpreted as "+/- j". For example, if j were 5 seconds and the backoff
// returned 20s, the value could be between 15 and 25 seconds. The value can
// never be less than 0.
func WithJitter(j time.Duration, next Backoff) Backoff {
	return BackoffFunc(func() (time.Duration, bool) {
		val, stop := next.Next()
		if stop {
			return 0, true
		}

		if j <= 0 {
			return val, false
		}

		diff := time.Duration(rand.Int64N(int64(j)*2) - int64(j))
		val = max(val+diff, 0)
		return val, false
	})
}

// WithJitterPercent wraps a backoff function and adds the specified jitter
// percentage. j can be interpreted as "+/- j%". For example, if j were 5 and
// the backoff returned 20s, the value could be between 19 and 21 seconds. The
// value can never be less than 0 or greater than 100.
func WithJitterPercent(j uint64, next Backoff) Backoff {
	return BackoffFunc(func() (time.Duration, bool) {
		val, stop := next.Next()
		if stop {
			return 0, true
		}

		if j <= 0 {
			return val, false
		}

		// Get a value between -j and j, the convert to a percentage
		top := rand.Int64N(int64(j)*2) - int64(j)
		pct := 1 - float64(top)/100.0

		val = max(time.Duration(float64(val)*pct), 0)
		return val, false
	})
}

// WithFullJitter wraps a backoff function and returns a random value between
// zero (inclusive) and the wrapped backoff's returned value (exclusive). For
// example, if the backoff returned 20s, the value could be anywhere in [0, 20s).
//
// Unlike WithJitter, which applies "+/- j" around the value, full jitter
// spreads the wait across the entire range, which spreads out retrying clients
// more effectively. See
// https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/.
func WithFullJitter(next Backoff) Backoff {
	return BackoffFunc(func() (time.Duration, bool) {
		val, stop := next.Next()
		if stop {
			return 0, true
		}

		if val <= 0 {
			return val, false
		}

		return time.Duration(rand.Int64N(int64(val))), false
	})
}

// WithMaxRetries executes the backoff function up until the maximum attempts.
func WithMaxRetries(max uint64, next Backoff) Backoff {
	var l sync.Mutex
	var attempt uint64

	return BackoffFunc(func() (time.Duration, bool) {
		l.Lock()
		defer l.Unlock()

		if attempt >= max {
			return 0, true
		}
		attempt++

		val, stop := next.Next()
		if stop {
			return 0, true
		}

		return val, false
	})
}

// WithCappedDuration sets a maximum on the duration returned from the next
// backoff. This is NOT a total backoff time, but rather a cap on the maximum
// value a backoff can return. Without another middleware, the backoff will
// continue infinitely.
func WithCappedDuration(cap time.Duration, next Backoff) Backoff {
	return BackoffFunc(func() (time.Duration, bool) {
		val, stop := next.Next()
		if stop {
			return 0, true
		}

		if val <= 0 || val > cap {
			val = cap
		}
		return val, false
	})
}

// WithMaxDuration sets a maximum on the total amount of time a backoff should
// execute. It's best-effort, and should not be used to guarantee an exact
// amount of time.
func WithMaxDuration(timeout time.Duration, next Backoff) Backoff {
	start := time.Now()

	return BackoffFunc(func() (time.Duration, bool) {
		diff := timeout - time.Since(start)
		if diff <= 0 {
			return 0, true
		}

		val, stop := next.Next()
		if stop {
			return 0, true
		}

		if val <= 0 || val > diff {
			val = diff
		}
		return val, false
	})
}
