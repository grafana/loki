package distributor

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRandomHalfOpenCircuitBreaker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newLinearRampCircuitBreaker(time.Second, 10*time.Second)
		// This test uses a stub that returns the same value each time.
		var f float64
		b.randf64 = func() float64 { return f }

		require.True(t, b.IsPermitted())
		require.Equal(t, circuitBreakerClosed, b.state)

		// Open the circuit breaker, IsPermitted should return false.
		b.Open()
		require.False(t, b.IsPermitted())
		require.Equal(t, circuitBreakerOpen, b.state)

		// Sleep until the end of the open period, should remain in open.
		time.Sleep(1 * time.Second)
		require.False(t, b.IsPermitted())
		require.Equal(t, circuitBreakerOpen, b.state)

		// Should switch to half-open.
		time.Sleep(1 * time.Second)
		require.True(t, b.IsPermitted())
		require.Equal(t, circuitBreakerHalfOpen, b.state)

		// At 1s into a 10s half-open period the ramp is 0.1.
		f = 0.099
		require.True(t, b.IsPermitted())
		f = 0.1
		require.False(t, b.IsPermitted())

		// At 9s into a 10s half-open period the ramp is 0.9.
		time.Sleep(8 * time.Second)
		f = 0.899
		require.True(t, b.IsPermitted())
		f = 0.9
		require.False(t, b.IsPermitted())

		// Should switch to closed.
		time.Sleep(2 * time.Second)
		require.True(t, b.IsPermitted())
		require.Equal(t, circuitBreakerClosed, b.state)
	})
}
