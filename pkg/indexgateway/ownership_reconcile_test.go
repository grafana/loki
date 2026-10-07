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

// newMutableTestRing returns a ring with replication factor rf over instances,
// whose addresses are their IDs.
func newMutableTestRing(t *testing.T, rf int, instances []testInstance) *mutableTestRing {
	t.Helper()
	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })

	r, err := ring.NewWithStoreClientAndStrategy(ring.Config{
		KVStore:           kv.Config{Mock: kvStore},
		HeartbeatTimeout:  time.Hour,
		ReplicationFactor: rf,
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

// reconcileSim simulates the indexes each index gateway holds as they
// reconcile: load what FilterTenants keeps, then drop what
// FilterTenantsToDrop returns, as a query readiness run does.
type reconcileSim struct {
	t       *testing.T
	r       ring.ReadRing
	tenants []string
	tables  []string
	held    map[string]map[indexKey]bool // by instance address
}

func newReconcileSim(t *testing.T, r ring.ReadRing) *reconcileSim {
	sim := &reconcileSim{t: t, r: r, held: map[string]map[indexKey]bool{}}
	for i := range 40 {
		sim.tenants = append(sim.tenants, fmt.Sprintf("tenant-%d", i))
	}
	for table := 19500; table < 19505; table++ {
		sim.tables = append(sim.tables, fmt.Sprintf("index_%d", table))
	}
	return sim
}

func (s *reconcileSim) keys() []indexKey {
	var keys []indexKey
	for _, table := range s.tables {
		for _, tenant := range s.tenants {
			keys = append(keys, indexKey{tenant: tenant, table: table})
		}
	}
	return keys
}

// reconcile runs one reconcile on each of instances and returns what each
// one dropped.
func (s *reconcileSim) reconcile(instances []testInstance) map[string][]indexKey {
	s.t.Helper()
	dropped := map[string][]indexKey{}
	for _, inst := range instances {
		f := NewIndexOwnershipFilter(s.r, inst.id)
		held := s.held[inst.id]
		if held == nil {
			held = map[indexKey]bool{}
			s.held[inst.id] = held
		}
		for _, table := range s.tables {
			load, err := f.FilterTenants(table, s.tenants)
			require.NoError(s.t, err)
			for _, tenant := range load {
				held[indexKey{tenant: tenant, table: table}] = true
			}

			var heldInTable []string
			for k := range held {
				if k.table == table {
					heldInTable = append(heldInTable, k.tenant)
				}
			}
			drop, err := f.FilterTenantsToDrop(table, heldInTable)
			require.NoError(s.t, err)
			for _, tenant := range drop {
				k := indexKey{tenant: tenant, table: table}
				delete(held, k)
				dropped[inst.id] = append(dropped[inst.id], k)
			}
		}
	}
	return dropped
}

// requireServed checks that every index is held by every instance that
// clients route it to (IndexOwnersRead).
func (s *reconcileSim) requireServed() {
	s.t.Helper()
	for k, owners := range ownersOf(s.t, s.r, s.keys(), IndexOwnersRead) {
		for _, owner := range owners {
			require.True(s.t, s.held[owner][k], "%v is routed to %s, which does not hold it", k, owner)
		}
	}
}

// requireHoldsOnlyOwned checks that each instance holds exactly the indexes
// it owns for routing or loading.
func (s *reconcileSim) requireHoldsOnlyOwned(instances []testInstance) {
	s.t.Helper()
	read := ownersOf(s.t, s.r, s.keys(), IndexOwnersRead)
	syncOwners := ownersOf(s.t, s.r, s.keys(), IndexesSync)
	for _, inst := range instances {
		for _, k := range s.keys() {
			owned := slices.Contains(read[k], inst.id) || slices.Contains(syncOwners[k], inst.id)
			require.Equal(s.t, owned, s.held[inst.id][k], "instance %s, index %v", inst.id, k)
		}
	}
}

func TestIndexOwnershipReconcile_Scale(t *testing.T) {
	for _, tc := range []struct {
		rf         int
		start, add int
	}{
		// Scale 1 -> 3 -> 2. With RF 1 each index has one owner, so
		// ownership moves on every step.
		{rf: 1, start: 1, add: 2},
		// The same with the default RF of 3, on more instances so that
		// ownership moves: 3 -> 6 -> 5.
		{rf: ReplicationFactor, start: 3, add: 3},
	} {
		t.Run(fmt.Sprintf("rf=%d %d->%d->%d", tc.rf, tc.start, tc.start+tc.add, tc.start+tc.add-1), func(t *testing.T) {
			initial := newTestInstances(0, tc.start)
			r := newMutableTestRing(t, tc.rf, initial)
			sim := newReconcileSim(t, r)

			// The first instances own and hold everything.
			require.Empty(t, sim.reconcile(initial))
			sim.requireServed()
			for _, inst := range initial {
				require.Equal(t, tc.rf*len(sim.keys())/tc.start, len(sim.held[inst.id]))
			}

			// New instances join. While they are JOINING they load their
			// share, and nobody drops anything.
			joining := withState(newTestInstances(tc.start, tc.add), ring.JOINING)
			all := append(slices.Clone(initial), joining...)
			r.set(all)
			require.Empty(t, sim.reconcile(all), "nothing is dropped while new owners are JOINING")
			sim.requireServed()
			for _, inst := range joining {
				require.NotZero(t, len(sim.held[inst.id]), "joining instance %s preloads its share", inst.id)
			}

			// Once they are ACTIVE, the old owners drop what they no longer
			// own, and every index is still held where clients route it.
			all = withState(all, ring.ACTIVE)
			r.set(all)
			dropped := sim.reconcile(all)
			for _, inst := range initial {
				require.NotEmpty(t, dropped[inst.id], "instance %s sheds ownership", inst.id)
			}
			sim.requireServed()
			sim.requireHoldsOnlyOwned(all)

			// One instance starts leaving: nobody drops anything, the leaving
			// instance included.
			leaving := all[len(all)-1]
			leaving.state = ring.LEAVING
			remaining := slices.Clone(all[:len(all)-1])
			r.set(append(slices.Clone(remaining), leaving))
			require.Empty(t, sim.reconcile(append(slices.Clone(remaining), leaving)), "nothing is dropped while an instance is LEAVING")

			// Once it is gone, the rest load what they gained.
			r.set(remaining)
			sim.reconcile(remaining)
			delete(sim.held, leaving.id)
			sim.requireServed()
			sim.requireHoldsOnlyOwned(remaining)
		})
	}
}

func TestIndexOwnershipFilter_FilterTenantsToDrop(t *testing.T) {
	instances := newTestInstances(0, 4)
	tenants := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	const table = "index_19500"

	t.Run("an ACTIVE instance drops what it does not own", func(t *testing.T) {
		r := newMutableTestRing(t, 1, instances)
		f := NewIndexOwnershipFilter(r, instances[0].id)
		drop, err := f.FilterTenantsToDrop(table, tenants)
		require.NoError(t, err)
		owned, err := f.FilterTenants(table, tenants)
		require.NoError(t, err)
		require.NotEmpty(t, drop)
		require.ElementsMatch(t, tenants, append(drop, owned...))
	})

	for _, state := range []ring.InstanceState{ring.JOINING, ring.LEAVING} {
		t.Run(fmt.Sprintf("a %s instance drops nothing", state), func(t *testing.T) {
			withSelf := slices.Clone(instances)
			withSelf[0].state = state
			r := newMutableTestRing(t, 1, withSelf)
			drop, err := NewIndexOwnershipFilter(r, instances[0].id).FilterTenantsToDrop(table, tenants)
			require.NoError(t, err)
			require.Empty(t, drop)
		})
	}

	t.Run("an instance missing from the ring drops nothing", func(t *testing.T) {
		r := newMutableTestRing(t, 1, instances)
		drop, err := NewIndexOwnershipFilter(r, "not-in-the-ring").FilterTenantsToDrop(table, tenants)
		require.NoError(t, err)
		require.Empty(t, drop)
	})

	t.Run("an empty ring drops nothing", func(t *testing.T) {
		r := newMutableTestRing(t, 1, nil)
		drop, err := NewIndexOwnershipFilter(r, instances[0].id).FilterTenantsToDrop(table, tenants)
		require.Error(t, err)
		require.Empty(t, drop)
	})
}

// countingPreloader counts PreloadIndexes calls and fails them while err is set.
type countingPreloader struct {
	mtx   sync.Mutex
	calls int
	err   error
}

func (p *countingPreloader) PreloadIndexes(context.Context) error {
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
	r := newMutableTestRing(t, ReplicationFactor, instances)
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
