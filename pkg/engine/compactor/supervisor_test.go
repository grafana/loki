package compactor

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestReconcileWorkers(t *testing.T) {
	set := func(tenants ...string) map[string]struct{} {
		out := make(map[string]struct{}, len(tenants))
		for _, tenant := range tenants {
			out[tenant] = struct{}{}
		}
		return out
	}
	enabledOnly := func(tenants ...string) func(string) bool {
		enabled := set(tenants...)
		return func(tenant string) bool {
			_, ok := enabled[tenant]
			return ok
		}
	}

	for _, tc := range []struct {
		name       string
		running    map[string]struct{}
		discovered map[string]struct{}
		enabled    func(string) bool
		wantStart  []string
		wantStop   []string
	}{
		{
			name:       "starts discovered enabled tenants that are not running",
			discovered: set("acme", "bravo"),
			enabled:    enabledOnly("acme", "bravo"),
			wantStart:  []string{"acme", "bravo"},
		},
		{
			name:       "does not start discovered tenants that are disabled",
			discovered: set("acme", "bravo"),
			enabled:    enabledOnly("acme"),
			wantStart:  []string{"acme"},
		},
		{
			name:       "does not start enabled tenants that are not discovered",
			discovered: set("acme"),
			enabled:    enabledOnly("acme", "bravo"),
			wantStart:  []string{"acme"},
		},
		{
			name:       "leaves running tenants that are discovered and enabled alone",
			running:    set("acme"),
			discovered: set("acme"),
			enabled:    enabledOnly("acme"),
		},
		{
			name:       "stops running tenants absent from discovery",
			running:    set("acme", "bravo"),
			discovered: set("acme"),
			enabled:    enabledOnly("acme", "bravo"),
			wantStop:   []string{"bravo"},
		},
		{
			name:       "stops running tenants that are disabled",
			running:    set("acme"),
			discovered: set("acme"),
			enabled:    enabledOnly(),
			wantStop:   []string{"acme"},
		},
		{
			name:       "starts and stops in the same pass",
			running:    set("acme"),
			discovered: set("bravo"),
			enabled:    enabledOnly("acme", "bravo"),
			wantStart:  []string{"bravo"},
			wantStop:   []string{"acme"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, stop := reconcileWorkers(tc.running, tc.discovered, tc.enabled)
			require.Equal(t, tc.wantStart, start)
			require.Equal(t, tc.wantStop, stop)
		})
	}
}

// fakeWorkers records which tenants have a worker running. Each worker blocks
// until its context is cancelled.
type fakeWorkers struct {
	mu     sync.Mutex
	starts map[string]int
	live   map[string]int
}

func newFakeWorkers() *fakeWorkers {
	return &fakeWorkers{starts: map[string]int{}, live: map[string]int{}}
}

func (f *fakeWorkers) run(ctx context.Context, tenant string) {
	f.mu.Lock()
	f.starts[tenant]++
	f.live[tenant]++
	f.mu.Unlock()
	<-ctx.Done()
	f.mu.Lock()
	f.live[tenant]--
	f.mu.Unlock()
}

func (f *fakeWorkers) liveTenants() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for tenant, n := range f.live {
		if n > 0 {
			out = append(out, tenant)
		}
	}
	slices.Sort(out)
	return out
}

func (f *fakeWorkers) startCounts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.starts)
}

