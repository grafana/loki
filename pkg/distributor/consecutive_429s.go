package distributor

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

// default429IdleTimeout is how long a tenant must go without an ingestion
// rate-limit decision before its counter is evicted.
const default429IdleTimeout = 15 * time.Second

// consecutive429s tracks, per tenant, the number of consecutive push requests
// rejected with a 429 by the ingestion rate limiter. It is intended as a
// backpressure signal: a tenant with a long streak of rejections is one whose
// clients are persistently over their limit.
//
// Counts are approximate. A tenant's pushes are concurrent and have no defined
// order, so when rejections and successes are in flight together the observed
// streak depends on scheduling. Making it exact is not possible without
// serializing a tenant's pushes, which is not worth it for a heuristic.
type consecutive429s struct {
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
	// lastSeen is the unix-nano time of the most recent Observe for this tenant.
	lastSeen atomic.Int64
}

// newConsecutive429s returns a tracker that evicts tenants which have not been
// observed within idleTimeout.
func newConsecutive429s(idleTimeout time.Duration) *consecutive429s {
	c := &consecutive429s{
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
	} else {
		tc.consecutive.Store(0)
	}

	c.maybeSweep(now)
	return int(n)
}

// Get returns the current consecutive 429 count for tenantID, or 0 if the tenant
// has no recorded rejections. Unlike Observe it never creates a counter, so
// reads for unknown tenants cannot grow the map.
func (c *consecutive429s) Get(tenantID string) int {
	c.maybeSweep(c.now())
	c.mtx.RLock()
	tc := c.counters[tenantID]
	c.mtx.RUnlock()
	if tc == nil {
		return 0
	}
	return int(tc.consecutive.Load())
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
