package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"
)

// mutableTestRing is a running ring whose instances a test can change.
type mutableTestRing struct {
	*ring.Ring
	t          *testing.T
	kvStore    *consul.Client
	registered int64
}

// newMutableTestRing returns a ring over instances, whose addresses are their
// IDs.
func newMutableTestRing(t *testing.T, instances []testInstance) *mutableTestRing {
	t.Helper()
	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })

	r, err := ring.NewWithStoreClientAndStrategy(ring.Config{
		KVStore:           kv.Config{Mock: kvStore},
		HeartbeatTimeout:  time.Hour,
		ReplicationFactor: ReplicationFactor,
	}, "index-gateway", "ring", kvStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), nil, log.NewNopLogger())
	require.NoError(t, err)

	m := &mutableTestRing{Ring: r, t: t, kvStore: kvStore, registered: time.Now().Add(-10 * time.Minute).Unix()}
	m.writeDesc(instances, time.Now().Unix())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), r))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), r) })
	m.awaitInstances(instances)
	return m
}

func (m *mutableTestRing) writeDesc(instances []testInstance, heartbeat int64) {
	descs := make(map[string]ring.InstanceDesc, len(instances))
	for _, inst := range instances {
		descs[inst.id] = ring.InstanceDesc{
			Id:                  inst.id,
			Addr:                inst.id,
			State:               inst.state,
			Timestamp:           heartbeat,
			RegisteredTimestamp: m.registered,
			Tokens:              inst.tokens,
		}
	}
	require.NoError(m.t, m.kvStore.CAS(context.Background(), "ring", func(_ interface{}) (interface{}, bool, error) {
		return &ring.Desc{Ingesters: descs}, true, nil
	}))
}

// set replaces the ring's instances and waits until the ring sees them.
func (m *mutableTestRing) set(instances []testInstance) {
	m.t.Helper()
	m.writeDesc(instances, time.Now().Unix())
	m.awaitInstances(instances)
}

// heartbeat updates only the heartbeat timestamps of instances.
func (m *mutableTestRing) heartbeat(instances []testInstance, at int64) {
	m.writeDesc(instances, at)
}

func (m *mutableTestRing) awaitInstances(instances []testInstance) {
	m.t.Helper()
	want := map[string]ring.InstanceState{}
	for _, inst := range instances {
		want[inst.id] = inst.state
	}
	require.Eventually(m.t, func() bool {
		set, err := m.GetAllHealthy(IndexesSync)
		if err != nil {
			return len(want) == 0
		}
		got := map[string]ring.InstanceState{}
		for _, inst := range set.Instances {
			got[inst.Id] = inst.State
		}
		return maps.Equal(want, got)
	}, 5*time.Second, 5*time.Millisecond)
}

func withState(instances []testInstance, state ring.InstanceState) []testInstance {
	out := slices.Clone(instances)
	for i := range out {
		out[i].state = state
	}
	return out
}