func TestSupervisor(t *testing.T) {
	allEnabled := func(string) bool { return true }
	discoverFixed := func(tenants ...string) func(context.Context) (map[string]struct{}, error) {
		return func(context.Context) (map[string]struct{}, error) {
			out := make(map[string]struct{}, len(tenants))
			for _, tenant := range tenants {
				out[tenant] = struct{}{}
			}
			return out, nil
		}
	}

	t.Run("reconcile starts a worker per discovered tenant and stops a removed one", func(t *testing.T) {
		workers := newFakeWorkers()
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme", "bravo"), allEnabled, workers.run)
		defer s.stopAll()

		s.reconcile(t.Context())
		require.Eventually(t, func() bool {
			return slices.Equal([]string{"acme", "bravo"}, workers.liveTenants())
		}, 2*time.Second, 5*time.Millisecond)

		s.discover = discoverFixed("acme")
		s.reconcile(t.Context())
		require.Eventually(t, func() bool {
			return slices.Equal([]string{"acme"}, workers.liveTenants())
		}, 2*time.Second, 5*time.Millisecond)
	})

	t.Run("repeated reconcile does not start a duplicate worker", func(t *testing.T) {
		workers := newFakeWorkers()
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme"), allEnabled, workers.run)
		defer s.stopAll()

		s.reconcile(t.Context())
		s.reconcile(t.Context())
		require.Eventually(t, func() bool {
			return slices.Equal([]string{"acme"}, workers.liveTenants())
		}, 2*time.Second, 5*time.Millisecond)
		require.Equal(t, map[string]int{"acme": 1}, workers.startCounts())
	})

	t.Run("a discovery error leaves running workers alone and starts none", func(t *testing.T) {
		workers := newFakeWorkers()
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme"), allEnabled, workers.run)
		defer s.stopAll()

		s.reconcile(t.Context())
		require.Eventually(t, func() bool {
			return slices.Equal([]string{"acme"}, workers.liveTenants())
		}, 2*time.Second, 5*time.Millisecond)

		s.discover = func(context.Context) (map[string]struct{}, error) {
			return map[string]struct{}{"bravo": {}}, errors.New("read failed")
		}
		s.reconcile(t.Context())
		require.Equal(t, []string{"acme"}, workers.liveTenants())
		require.Equal(t, map[string]int{"acme": 1}, workers.startCounts())
	})

	t.Run("a discovery error still stops a disabled worker", func(t *testing.T) {
		workers := newFakeWorkers()
		enabled := true
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme"), func(string) bool { return enabled }, workers.run)
		defer s.stopAll()

		s.reconcile(t.Context())
		enabled = false
		s.discover = func(context.Context) (map[string]struct{}, error) { return nil, errors.New("read failed") }
		s.reconcile(t.Context())
		require.Eventually(t, func() bool { return len(s.running()) == 0 }, 2*time.Second, 5*time.Millisecond)
		require.Empty(t, workers.liveTenants())
	})

	t.Run("a worker that exits on its own is restarted on the next reconcile", func(t *testing.T) {
		var starts atomic.Int32
		exitFirstRun := func(ctx context.Context, _ string) {
			if starts.Add(1) == 1 {
				return
			}
			<-ctx.Done()
		}
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme"), allEnabled, exitFirstRun)
		defer s.stopAll()

		s.reconcile(t.Context())
		require.Eventually(t, func() bool { return len(s.running()) == 0 }, 2*time.Second, 5*time.Millisecond)

		s.reconcile(t.Context())
		require.Eventually(t, func() bool { return starts.Load() == 2 }, 2*time.Second, 5*time.Millisecond)
	})

	t.Run("a stopped tenant gets a new worker when it is re-enabled", func(t *testing.T) {
		workers := newFakeWorkers()
		enabled := true
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme"), func(string) bool { return enabled }, workers.run)
		defer s.stopAll()

		s.reconcile(t.Context())
		enabled = false
		s.reconcile(t.Context())
		require.Eventually(t, func() bool { return len(s.running()) == 0 }, 2*time.Second, 5*time.Millisecond)

		enabled = true
		s.reconcile(t.Context())
		require.Eventually(t, func() bool {
			return slices.Equal([]string{"acme"}, workers.liveTenants())
		}, 2*time.Second, 5*time.Millisecond)
		require.Equal(t, map[string]int{"acme": 2}, workers.startCounts())
	})

	t.Run("Run stops every worker and returns when ctx is cancelled", func(t *testing.T) {
		workers := newFakeWorkers()
		s := newTenantsSupervisor(log.NewNopLogger(), time.Hour, discoverFixed("acme", "bravo"), allEnabled, workers.run)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()

		require.Eventually(t, func() bool { return len(workers.liveTenants()) == 2 }, 2*time.Second, 5*time.Millisecond)
		cancel()

		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
		require.Empty(t, workers.liveTenants())
	})
}
