package distributor

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

const (
	// default429IdleTimeout is how long a tenant must go without an ingestion
	// rate-limit decision before its counter is evicted.
	default429IdleTimeout = 15 * time.Second

	// default429Threshold is the number of consecutive 429s a tenant may
	// accumulate before its circuit opens.
	default429Threshold = 3

	// default429OpenPeriod is how long a tenant's circuit stays open once tripped.
	// It must be shorter than default429IdleTimeout, so that the open period, and
	// not the eviction sweep, is what closes the circuit.
	default429OpenPeriod = 5 * time.Second
)

// consecutive429s tracks, per tenant, the number of consecutive push requests
// rejected with a 429 by the ingestion rate limiter. It is intended as a
// backpressure signal: a tenant with a long streak of rejections is one whose
// clients are persistently over their limit.
//
// It behaves as a per-tenant circuit breaker. A streak longer than threshold
// opens the tenant's circuit, and the streak then latches: an admitted request
// no longer clears it. The circuit closes again openPeriod after it opened, at
// which point the tenant's requests reach the rate limiter once more and, if it
// is still over its limit, the streak rebuilds and the circuit reopens.
//
// Counts are approximate. A tenant's pushes are concurrent and have no defined
// order, so when rejections and successes are in flight together the observed
// streak depends on scheduling. Making it exact is not possible without
// serializing a tenant's pushes, which is not worth it for a heuristic. The same
// caveat applies to the circuit: an admitted request still in flight when the
// circuit opens can clear a just-opened circuit. That is benign, as the next
// streak re-opens it.
type consecutive429s struct {
	threshold   int
	openPeriod  time.Duration
	idleTimeout time.Duration
	// now is overridable in tests.
	now func() time.Time

	// lastSweep is the unix-nano time of the last eviction sweep. It is atomic
	// so maybeSweep can decide whether to sweep without taking mtx.
	lastSweep atomic.Int64

	// mtx guards the shape of counters, not the counters themselves: the
	// per-tenant state is atomic, so the common path (a tenant that already has
	// a counter) needs only a read lock, and the write lock is taken only to
	// insert a new tenant or to evict idle ones.
	mtx      sync.RWMutex
	counters map[string]*tenantCounter
}

// A tenantCounter is the per-tenant state of a [consecutive429s].
type tenantCounter struct {
	consecutive atomic.Int64
	// openedAt is the unix-nano time of the observation that opened this tenant's
	// circuit. It is meaningless while the circuit is closed.
	openedAt atomic.Int64
	// lastSeen is the unix-nano time of the most recent Observe for this tenant.
	lastSeen atomic.Int64
}

// newConsecutive429s returns a tracker that opens a tenant's circuit after more
// than threshold consecutive rejections, keeps it open for openPeriod, and evicts
// tenants which have not been observed within idleTimeout. openPeriod must be
// shorter than idleTimeout, otherwise eviction could close a circuit early.
func newConsecutive429s(threshold int, openPeriod, idleTimeout time.Duration) *consecutive429s {
	c := &consecutive429s{
		threshold:   threshold,
		openPeriod:  openPeriod,
		idleTimeout: idleTimeout,
		now:         time.Now,
		counters:    make(map[string]*tenantCounter),
	}
	// Seed lastSweep so the first Observe doesn't take the write lock to sweep an
	// empty map.
	c.lastSweep.Store(c.now().UnixNano())
	return c
}

// Observe records the outcome of one ingestion rate-limit decision for tenantID
// and returns the tenant's resulting consecutive 429 count. rateLimited must be
// true iff the request was rejected with a 429 by the ingestion rate limiter,
// and false iff it was admitted. It must not be called for requests that never
// reached the rate limiter, as those neither extend nor reset a streak.
func (c *consecutive429s) Observe(tenantID string, rateLimited bool) int {
	now := c.now()
	tc := c.counter(tenantID)
	tc.lastSeen.Store(now.UnixNano())

	var n int64
	if rateLimited {
		n = tc.consecutive.Add(1)
		// Add returns a distinct value to each caller and nothing ever stores a value
		// above zero, so exactly one observation sees threshold+1: the one that opens
		// the circuit. Stamping only there stops stragglers extending the open period.
		if n == int64(c.threshold)+1 {
			tc.openedAt.Store(now.UnixNano())
		}
	} else if tc.consecutive.Load() <= int64(c.threshold) {
		// Below the threshold an admitted request still breaks the streak. Once the
		// circuit is open the streak latches: it stays open for openPeriod no matter
		// what the requests already in flight do.
		tc.consecutive.Store(0)
	}

	c.maybeSweep(now)
	return int(n)
}

// IsOpen reports whether tenantID's circuit is open, meaning its push requests
// should be shed without reaching the ingestion rate limiter. The circuit opens
// once a tenant exceeds threshold consecutive 429s and closes again openPeriod
// later. Like Get it never creates a counter.
func (c *consecutive429s) IsOpen(tenantID string) bool {
	now := c.now()
	c.maybeSweep(now)

	tc := c.find(tenantID)
	if tc == nil || tc.consecutive.Load() <= int64(c.threshold) {
		return false
	}
	if now.UnixNano()-tc.openedAt.Load() >= int64(c.openPeriod) {
		// The open period has elapsed. Close the circuit so this tenant reaches the
		// rate limiter again; if it is still over its limit the streak rebuilds and
		// the circuit reopens. Transitioning lazily on read, rather than with a timer,
		// mirrors [trialCircuitBreaker.handleOpenState].
		tc.consecutive.Store(0)
		return false
	}
	return true
}

// Get returns the current consecutive 429 count for tenantID, or 0 if the tenant
// has no recorded rejections. Unlike Observe it never creates a counter, so
// reads for unknown tenants cannot grow the map.
func (c *consecutive429s) Get(tenantID string) int {
	c.maybeSweep(c.now())
	tc := c.find(tenantID)
	if tc == nil {
		return 0
	}
	return int(tc.consecutive.Load())
}

// find returns tenantID's counter, or nil if it has none. Unlike counter it never
// creates one, so reads for unknown tenants cannot grow the map.
func (c *consecutive429s) find(tenantID string) *tenantCounter {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.counters[tenantID]
}

// counter returns tenantID's counter, creating it if absent.
func (c *consecutive429s) counter(tenantID string) *tenantCounter {
	c.mtx.RLock()
	tc := c.counters[tenantID]
	c.mtx.RUnlock()
	if tc != nil {
		return tc
	}

	c.mtx.Lock()
	defer c.mtx.Unlock()
	// Re-check: another goroutine may have inserted it while we upgraded.
	if tc = c.counters[tenantID]; tc == nil {
		tc = &tenantCounter{}
		c.counters[tenantID] = tc
	}
	return tc
}

// maybeSweep deletes the counters of tenants not observed within idleTimeout. It
// sweeps at most once per idleTimeout, and the compare-and-swap ensures only one
// caller sweeps at a time.
//
// A counter can be evicted between counter() returning it and the caller
// updating it, losing that update. This is benign: eviction only targets tenants
// idle for a full idleTimeout, so an active streak is never dropped.
func (c *consecutive429s) maybeSweep(now time.Time) {
	last := c.lastSweep.Load()
	if now.UnixNano()-last < int64(c.idleTimeout) {
		return
	}
	if !c.lastSweep.CompareAndSwap(last, now.UnixNano()) {
		return
	}

	cutoff := now.Add(-c.idleTimeout).UnixNano()
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for tenantID, tc := range c.counters {
		if tc.lastSeen.Load() < cutoff {
			delete(c.counters, tenantID)
		}
	}
}
