package distributor

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTrialCircuitBreaker(t *testing.T) {
	isAnyErr := func(err error) bool { return err != nil }

	t.Run("allows requests when closed", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerClosed, b.state)
	})

	t.Run("transitions to open on error", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("transitions to open after 3 successive errors", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 3, 1, isAnyErr)
		// The first two errors should increment the failure counter but not open
		// the circuit breaker.
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerClosed, b.state)
		require.Equal(t, 1, b.failures)

		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerClosed, b.state)
		require.Equal(t, 2, b.failures)

		// The third error should open it.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
		// The failure counter is reset to 0 at the start of each term.
		require.Equal(t, 0, b.failures)
	})

	t.Run("success resets failure counter when closed", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 3, 1, isAnyErr)
		// An error should increment the failure counter.
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerClosed, b.state)
		require.Equal(t, 1, b.failures)
		// A success should reset the failure counter.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(nil)
		require.Equal(t, circuitBreakerClosed, b.state)
		require.Equal(t, 0, b.failures)
	})

	t.Run("rejects all requests when open", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		b.state = circuitBreakerOpen
		b.lastOpened = time.Now()

		var calls int
		b.noopDoneFunc = func(_ error) {
			calls++
		}

		ok, doneFunc := b.Allow()
		require.False(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerOpen, b.state)

		// When the circuit breaker is open, the doneFunc should be a noopDoneFunc.
		require.Equal(t, 0, calls)
		doneFunc(nil)
		require.Equal(t, 1, calls)
	})

	t.Run("transitions to half-open after open period", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
			b.state = circuitBreakerOpen
			b.lastOpened = time.Now()

			// Sleep until the end of the open period, should remain in open.
			time.Sleep(1 * time.Second)
			ok, _ := b.Allow()
			require.False(t, ok)
			require.Equal(t, circuitBreakerOpen, b.state)

			// Should switch to half-open.
			time.Sleep(1 * time.Second)
			ok, _ = b.Allow()
			require.True(t, ok)
			require.Equal(t, circuitBreakerHalfOpen, b.state)
		})
	})

	t.Run("allows up 10 trial requests when half-open, additional requests are dropped", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 10, isAnyErr)
		b.state = circuitBreakerHalfOpen
		for range 10 {
			ok, _ := b.Allow()
			require.True(t, ok)
		}
		// The 11th request is dropped.
		ok, _ := b.Allow()
		require.False(t, ok)
		require.Equal(t, circuitBreakerHalfOpen, b.state)
	})

	t.Run("transitions to closed when all trial requests succeed", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 10, isAnyErr)
		b.state = circuitBreakerHalfOpen
		for range 9 {
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			doneFunc(nil)
		}
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		require.Equal(t, circuitBreakerHalfOpen, b.state)
		// When the last request succeeds it should transition to closed.
		doneFunc(nil)
		require.Equal(t, circuitBreakerClosed, b.state)
	})

	t.Run("transitions to open when a trial request fails", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 1, 10, isAnyErr)
		b.state = circuitBreakerHalfOpen

		// The first trial request is successful.
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(nil)
		require.Equal(t, circuitBreakerHalfOpen, b.state)

		// The second trial request fails.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("an error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("state transition increments term", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
			require.Equal(t, 0, b.term)

			// Transition from closed to open increments the term.
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			doneFunc(errors.New("an error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)
			require.Equal(t, 1, b.term)

			// Transition from open to half-open increments the term.
			time.Sleep(2 * time.Second)
			_, doneFunc = b.Allow()
			require.Equal(t, circuitBreakerHalfOpen, b.state)
			require.Equal(t, 2, b.term)

			// Transition from half-open to open increments the term.
			doneFunc(errors.New("an error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)
			require.Equal(t, 3, b.term)

			// Transition back to half-open increments the term once more.
			time.Sleep(2 * time.Second)
			_, doneFunc = b.Allow()
			require.Equal(t, circuitBreakerHalfOpen, b.state)
			require.Equal(t, 4, b.term)

			// Transition from half-open to closed increments the term.
			doneFunc(nil)
			require.Equal(t, circuitBreakerClosed, b.state)
			require.Equal(t, 5, b.term)
		})
	})

	t.Run("transition to half-open resets counters", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTrialCircuitBreaker(time.Second, 1, 3, isAnyErr)
			b.state = circuitBreakerOpen
			b.lastOpened = time.Now()

			// Set the counters to some non-zero value.
			b.trials, b.successes, b.failures = 4, 5, 6
			time.Sleep(2 * time.Second)
			_, _ = b.Allow()
			require.Equal(t, 1, b.trials)
			require.Equal(t, 0, b.successes)
			require.Equal(t, 0, b.failures)
			require.Equal(t, circuitBreakerHalfOpen, b.state)
		})
	})

	t.Run("doneFunc with previous term is skipped", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, 3, 1, isAnyErr)

		// If we pass an error to doneFunc, we expect the failure counter to be
		// incremented.
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("an error occurred"))
		require.Equal(t, 1, b.failures)

		// If we repeat this, but advance the term, no increment should happen.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		b.term++
		doneFunc(errors.New("an error occurred"))
		require.Equal(t, 1, b.failures)

		// Now the term matches once more, the failure counter should be incremented.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("an error occurred"))
		require.Equal(t, 2, b.failures)
	})
}
