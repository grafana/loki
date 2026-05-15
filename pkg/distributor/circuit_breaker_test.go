package distributor

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinearRampCircuitBreaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newLinearRampCircuitBreaker(
			time.Second,
			10*time.Second,
			func(err error) bool { return err != nil },
			10,
		)

		ok, doneFunc := b.Allow()
		require.True(t, ok)
		require.NotNil(t, doneFunc)
		require.Equal(t, circuitBreakerClosed, b.state)
		// Check that the doneFunc decrements the request counter.
		require.Equal(t, 1, b.requests)
		doneFunc(nil)
		require.Equal(t, 0, b.requests)

		// An error should open the circuit breaker.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)

		// Sleep until the end of the open period, should remain in open.
		time.Sleep(1 * time.Second)
		ok, _ = b.Allow()
		require.False(t, ok)
		require.Equal(t, circuitBreakerOpen, b.state)

		// Should switch to half-open.
		time.Sleep(1 * time.Second)
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		require.Equal(t, circuitBreakerHalfOpen, b.state)
		doneFunc(nil)

		// At 1s into a 10s half-open period the ramp is 0.1. This means at most one
		// request can be processed at a time.
		ok, doneFunc = b.Allow()
		require.True(t, ok)
		ok2, doneFunc2 := b.Allow()
		require.False(t, ok2)
		doneFunc(nil)
		doneFunc2(nil)

		// At 9s into a 10s half-open period the ramp is 0.9. This means 9 requests
		// can be processed at a time.
		time.Sleep(8 * time.Second)
		for i := 0; i < 9; i++ {
			ok, _ = b.Allow()
			require.True(t, ok)
		}
		// The 10th request should be rejected.
		ok, _ = b.Allow()
		require.False(t, ok)

		// Should switch to closed.
		time.Sleep(2 * time.Second)
		ok, _ = b.Allow()
		require.True(t, ok)
		require.Equal(t, circuitBreakerClosed, b.state)
	})
}
