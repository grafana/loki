package distributor

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
)

const (
	// How often idle tenants are evicted.
	tenantCleanupInterval = time.Minute

	// How long a tenant must be idle before it can be evicted.
	tenantIdlePeriod = 5 * time.Minute
)

var (
	tenantCircuitBreakerStateDesc = prometheus.NewDesc(
		"loki_distributor_tenant_circuit_breaker_state",
		"The state of the circuit breaker for each tenant.",
		[]string{"tenant"},
		nil,
	)
	tenantCircuitBreakerOpenDesc = prometheus.NewDesc(
		"loki_distributor_tenant_circuit_breaker_open_total",
		"The number of times the circuit breaker opened for each tenant.",
		[]string{"tenant"},
		nil,
	)
)

// A tenantState is the inflight bytes accounting and circuit breaker for a
// single tenant.
type tenantState struct {
	inflightBytes atomic.Int64

	// lastSeen is unix nanoseconds, updated each time the tenant is looked up.
	// It is used to evict tenants that have stopped sending requests.
	lastSeen atomic.Int64

	// circuitBreaker is nil when circuit breakers are disabled.
	circuitBreaker *trialCircuitBreaker
}

// A tenantCircuitBreaker maintains an inflight bytes counter and a circuit
// breaker per tenant, so that a tenant sending more than its share of the
// inflight bytes limit can be shed on its own instead of the distributor
// shedding load for all tenants.
//
// It is a [services.Service] that periodically evicts idle tenants, and a
// [prometheus.Collector] that reports the state of each tenant's circuit
// breaker.
type tenantCircuitBreaker struct {
	services.Service

	// newCircuitBreaker returns the circuit breaker for a new tenant. It
	// returns nil when circuit breakers are disabled.
	newCircuitBreaker func() *trialCircuitBreaker

	// mtx guards tenants. It must never be acquired while holding a
	// trialCircuitBreaker mutex: the lock order is always mtx first.
	mtx     sync.RWMutex
	tenants map[string]*tenantState
}

// newTenantCircuitBreaker returns a new tenantCircuitBreaker. newCircuitBreaker
// may be nil, or may return nil, in which case inflight bytes are still counted
// per tenant but [tenantCircuitBreaker.Allow] always permits the request.
func newTenantCircuitBreaker(newCircuitBreaker func() *trialCircuitBreaker) *tenantCircuitBreaker {
	b := &tenantCircuitBreaker{
		newCircuitBreaker: newCircuitBreaker,
		tenants:           make(map[string]*tenantState),
	}
	b.Service = services.
		NewTimerService(tenantCleanupInterval, nil, b.cleanup, nil).
		WithName("tenant circuit breaker")
	return b
}

// Allow returns true if the tenant's request can proceed, otherwise false. It
// returns a done callback that MUST be called when the request is finished.
func (b *tenantCircuitBreaker) Allow(tenantID string) (bool, func(err error)) {
	circuitBreaker := b.state(tenantID).circuitBreaker
	if circuitBreaker == nil {
		return true, noopDoneFunc
	}
	return circuitBreaker.Allow()
}

// state returns the state for tenantID, creating it if it does not exist.
func (b *tenantCircuitBreaker) state(tenantID string) *tenantState {
	now := time.Now().UnixNano()

	b.mtx.RLock()
	state, ok := b.tenants[tenantID]
	b.mtx.RUnlock()
	if ok {
		state.lastSeen.Store(now)
		return state
	}

	b.mtx.Lock()
	defer b.mtx.Unlock()
	// Another request may have created the state while we waited for the lock.
	if state, ok = b.tenants[tenantID]; ok {
		state.lastSeen.Store(now)
		return state
	}
	state = &tenantState{}
	if b.newCircuitBreaker != nil {
		state.circuitBreaker = b.newCircuitBreaker()
	}
	state.lastSeen.Store(now)
	b.tenants[tenantID] = state
	return state
}

// cleanup evicts tenants that have stopped sending requests. A tenant is
// evicted when it has no inflight bytes, has not been seen for
// tenantIdlePeriod, and its circuit breaker holds no state worth keeping: it is
// closed and has recorded no failures. Such an entry is indistinguishable from
// a freshly created one, so evicting it loses nothing. An open or half-open
// circuit breaker is always kept, because evicting it would silently reset it
// and admit the traffic it is shedding.
//
// A request that looked up its state just before it was evicted continues to
// use the evicted state. This is harmless: the counter it increments and
// decrements is its own, so the counter never goes negative, and the only
// effect is that the tenant is briefly undercounted. The idle period makes this
// vanishingly unlikely in any case.
func (b *tenantCircuitBreaker) cleanup(_ context.Context) error {
	deadline := time.Now().Add(-tenantIdlePeriod).UnixNano()

	b.mtx.Lock()
	defer b.mtx.Unlock()
	for tenantID, state := range b.tenants {
		if state.inflightBytes.Load() != 0 || state.lastSeen.Load() > deadline {
			continue
		}
		if state.circuitBreaker != nil {
			s, _, failures := state.circuitBreaker.snapshot()
			if s != circuitBreakerClosed || failures != 0 {
				continue
			}
		}
		delete(b.tenants, tenantID)
	}
	return nil
}

// Describe implements [prometheus.Collector].
func (b *tenantCircuitBreaker) Describe(descs chan<- *prometheus.Desc) {
	descs <- tenantCircuitBreakerStateDesc
	descs <- tenantCircuitBreakerOpenDesc
}

// Collect implements [prometheus.Collector].
func (b *tenantCircuitBreaker) Collect(metrics chan<- prometheus.Metric) {
	type tenantSnapshot struct {
		tenantID   string
		state      int
		totalOpens int
	}

	// Snapshot all tenants up front so the mutex is not held while sending on
	// the channel.
	b.mtx.RLock()
	snapshots := make([]tenantSnapshot, 0, len(b.tenants))
	for tenantID, state := range b.tenants {
		if state.circuitBreaker == nil {
			continue
		}
		s, totalOpens, _ := state.circuitBreaker.snapshot()
		snapshots = append(snapshots, tenantSnapshot{tenantID, s, totalOpens})
	}
	b.mtx.RUnlock()

	for _, s := range snapshots {
		metrics <- prometheus.MustNewConstMetric(
			tenantCircuitBreakerStateDesc,
			prometheus.GaugeValue,
			float64(s.state),
			s.tenantID,
		)
		metrics <- prometheus.MustNewConstMetric(
			tenantCircuitBreakerOpenDesc,
			prometheus.CounterValue,
			float64(s.totalOpens),
			s.tenantID,
		)
	}
}
