package distributor

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinearRampCircuitBreaker(t *testing.T) {
	isAnyErr := func(err error) bool { return err != nil }

	t.Run("allows requests when closed", func(t *testing.T) {
		b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerClosed, b.state)
	})

	t.Run("transitions to open on error", func(t *testing.T) {
		b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("transitions to open are de-bounced", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)

			// Make two calls to Allow so we can have two separate doneFuncs.
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			ok2, doneFunc2 := b.Allow()
			require.True(t, ok2)

			// The first doneFunc should open the circuit breaker.
			doneFunc(errors.New("some error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)
			now := time.Now()
			require.Equal(t, now, b.lastOpened)

			// Sleep 1ms, and then use doneFunc2 to open the circuit breaker a second
			// time. It should be de-bounced.
			time.Sleep(time.Millisecond)
			doneFunc2(errors.New("some error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)
			require.Equal(t, now, b.lastOpened)
		})
	})

	t.Run("rejects all requests when open", func(t *testing.T) {
		b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
		b.state = circuitBreakerOpen
		b.lastOpened = time.Now()

		ok, doneFunc := b.Allow()
		require.False(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerOpen, b.state)

		// When the circuit breaker is open, the requests counter should not be
		// incremented, and the doneFunc should be a noopDoneFunc.
		require.Equal(t, b.requests, 0)
		doneFunc(nil)
		require.Equal(t, b.requests, 0)
	})

	t.Run("transitions to half-open after open period", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
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

	t.Run("allows some requests when half-open", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
			b.state = circuitBreakerHalfOpen
			b.lastOpened = time.Now().Add(-time.Second)

			// At 0 seconds into the 10s half-open period, no requests are allowed as
			// a whole second has not passed.
			ok, doneFunc := b.Allow()
			require.False(t, ok)
			doneFunc(nil)

			// At 1s into a 10s half-open period the ramp is 0.1. This means at most one
			// request can be processed at a time.
			time.Sleep(1 * time.Second)
			ok, doneFunc = b.Allow()
			require.True(t, ok)

			// The second request should fail.
			ok2, doneFunc2 := b.Allow()
			require.False(t, ok2)
			doneFunc2(nil)
			doneFunc(nil)

			// At 9s into a 10s half-open period the ramp is 0.9. This means 9 requests
			// can be processed at a time.
			time.Sleep(8 * time.Second)
			doneFuncs := make([]func(err error), 0, 10)
			for range 9 {
				ok, doneFunc = b.Allow()
				require.True(t, ok)
				doneFuncs = append(doneFuncs, doneFunc)
			}

			// The 10th request should be rejected.
			ok, doneFunc = b.Allow()
			require.False(t, ok)
			doneFuncs = append(doneFuncs, doneFunc)

			// The 10th should be allowed now we've called one of the done funcs from
			// the previous requests.
			doneFuncs[0](nil)
			ok, _ = b.Allow()
			require.True(t, ok)
		})
	})

	t.Run("transitions to closed after half-open period", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
			b.state = circuitBreakerHalfOpen
			b.lastOpened = time.Now().Add(-time.Second)

			// Sleep until the end of the half-open period, should remain half open.
			time.Sleep(10 * time.Second)
			ok, _ := b.Allow()
			require.True(t, ok)
			require.Equal(t, circuitBreakerHalfOpen, b.state)

			// Should switch to closed.
			time.Sleep(1 * time.Second)
			ok, _ = b.Allow()
			require.True(t, ok)
			require.Equal(t, circuitBreakerClosed, b.state)
		})
	})

	t.Run("transitions to open if error occurs in half-open period", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newLinearRampCircuitBreaker(time.Second, 10*time.Second, isAnyErr, 10)
			b.state = circuitBreakerHalfOpen
			b.lastOpened = time.Now().Add(-time.Second)

			// We are 5 seconds into the half open window, and then an error occurs,
			// should transition back to open.
			time.Sleep(5 * time.Second)
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			require.Equal(t, circuitBreakerHalfOpen, b.state)
			doneFunc(errors.New("an error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)
		})
	})
}
