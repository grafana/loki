package distributor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestTenantCircuitBreaker(t *testing.T) {
	// Opens the circuit breaker on the first per-tenant error, and permits one
	// trial request in the half-open state.
	newCircuitBreaker := func() *trialCircuitBreaker {
		return newTrialCircuitBreaker(time.Second, 1, 1, func(err error) bool {
			return errors.Is(err, errTooManyRequestsMaxLoad)
		})
	}

	t.Run("creates state per tenant", func(t *testing.T) {
		b := newTenantCircuitBreaker(newCircuitBreaker)
		a := b.state("tenant-a")
		require.NotNil(t, a)
		require.NotNil(t, a.circuitBreaker)
		// The same tenant must always get the same state.
		require.Same(t, a, b.state("tenant-a"))
		// A different tenant must get different state.
		require.NotSame(t, a, b.state("tenant-b"))
		require.Len(t, b.tenants, 2)
	})

	t.Run("does not create circuit breakers when disabled", func(t *testing.T) {
		b := newTenantCircuitBreaker(nil)
		state := b.state("tenant-a")
		require.NotNil(t, state)
		require.Nil(t, state.circuitBreaker)
		// All requests must be permitted.
		ok, doneFunc := b.Allow("tenant-a")
		require.True(t, ok)
		require.NotNil(t, doneFunc)
	})

	t.Run("tracks inflight bytes per tenant", func(t *testing.T) {
		b := newTenantCircuitBreaker(newCircuitBreaker)
		require.Equal(t, int64(100), b.state("tenant-a").inflightBytes.Add(100))
		require.Equal(t, int64(150), b.state("tenant-a").inflightBytes.Add(50))
		// tenant-b must have its own counter.
		require.Equal(t, int64(10), b.state("tenant-b").inflightBytes.Add(10))
		require.Equal(t, int64(150), b.state("tenant-a").inflightBytes.Load())
		// The bytes must be released.
		b.state("tenant-a").inflightBytes.Add(-150)
		require.Zero(t, b.state("tenant-a").inflightBytes.Load())
	})

	t.Run("circuit breakers are independent per tenant", func(t *testing.T) {
		b := newTenantCircuitBreaker(newCircuitBreaker)

		// Open tenant-a's circuit breaker.
		ok, doneFunc := b.Allow("tenant-a")
		require.True(t, ok)
		doneFunc(errTooManyRequestsMaxLoad)
		require.Equal(t, circuitBreakerOpen, b.state("tenant-a").circuitBreaker.state)

		// tenant-a must now be denied.
		ok, _ = b.Allow("tenant-a")
		require.False(t, ok)

		// tenant-b must be unaffected.
		ok, _ = b.Allow("tenant-b")
		require.True(t, ok)
		require.Equal(t, circuitBreakerClosed, b.state("tenant-b").circuitBreaker.state)
	})

	t.Run("does not open the circuit breaker for the global error", func(t *testing.T) {
		b := newTenantCircuitBreaker(newCircuitBreaker)
		ok, doneFunc := b.Allow("tenant-a")
		require.True(t, ok)
		// The distributor as a whole being overloaded must not open an individual
		// tenant's circuit breaker.
		doneFunc(errServiceUnavailableMaxLoad)
		require.Equal(t, circuitBreakerClosed, b.state("tenant-a").circuitBreaker.state)
	})

	t.Run("cleanup evicts idle tenants", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTenantCircuitBreaker(newCircuitBreaker)
			b.state("tenant-a")
			require.Len(t, b.tenants, 1)

			// The tenant must not be evicted before the idle period elapses.
			time.Sleep(tenantIdlePeriod - time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Len(t, b.tenants, 1)

			time.Sleep(2 * time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Empty(t, b.tenants)
		})
	})

	t.Run("cleanup does not evict tenants with inflight bytes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTenantCircuitBreaker(newCircuitBreaker)
			b.state("tenant-a").inflightBytes.Add(100)

			time.Sleep(tenantIdlePeriod + time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Len(t, b.tenants, 1)

			// Once the bytes are released the tenant can be evicted.
			b.state("tenant-a").inflightBytes.Add(-100)
			time.Sleep(tenantIdlePeriod + time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Empty(t, b.tenants)
		})
	})

	t.Run("cleanup does not evict tenants with an open circuit breaker", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := newTenantCircuitBreaker(newCircuitBreaker)
			ok, doneFunc := b.Allow("tenant-a")
			require.True(t, ok)
			doneFunc(errTooManyRequestsMaxLoad)
			require.Equal(t, circuitBreakerOpen, b.state("tenant-a").circuitBreaker.state)

			// Evicting it would silently reset it and admit the requests it is
			// shedding.
			time.Sleep(tenantIdlePeriod + time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Len(t, b.tenants, 1)
		})
	})

	t.Run("cleanup does not evict tenants with recorded failures", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// Requires two failures to open, so one failure leaves the circuit
			// breaker closed but with state worth keeping.
			b := newTenantCircuitBreaker(func() *trialCircuitBreaker {
				return newTrialCircuitBreaker(time.Second, 2, 1, func(err error) bool {
					return errors.Is(err, errTooManyRequestsMaxLoad)
				})
			})
			ok, doneFunc := b.Allow("tenant-a")
			require.True(t, ok)
			doneFunc(errTooManyRequestsMaxLoad)
			require.Equal(t, circuitBreakerClosed, b.state("tenant-a").circuitBreaker.state)
			require.Equal(t, 1, b.state("tenant-a").circuitBreaker.failures)

			time.Sleep(tenantIdlePeriod + time.Second)
			require.NoError(t, b.cleanup(context.Background()))
			require.Len(t, b.tenants, 1)
		})
	})

	t.Run("collects metrics per tenant", func(t *testing.T) {
		b := newTenantCircuitBreaker(newCircuitBreaker)

		// Open tenant-a's circuit breaker, leave tenant-b's closed.
		ok, doneFunc := b.Allow("tenant-a")
		require.True(t, ok)
		doneFunc(errTooManyRequestsMaxLoad)
		ok, _ = b.Allow("tenant-b")
		require.True(t, ok)

		expected := `
# HELP loki_distributor_tenant_circuit_breaker_open_total The number of times the circuit breaker opened for each tenant.
# TYPE loki_distributor_tenant_circuit_breaker_open_total counter
loki_distributor_tenant_circuit_breaker_open_total{tenant="tenant-a"} 1
loki_distributor_tenant_circuit_breaker_open_total{tenant="tenant-b"} 0
# HELP loki_distributor_tenant_circuit_breaker_state The state of the circuit breaker for each tenant.
# TYPE loki_distributor_tenant_circuit_breaker_state gauge
loki_distributor_tenant_circuit_breaker_state{tenant="tenant-a"} 1
loki_distributor_tenant_circuit_breaker_state{tenant="tenant-b"} 0
`
		require.NoError(t, testutil.CollectAndCompare(b, strings.NewReader(expected),
			"loki_distributor_tenant_circuit_breaker_state",
			"loki_distributor_tenant_circuit_breaker_open_total",
		))
	})

	t.Run("collects no metrics when circuit breakers are disabled", func(t *testing.T) {
		b := newTenantCircuitBreaker(nil)
		b.state("tenant-a")
		require.Zero(t, testutil.CollectAndCount(b,
			"loki_distributor_tenant_circuit_breaker_state",
			"loki_distributor_tenant_circuit_breaker_open_total",
		))
	})
}
