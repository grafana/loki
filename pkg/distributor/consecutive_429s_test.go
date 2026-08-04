package distributor

import (
	"sync"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"
)

const testIdleTimeout = 15 * time.Minute

// newTestConsecutive429s returns a tracker driven by a mock clock.
func newTestConsecutive429s(t *testing.T) (*consecutive429s, *quartz.Mock) {
	t.Helper()
	clock := quartz.NewMock(t)
	clock.Set(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	c := newConsecutive429s(testIdleTimeout)
	// quartz's Now is variadic, so it needs an adapter.
	c.now = func() time.Time { return clock.Now() }
	// Re-seed: newConsecutive429s stamped lastSweep from the real clock.
	c.lastSweep.Store(c.now().UnixNano())
	return c, clock
}

func (c *consecutive429s) len() int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return len(c.counters)
}

func TestConsecutive429s_Increments(t *testing.T) {
	c, _ := newTestConsecutive429s(t)

	for i := 1; i <= 5; i++ {
		require.Equal(t, i, c.Observe("tenant", true))
		require.Equal(t, i, c.Get("tenant"))
	}
}

func TestConsecutive429s_ResetsOnAdmittedRequest(t *testing.T) {
	c, _ := newTestConsecutive429s(t)

	require.Equal(t, 1, c.Observe("tenant", true))
	require.Equal(t, 2, c.Observe("tenant", true))

	// An admitted request breaks the streak.
	require.Equal(t, 0, c.Observe("tenant", false))
	require.Equal(t, 0, c.Get("tenant"))

	// The next rejection starts a new streak at 1, not 3.
	require.Equal(t, 1, c.Observe("tenant", true))
	require.Equal(t, 1, c.Get("tenant"))
}

func TestConsecutive429s_GetUnknownTenant(t *testing.T) {
	c, _ := newTestConsecutive429s(t)

	require.Equal(t, 0, c.Get("nobody"))
	// Get must not create a counter, otherwise a hot-path read would grow the map
	// for every tenant that is never rate limited.
	require.Equal(t, 0, c.len())
}

func TestConsecutive429s_TenantsAreIndependent(t *testing.T) {
	c, _ := newTestConsecutive429s(t)

	require.Equal(t, 1, c.Observe("a", true))
	require.Equal(t, 2, c.Observe("a", true))
	require.Equal(t, 1, c.Observe("b", true))

	// Resetting b leaves a's streak intact.
	require.Equal(t, 0, c.Observe("b", false))
	require.Equal(t, 2, c.Get("a"))
	require.Equal(t, 0, c.Get("b"))
}

func TestConsecutive429s_EvictsIdleTenants(t *testing.T) {
	c, clock := newTestConsecutive429s(t)

	require.Equal(t, 1, c.Observe("idle", true))
	require.Equal(t, 1, c.len())

	// "active" is observed just before the sweep, so it must survive it, while
	// "idle" has by then been untouched for longer than idleTimeout.
	clock.Advance(testIdleTimeout + time.Second)
	require.Equal(t, 1, c.Observe("active", true))

	require.Equal(t, 1, c.len())
	require.Equal(t, 0, c.Get("idle"))
	require.Equal(t, 1, c.Get("active"))
}

func TestConsecutive429s_SweepsAtMostOncePerIdleTimeout(t *testing.T) {
	c, clock := newTestConsecutive429s(t)

	c.Observe("tenant", true)
	before := c.lastSweep.Load()

	// Not enough time has passed, so no sweep.
	clock.Advance(testIdleTimeout / 2)
	c.Observe("tenant", true)
	require.Equal(t, before, c.lastSweep.Load())

	// Crossing the interval sweeps once...
	clock.Advance(testIdleTimeout)
	c.Observe("tenant", true)
	swept := c.lastSweep.Load()
	require.Greater(t, swept, before)

	// ...and further calls at the same time do not sweep again.
	c.Observe("tenant", true)
	require.Equal(t, swept, c.lastSweep.Load())

	// The tenant was observed on every sweep, so it was never evicted.
	require.Equal(t, 4, c.Get("tenant"))
}

func TestConsecutive429s_Concurrent(t *testing.T) {
	c, _ := newTestConsecutive429s(t)

	const (
		goroutines = 16
		perRoutine = 200
	)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perRoutine; j++ {
				c.Observe("tenant", true)
				c.Get("tenant")
			}
		}()
	}
	wg.Wait()

	// Every observation was a rejection and nothing resets, so the total is exact
	// even though interleaved rejections and successes would not be.
	require.Equal(t, goroutines*perRoutine, c.Get("tenant"))
}
