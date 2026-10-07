package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"
)

type testInstance struct {
	id     string
	state  ring.InstanceState
	tokens []uint32
}

// newTestInstances returns n ACTIVE instances with NumTokens tokens each. The
// tokens of instance i depend only on i, so the same instance keeps its tokens
// across rings.
func newTestInstances(from, n int) []testInstance {
	out := make([]testInstance, 0, n)
	for i := from; i < from+n; i++ {
		rnd := rand.New(rand.NewSource(int64(i)))
		tokens := make([]uint32, NumTokens)
		for j := range tokens {
			tokens[j] = rnd.Uint32()
		}
		slices.Sort(tokens)
		out = append(out, testInstance{id: fmt.Sprintf("index-gateway-%d", i), state: ring.ACTIVE, tokens: tokens})
	}
	return out
}

// newTestRing returns a running ring holding instances, whose addresses are
// their IDs.
func newTestRing(t *testing.T, instances []testInstance) *ring.Ring {
	t.Helper()
	descs := make(map[string]ring.InstanceDesc, len(instances))
	for _, inst := range instances {
		descs[inst.id] = ring.InstanceDesc{
			Id:                  inst.id,
			Addr:                inst.id,
			State:               inst.state,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-10 * time.Minute).Unix(),
			Tokens:              inst.tokens,
		}
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })
	require.NoError(t, kvStore.CAS(context.Background(), "ring", func(_ interface{}) (interface{}, bool, error) {
		return &ring.Desc{Ingesters: descs}, true, nil
	}))

	r, err := ring.NewWithStoreClientAndStrategy(ring.Config{
		KVStore:           kv.Config{Mock: kvStore},
		HeartbeatTimeout:  time.Hour,
		ReplicationFactor: ReplicationFactor,
	}, "index-gateway", "ring", kvStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), nil, log.NewNopLogger())
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), r))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), r) })
	require.Equal(t, len(instances), r.InstancesCount())
	return r
}

type indexKey struct{ tenant, table string }

func testIndexKeys() []indexKey {
	var keys []indexKey
	for tenant := range 200 {
		for table := 19500; table < 19530; table++ {
			keys = append(keys, indexKey{tenant: fmt.Sprint(tenant), table: fmt.Sprintf("index_%d", table)})
		}
	}
	return keys
}

// ownersOf returns the sorted owner addresses of every key.
func ownersOf(t *testing.T, r ring.ReadRing, keys []indexKey, op ring.Operation) map[indexKey][]string {
	t.Helper()
	o := NewIndexOwnership(r)
	out := make(map[indexKey][]string, len(keys))
	for _, k := range keys {
		rs, err := o.Owners(k.tenant, k.table, op)
		require.NoError(t, err)
		addrs := rs.GetAddresses()
		slices.Sort(addrs)
		out[k] = addrs
	}
	return out
}

func TestIndexOwnershipKey(t *testing.T) {
	require.Equal(t, IndexOwnershipKey("tenant-a", "index_19500"), IndexOwnershipKey("tenant-a", "index_19500"))
	require.NotEqual(t, IndexOwnershipKey("tenant-a", "index_19500"), IndexOwnershipKey("tenant-a", "index_19501"))
	require.NotEqual(t, IndexOwnershipKey("ab", "c"), IndexOwnershipKey("a", "bc"))
}

func TestIndexOwnership_Spread(t *testing.T) {
	const n = 6
	instances := newTestInstances(0, n)
	keys := testIndexKeys()
	owners := ownersOf(t, newTestRing(t, instances), keys, IndexOwnersRead)

	perInstance := map[string]int{}
	for _, addrs := range owners {
		require.Len(t, addrs, ReplicationFactor)
		for _, a := range addrs {
			perInstance[a]++
		}
	}

	// Every instance owns about RF/n of the indexes.
	expected := float64(len(keys)) * ReplicationFactor / n
	for _, inst := range instances {
		got := float64(perInstance[inst.id])
		require.InDelta(t, expected, got, 0.3*expected, "instance %s", inst.id)
	}
}

func TestIndexOwnership_InstanceJoins(t *testing.T) {
	const n = 6
	before := newTestInstances(0, n)
	joining := newTestInstances(n, 1)[0]
	keys := testIndexKeys()

	ownersBefore := ownersOf(t, newTestRing(t, before), keys, IndexOwnersRead)
	ownersAfter := ownersOf(t, newTestRing(t, append(slices.Clone(before), joining)), keys, IndexOwnersRead)

	changed := 0
	for _, k := range keys {
		b, a := ownersBefore[k], ownersAfter[k]
		if slices.Equal(b, a) {
			continue
		}
		changed++
		// The only change is that the new instance replaces one old owner.
		require.Contains(t, a, joining.id)
		require.Len(t, a, ReplicationFactor)
		require.Len(t, slices.DeleteFunc(slices.Clone(b), func(s string) bool { return slices.Contains(a, s) }), 1)
	}

	// About RF/(n+1) of the indexes move to the new instance.
	expected := float64(len(keys)) * ReplicationFactor / (n + 1)
	require.InDelta(t, expected, float64(changed), 0.3*expected)
}