func TestIndexOwnershipFilter_FilterTenantsToDrop(t *testing.T) {
	instances := newTestInstances(0, 6)
	const table = "index_19510"
	var tenants []string
	for tenant := range 200 {
		tenants = append(tenants, fmt.Sprint(tenant))
	}

	t.Run("an ACTIVE instance drops exactly what it does not own", func(t *testing.T) {
		r := newTestRing(t, instances)
		for _, inst := range instances {
			f := NewIndexOwnershipFilter(r, inst.id)
			drop, err := f.FilterTenantsToDrop(table, tenants)
			require.NoError(t, err)
			owned, err := f.FilterTenants(table, tenants)
			require.NoError(t, err)
			require.NotEmpty(t, drop)
			require.ElementsMatch(t, tenants, append(drop, owned...), "instance %s", inst.id)
		}
	})

	newcomer := newTestInstances(len(instances), 1)[0]
	leaver := len(instances) - 1
	for _, tc := range []struct {
		name   string
		during func() []testInstance
	}{
		{"a new instance is JOINING", func() []testInstance {
			joining := newcomer
			joining.state = ring.JOINING
			return append(slices.Clone(instances), joining)
		}},
		{"an instance is LEAVING", func() []testInstance {
			out := slices.Clone(instances)
			out[leaver].state = ring.LEAVING
			return out
		}},
	} {
		t.Run(fmt.Sprintf("while %s, no instance drops what it holds", tc.name), func(t *testing.T) {
			before := newTestRing(t, instances)
			during := newTestRing(t, tc.during())
			for _, inst := range instances {
				held, err := NewIndexOwnershipFilter(before, inst.id).FilterTenants(table, tenants)
				require.NoError(t, err)
				drop, err := NewIndexOwnershipFilter(during, inst.id).FilterTenantsToDrop(table, held)
				require.NoError(t, err)
				require.Empty(t, drop, "instance %s", inst.id)
			}
		})
	}

	t.Run("once a new instance is ACTIVE, the others drop only what moved to it", func(t *testing.T) {
		before := newTestRing(t, instances)
		after := newTestRing(t, append(slices.Clone(instances), newcomer))
		var dropped int
		for _, inst := range instances {
			held, err := NewIndexOwnershipFilter(before, inst.id).FilterTenants(table, tenants)
			require.NoError(t, err)
			drop, err := NewIndexOwnershipFilter(after, inst.id).FilterTenantsToDrop(table, held)
			require.NoError(t, err)
			for _, tenant := range drop {
				owners, err := NewIndexOwnership(after).Owners(tenant, table, IndexOwnersRead)
				require.NoError(t, err)
				require.True(t, owners.Includes(newcomer.id), "instance %s drops tenant %s, which did not move to %s", inst.id, tenant, newcomer.id)
			}
			dropped += len(drop)
		}
		require.NotZero(t, dropped)
	})

	for _, state := range []ring.InstanceState{ring.JOINING, ring.LEAVING} {
		t.Run(fmt.Sprintf("a %s instance drops nothing", state), func(t *testing.T) {
			withSelf := slices.Clone(instances)
			withSelf[0].state = state
			r := newTestRing(t, withSelf)
			drop, err := NewIndexOwnershipFilter(r, instances[0].id).FilterTenantsToDrop(table, tenants)
			require.NoError(t, err)
			require.Empty(t, drop)
		})
	}

	t.Run("an instance missing from the ring drops nothing", func(t *testing.T) {
		r := newTestRing(t, instances)
		drop, err := NewIndexOwnershipFilter(r, "not-in-the-ring").FilterTenantsToDrop(table, tenants)
		require.NoError(t, err)
		require.Empty(t, drop)
	})

	t.Run("an empty ring drops nothing", func(t *testing.T) {
		r := newTestRing(t, nil)
		drop, err := NewIndexOwnershipFilter(r, instances[0].id).FilterTenantsToDrop(table, tenants)
		require.Error(t, err)
		require.Empty(t, drop)
	})
}

// countingPreloader counts EnsureQueryReadiness calls and fails them while err is set.
type countingPreloader struct {
	mtx   sync.Mutex
	calls int
	err   error
}

func (p *countingPreloader) EnsureQueryReadiness(context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.calls++
	return p.err
}

func (p *countingPreloader) count() int {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.calls
}

func (p *countingPreloader) setErr(err error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.err = err
}

func TestIndexOwnershipWatcher(t *testing.T) {
	const period = 10 * time.Millisecond
	instances := newTestInstances(0, 3)
	r := newMutableTestRing(t, instances)
	preloader := &countingPreloader{}
	w := NewIndexOwnershipWatcher(r, period, preloader, log.NewNopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Nothing happens before the initial preload has run, even if the ring
	// changes.
	joining := append(slices.Clone(instances), withState(newTestInstances(3, 1), ring.JOINING)...)
	r.set(joining)
	time.Sleep(10 * period)
	require.Zero(t, preloader.count())

	// The first check after Activate reconciles.
	w.Activate()
	require.Eventually(t, func() bool { return preloader.count() == 1 }, time.Second, period)

	// A heartbeat alone does not.
	r.heartbeat(joining, time.Now().Add(time.Second).Unix())
	time.Sleep(10 * period)
	require.Equal(t, 1, preloader.count())

	// A state change does.
	active := withState(joining, ring.ACTIVE)
	r.set(active)
	require.Eventually(t, func() bool { return preloader.count() == 2 }, time.Second, period)

	// A failed reconcile is retried on the next checks until it succeeds.
	preloader.setErr(errors.New("failed"))
	r.set(active[:3])
	require.Eventually(t, func() bool { return preloader.count() >= 5 }, time.Second, period)
	preloader.setErr(nil)
	time.Sleep(5 * period)
	settled := preloader.count()
	time.Sleep(10 * period)
	require.Equal(t, settled, preloader.count(), "it stops once the reconcile succeeds")
}
