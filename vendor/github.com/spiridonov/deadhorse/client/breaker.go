package client

import (
	"sync/atomic"
	"time"
)

// defaultBreakerFailureThreshold and defaultBreakerOpenDuration are the
// out-of-the-box circuit breaker settings: five consecutive shard-attributed
// failures (see exchange's shardFault return for exactly what counts) trip
// the breaker, which then fails every call to that shard fast for one
// second before letting a single trial call back through. Override with
// WithBreaker -- there's no size of these two numbers that's right for
// every deployment, since they trade off how quickly a truly dead shard
// stops costing every caller its full timeout against how long a shard that
// recovers mid-outage stays needlessly cut off.
const (
	defaultBreakerFailureThreshold = 5
	defaultBreakerOpenDuration     = 1 * time.Second
)

// circuitBreaker fails calls to one shard fast -- without dialing, writing,
// or waiting out any timeout -- once that shard has just produced enough
// consecutive failures that another attempt right now is far more likely to
// waste a full timeout than to get a real answer. It exists because
// shardConn itself has no memory between calls: without a breaker, a shard
// that's fully dead or black-holing packets costs every single caller its
// whole configured timeout, forever, instead of just the first few callers
// unlucky enough to be the ones who notice.
//
// States, and how a call moves between them:
//   - closed: every call is let through by allow. recordFailure counts
//     consecutive failures; recordSuccess resets that count to zero.
//     Reaching failureThreshold consecutive failures opens the breaker.
//   - open: every call is failed immediately by allow, with no network
//     activity at all, until openDuration has passed since the breaker
//     last tripped (or was last reopened -- see below).
//   - half-open (not a separately stored state, just what "open, and
//     openDuration has elapsed" means): the next call through allow becomes
//     "the trial" -- exactly one at a time, guarded by trialInFlight -- and
//     is let through to actually attempt the shard, while every other
//     concurrent call is still failed fast. The trial's outcome decides
//     what's next: recordSuccess closes the breaker; recordFailure reopens
//     it and restarts the openDuration cooldown, so the next trial isn't
//     immediate.
//
// allow, recordSuccess, and recordFailure are each called from whichever
// goroutine is making (or just finished) a call to this shard, with no
// ordering between concurrent calls beyond what the atomics below give, and
// none of the three may ever block a concurrent call to the same shard --
// hence a handful of atomics here rather than a mutex. The zero value is
// not useful (failureThreshold of 0 would trip on the very first failure);
// always build one through newCircuitBreaker.
type circuitBreaker struct {
	failureThreshold int64
	openDuration     time.Duration

	open                atomic.Bool
	consecutiveFailures atomic.Int64
	openedAtNano        atomic.Int64
	trialInFlight       atomic.Bool
}

// newCircuitBreaker builds a closed breaker. failureThreshold <= 0 disables
// it entirely: allow always returns true and recordFailure never opens it,
// exactly as if there were no breaker at all.
func newCircuitBreaker(failureThreshold int64, openDuration time.Duration) *circuitBreaker {
	return &circuitBreaker{failureThreshold: failureThreshold, openDuration: openDuration}
}

// allow reports whether a call to this shard may proceed. A false result
// means: report this call as failed exactly as if the network had, without
// touching the network at all.
func (b *circuitBreaker) allow() bool {
	if b.failureThreshold <= 0 || !b.open.Load() {
		return true
	}
	if time.Since(time.Unix(0, b.openedAtNano.Load())) < b.openDuration {
		return false
	}
	// Cooldown elapsed: let exactly one concurrent call through as the
	// trial. Everyone else -- including any other goroutine that sees the
	// cooldown as elapsed at the very same instant -- finds trialInFlight
	// already true and keeps failing fast.
	return b.trialInFlight.CompareAndSwap(false, true)
}

// recordSuccess reports that a call let through by allow actually got a
// real answer from the shard: whatever the breaker's state, it is (or goes
// back to) closed.
func (b *circuitBreaker) recordSuccess() {
	b.consecutiveFailures.Store(0)
	if b.open.CompareAndSwap(true, false) {
		b.trialInFlight.Store(false)
	}
}

// recordFailure reports that a call let through by allow failed for a
// reason attributable to the shard (see exchange's shardFault return --
// this must never be called for a failure that was actually the caller's
// own ctx ending first). trialInFlight being true means this failure came
// from the trial: reopen the breaker and restart its cooldown. Otherwise,
// just count the failure, opening the breaker for the first time once
// failureThreshold consecutive failures is reached.
func (b *circuitBreaker) recordFailure() {
	if b.failureThreshold <= 0 {
		return
	}
	if b.trialInFlight.Load() {
		b.openedAtNano.Store(time.Now().UnixNano())
		b.trialInFlight.Store(false)
		return
	}
	if b.consecutiveFailures.Add(1) >= b.failureThreshold {
		b.openedAtNano.Store(time.Now().UnixNano())
		b.open.Store(true)
	}
}