func TestIndexOwnership_InstanceLeaves(t *testing.T) {
	const n = 6
	before := newTestInstances(0, n)
	leaving := before[2]
	after := slices.DeleteFunc(slices.Clone(before), func(i testInstance) bool { return i.id == leaving.id })
	keys := testIndexKeys()

	ownersBefore := ownersOf(t, newTestRing(t, before), keys, IndexOwnersRead)
	ownersAfter := ownersOf(t, newTestRing(t, after), keys, IndexOwnersRead)

	for _, k := range keys {
		b, a := ownersBefore[k], ownersAfter[k]
		if !slices.Contains(b, leaving.id) {
			// Indexes the leaving instance didn't own keep their owners.
			require.Equal(t, b, a)
			continue
		}
		// The others keep their indexes; one new owner takes the leaver's place.
		require.Len(t, a, ReplicationFactor)
		require.NotContains(t, a, leaving.id)
		for _, owner := range b {
			if owner != leaving.id {
				require.Contains(t, a, owner)
			}
		}
	}
}

func TestIndexOwnership_JoiningInstance(t *testing.T) {
	const n = 6
	before := newTestInstances(0, n)
	joining := newTestInstances(n, 1)[0]
	keys := testIndexKeys()

	ownersBefore := ownersOf(t, newTestRing(t, before), keys, IndexOwnersRead)
	ownersOnceActive := ownersOf(t, newTestRing(t, append(slices.Clone(before), joining)), keys, IndexesSync)

	joining.state = ring.JOINING
	withJoining := newTestRing(t, append(slices.Clone(before), joining))

	// For loading, a JOINING instance already owns what it will own once ACTIVE.
	require.Equal(t, ownersOnceActive, ownersOf(t, withJoining, keys, IndexesSync))

	// For reading and dropping, the old owners keep their indexes until it is ACTIVE.
	require.Equal(t, ownersBefore, ownersOf(t, withJoining, keys, IndexOwnersRead))
}

func TestIndexOwnershipFilter(t *testing.T) {
	instances := newTestInstances(0, 6)
	r := newTestRing(t, instances)
	keys := testIndexKeys()
	owners := ownersOf(t, r, keys, IndexesSync)

	var tenants []string
	for tenant := range 200 {
		tenants = append(tenants, fmt.Sprint(tenant))
	}

	const table = "index_19510"
	count := map[string]int{}
	for _, inst := range instances {
		got, err := NewIndexOwnershipFilter(r, inst.id).FilterTenants(table, tenants)
		require.NoError(t, err)
		for _, tenant := range got {
			require.Contains(t, owners[indexKey{tenant: tenant, table: table}], inst.id)
			count[tenant]++
		}
	}
	// Every tenant's index in the table is kept by exactly RF instances.
	for _, tenant := range tenants {
		require.Equal(t, ReplicationFactor, count[tenant], "tenant %s", tenant)
	}

	t.Run("instance not in the ring", func(t *testing.T) {
		_, err := NewIndexOwnershipFilter(r, "index-gateway-99").FilterTenants(table, tenants)
		require.ErrorIs(t, err, errGatewayUnhealthy)
	})
}

type preloaderFunc func(ctx context.Context) error

func (f preloaderFunc) PreloadIndexes(ctx context.Context) error { return f(ctx) }

func TestNewOwnedIndexPreload(t *testing.T) {
	blockUntilDone := preloaderFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	t.Run("preload succeeds", func(t *testing.T) {
		called := false
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: time.Minute}, nil, preloaderFunc(func(context.Context) error {
			called = true
			return nil
		}), log.NewNopLogger())
		require.NoError(t, fn(context.Background()))
		require.True(t, called)
	})

	t.Run("preload timeout becomes ACTIVE anyway", func(t *testing.T) {
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: 50 * time.Millisecond}, nil, blockUntilDone, log.NewNopLogger())
		require.NoError(t, fn(context.Background()))
	})

	t.Run("preload error fails", func(t *testing.T) {
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: time.Minute}, nil, preloaderFunc(func(context.Context) error {
			return errors.New("download failed")
		}), log.NewNopLogger())
		require.ErrorContains(t, fn(context.Background()), "download failed")
	})

	t.Run("cancelled start fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: time.Minute}, nil, blockUntilDone, log.NewNopLogger())
		require.ErrorIs(t, fn(ctx), context.DeadlineExceeded)
	})

	t.Run("waits for a stable ring first", func(t *testing.T) {
		r := newTestRing(t, newTestInstances(0, 3))
		start := time.Now()
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{
			WaitStabilityMinDuration: time.Second,
			WaitStabilityMaxDuration: 10 * time.Second,
		}, r, preloaderFunc(func(context.Context) error { return nil }), log.NewNopLogger())
		require.NoError(t, fn(context.Background()))
		require.GreaterOrEqual(t, time.Since(start), time.Second)
	})
}

func TestPerIndexOwnershipConfig_Validate(t *testing.T) {
	cfg := PerIndexOwnershipConfig{Enabled: true, WaitStabilityMinDuration: time.Minute, WaitStabilityMaxDuration: 5 * time.Minute}
	require.NoError(t, cfg.Validate(RingMode))
	require.Error(t, cfg.Validate(SimpleMode))

	bad := cfg
	bad.WaitStabilityMaxDuration = time.Second
	require.Error(t, bad.Validate(RingMode))

	bad = cfg
	bad.RingCheckPeriod = -time.Second
	require.Error(t, bad.Validate(RingMode))

	require.NoError(t, (&PerIndexOwnershipConfig{}).Validate(SimpleMode))
}
