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
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerClosed, b.state)
	})

	t.Run("transitions to open on error", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("rejects all requests when open", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		b.state = circuitBreakerOpen
		b.lastOpened = time.Now()

		ok, doneFunc := b.Allow()
		require.False(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerOpen, b.state)

		// When the circuit breaker is open, the doneFunc should be a noopDoneFunc
		// and calling it should not change the state.
		doneFunc(nil)
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("transitions to half-open after open period", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
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

	t.Run("allows up to maxTrials trials when half-open", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		b.state = circuitBreakerHalfOpen

		// Exactly maxTrials trial requests are allowed through.
		for range 10 {
			ok, _ := b.Allow()
			require.True(t, ok)
		}

		// Any further requests are rejected while the trials are outstanding.
		ok, _ := b.Allow()
		require.False(t, ok)
		require.Equal(t, circuitBreakerHalfOpen, b.state)
	})

	t.Run("transitions to closed when all trials succeed", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		b.state = circuitBreakerHalfOpen

		doneFuncs := make([]func(err error), 0, 10)
		for range 10 {
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			doneFuncs = append(doneFuncs, doneFunc)
		}

		// Completing all but the last trial successfully keeps it half-open.
		for _, doneFunc := range doneFuncs[:len(doneFuncs)-1] {
			doneFunc(nil)
			require.Equal(t, circuitBreakerHalfOpen, b.state)
		}

		// Completing the final trial successfully switches back to closed.
		doneFuncs[len(doneFuncs)-1](nil)
		require.Equal(t, circuitBreakerClosed, b.state)
	})

	t.Run("transitions to open if a trial fails in half-open", func(t *testing.T) {
		b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
		b.state = circuitBreakerHalfOpen

		// Some trials succeed.
		ok, doneFunc1 := b.Allow()
		require.True(t, ok)
		doneFunc1(nil)
		require.Equal(t, circuitBreakerHalfOpen, b.state)

		// A single failing trial switches back to open.
		ok, doneFunc2 := b.Allow()
		require.True(t, ok)
		doneFunc2(errors.New("an error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	})

	t.Run("resets trials and successes when re-entering half-open", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTrialCircuitBreaker(time.Second, isAnyErr, 10)
			b.state = circuitBreakerHalfOpen

			// A few successful trials accumulate, then a failure re-opens it.
			for range 3 {
				ok, doneFunc := b.Allow()
				require.True(t, ok)
				doneFunc(nil)
			}
			ok, doneFunc := b.Allow()
			require.True(t, ok)
			doneFunc(errors.New("an error occurred"))
			require.Equal(t, circuitBreakerOpen, b.state)

			// After the open period the counters should start fresh, so a full
			// maxTrials successful trials are needed to close again.
			time.Sleep(2 * time.Second)
			doneFuncs := make([]func(err error), 0, 10)
			for range 10 {
				ok, doneFunc := b.Allow()
				require.True(t, ok)
				doneFuncs = append(doneFuncs, doneFunc)
			}
			require.Equal(t, circuitBreakerHalfOpen, b.state)
			for _, doneFunc := range doneFuncs {
				doneFunc(nil)
			}
			require.Equal(t, circuitBreakerClosed, b.state)
		})
	})
}
