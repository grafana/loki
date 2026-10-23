package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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
		rs, err := o.Owners(k.tenant, k.table, WholeIndexShard, op)
		require.NoError(t, err)
		addrs := rs.GetAddresses()
		slices.Sort(addrs)
		out[k] = addrs
	}
	return out
}

func TestIndexOwnershipKey(t *testing.T) {
	whole := WholeIndexShard
	require.Equal(t, IndexOwnershipKey("tenant-a", "index_19500", whole), IndexOwnershipKey("tenant-a", "index_19500", whole))
	require.NotEqual(t, IndexOwnershipKey("tenant-a", "index_19500", whole), IndexOwnershipKey("tenant-a", "index_19501", whole))
	require.NotEqual(t, IndexOwnershipKey("ab", "c", whole), IndexOwnershipKey("a", "bc", whole))

	// the zero shard and a factor of 1 are the whole index
	require.Equal(t, IndexOwnershipKey("tenant-a", "index_19500", whole), IndexOwnershipKey("tenant-a", "index_19500", index.ShardAnnotation{}))
	require.Equal(t, IndexOwnershipKey("tenant-a", "index_19500", whole), IndexOwnershipKey("tenant-a", "index_19500", index.NewShard(0, 1)))

	// each shard of a split index has its own key
	keys := map[uint32]struct{}{}
	for _, shard := range IndexShards(8) {
		keys[IndexOwnershipKey("tenant-a", "index_19500", shard)] = struct{}{}
	}
	require.Len(t, keys, 8)
	require.NotEqual(t, IndexOwnershipKey("tenant-a", "index_19500", index.NewShard(0, 8)), IndexOwnershipKey("tenant-a", "index_19500", whole))
	require.NotEqual(t, IndexOwnershipKey("tenant-a", "index_19500", index.NewShard(0, 8)), IndexOwnershipKey("tenant-a", "index_19500", index.NewShard(0, 4)))
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
		got, err := NewIndexOwnershipFilter(r, inst.id, nil).FilterTenants(table, tenants)
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
		_, err := NewIndexOwnershipFilter(r, "index-gateway-99", nil).FilterTenants(table, tenants)
		require.ErrorIs(t, err, errGatewayUnhealthy)
	})
}

type queryReadinessFunc func(ctx context.Context) error

func (f queryReadinessFunc) EnsureQueryReadiness(ctx context.Context) error { return f(ctx) }

func TestNewOwnedIndexPreload(t *testing.T) {
	blockUntilDone := queryReadinessFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	t.Run("preload succeeds", func(t *testing.T) {
		called := false
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: time.Minute}, nil, queryReadinessFunc(func(context.Context) error {
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
		fn := NewOwnedIndexPreload(PerIndexOwnershipConfig{PreloadTimeout: time.Minute}, nil, queryReadinessFunc(func(context.Context) error {
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
		}, r, queryReadinessFunc(func(context.Context) error { return nil }), log.NewNopLogger())
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

// shardTestTables returns the tables of two periods: up to index_19999 split
// into k shards, and from index_20000 not split.
func shardTestTables(k int) config.TableRanges {
	period := func(k int) *config.PeriodConfig {
		return &config.PeriodConfig{IndexTables: config.IndexPeriodicTableConfig{
			ShardedPathPrefix:   "index_sharded/",
			ShardFactor:         k,
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
		}}
	}
	return config.TableRanges{
		{Start: 0, End: 19999, PeriodConfig: period(k)},
		{Start: 20000, End: math.MaxInt64, PeriodConfig: period(0)},
	}
}

func TestIndexShards(t *testing.T) {
	require.Equal(t, []index.ShardAnnotation{WholeIndexShard}, IndexShards(0))
	require.Equal(t, []index.ShardAnnotation{WholeIndexShard}, IndexShards(1))
	require.Equal(t, []index.ShardAnnotation{index.NewShard(0, 2), index.NewShard(1, 2)}, IndexShards(2))
	require.Len(t, IndexShards(8), 8)
}

func TestShardsOverlapping(t *testing.T) {
	shard := func(x, of uint32) index.ShardAnnotation { return index.NewShard(x, of) }
	for _, tc := range []struct {
		desc          string
		k             int
		from, through model.Fingerprint
		want          []index.ShardAnnotation
	}{
		{desc: "unsplit index, any range", k: 1, from: 1 << 62, through: 1<<62 + 10, want: []index.ShardAnnotation{WholeIndexShard}},
		{desc: "whole space", k: 4, from: 0, through: math.MaxUint64, want: IndexShards(4)},
		{desc: "power of two shard of the same factor", k: 4, from: 2 << 62, through: 3 << 62, want: []index.ShardAnnotation{shard(2, 4)}},
		{desc: "power of two shard of a smaller factor", k: 4, from: 1 << 63, through: math.MaxUint64, want: []index.ShardAnnotation{shard(2, 4), shard(3, 4)}},
		{desc: "power of two shard of a larger factor", k: 4, from: 5 << 61, through: 6 << 61, want: []index.ShardAnnotation{shard(2, 4)}},
		{desc: "bounded range inside one shard", k: 4, from: 100, through: 200, want: []index.ShardAnnotation{shard(0, 4)}},
		{desc: "bounded range across a shard boundary", k: 4, from: 1<<62 - 1, through: 1<<62 + 1, want: []index.ShardAnnotation{shard(0, 4), shard(1, 4)}},
		{desc: "bounded range ending at a boundary is exclusive", k: 4, from: 10, through: 1 << 62, want: []index.ShardAnnotation{shard(0, 4)}},
		{desc: "empty range", k: 4, from: 10, through: 10, want: nil},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.want, ShardsOverlapping(tc.k, tc.from, tc.through))
		})
	}
}

func TestIndexFileFingerprintBounds(t *testing.T) {
	full := model.Fingerprint(math.MaxUint64)
	for _, tc := range []struct {
		name          string
		from, through model.Fingerprint
	}{
		{name: "1760000000000000000-compactor-1-10-ff.tsdb.gz", from: 0, through: full},
		{name: "1760000000000000000-compactor-1-10-ff.tsdb", from: 0, through: full},
		{name: "1760000000000000000-compactor-1-10-ff-0_of_4.tsdb.gz", from: 0, through: 1 << 62},
		{name: "1760000000000000000-compactor-1-10-ff-3_of_4.tsdb.gz", from: 3 << 62, through: full},
		{name: "some/dir/1760000000000000000-compactor-1-10-ff-1_of_2.tsdb", from: 1 << 63, through: full},
		{name: "1760000000000000000-compactor-1-10-ff-0_of_1.tsdb.gz", from: 0, through: full},
		{name: "1700000000-ingester-0.tsdb.gz", from: 0, through: full},
		{name: "not-an-index-file", from: 0, through: full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, through := IndexFileFingerprintBounds(tc.name)
			require.Equal(t, tc.from, from)
			require.Equal(t, tc.through, through)
		})
	}
}

// TestIndexOwnershipFilter_Shards checks that with a shard factor the tenant
// filter keeps a tenant when any of its shards is owned, the file filter keeps
// the files whose range overlaps an owned shard (unsplit files always), and
// the drop filter drops a tenant only when none of its shards is owned. Every
// instance together must own every shard replication-factor times. A table of
// a period that is not split is owned whole.
func TestIndexOwnershipFilter_Shards(t *testing.T) {
	const (
		tenant       = "tenant-29"
		table        = "index_19500"
		unsplitTable = "index_20000"
		k            = 8
	)
	instances := newTestInstances(0, 6)
	r := newTestRing(t, instances)
	tables := shardTestTables(k)
	ownership := NewIndexOwnership(r)

	file := func(shard string) storage.IndexFile {
		name := "1760000000000000000-compactor-1-10-ff"
		if shard != "" {
			name += "-" + shard
		}
		return storage.IndexFile{Name: name + ".tsdb.gz"}
	}
	unsplit := file("")
	shardFiles := make([]storage.IndexFile, 0, k)
	for _, shard := range IndexShards(k) {
		shardFiles = append(shardFiles, file(shard.String()))
	}
	files := append([]storage.IndexFile{unsplit}, shardFiles...)

	keptBy := map[string]int{}
	var tenantOwners, droppers int
	for _, inst := range instances {
		f := NewIndexOwnershipFilter(r, inst.id, tables)
		owned, err := ownership.OwnedShards(inst.id, tenant, table, k, IndexesSync)
		require.NoError(t, err)

		ownsWhole, err := ownership.Owns(inst.id, tenant, unsplitTable, WholeIndexShard, IndexesSync)
		require.NoError(t, err)
		keptWhole, err := f.FilterTenants(unsplitTable, []string{tenant})
		require.NoError(t, err)
		require.Equal(t, ownsWhole, containsString(keptWhole, tenant))

		kept, err := f.FilterTenants(table, []string{tenant})
		require.NoError(t, err)
		drop, err := f.FilterTenantsToDrop(table, []string{tenant})
		require.NoError(t, err)
		keptFiles, err := f.FilterFiles(table, tenant, files)
		require.NoError(t, err)

		if len(owned) == 0 {
			require.NotContains(t, kept, tenant)
			require.Equal(t, []string{tenant}, drop)
			require.Empty(t, keptFiles)
			droppers++
			continue
		}
		tenantOwners++
		require.Contains(t, kept, tenant)
		require.Empty(t, drop)

		// the unsplit file and exactly the files of the owned shards
		require.Contains(t, keptFiles, unsplit)
		require.Len(t, keptFiles, len(owned)+1)
		for _, shard := range owned {
			require.Contains(t, keptFiles, file(shard.String()))
			keptBy[shard.String()]++
		}
	}

	// every shard has ReplicationFactor owners across the ring
	require.Len(t, keptBy, k)
	for shard, n := range keptBy {
		require.Equal(t, ReplicationFactor, n, shard)
	}
	// with 8 shards at RF 3 over 6 instances, more instances own a shard than
	// would own the unsplit index
	require.Greater(t, tenantOwners, ReplicationFactor)
	require.Equal(t, len(instances), tenantOwners+droppers)
}

// TestIndexOwnershipFilter_FilesOfReplacedOwnerAreKept checks that an instance
// keeps a shard's files while its new owner is still JOINING.
func TestIndexOwnershipFilter_FilesOfReplacedOwnerAreKept(t *testing.T) {
	const (
		tenant = "tenant"
		table  = "index_19500"
		k      = 4
	)
	tables := shardTestTables(k)
	before := newTestInstances(0, 6)
	joining := newTestInstances(6, 1)[0]
	joining.state = ring.JOINING
	after := append(append([]testInstance(nil), before...), joining)

	rBefore := newTestRing(t, before)
	rAfter := newTestRing(t, after)
	ownershipBefore, ownershipAfter := NewIndexOwnership(rBefore), NewIndexOwnership(rAfter)

	var checked int
	for _, shard := range IndexShards(k) {
		// the joining instance takes the shard from someone for loading
		ownsForSync, err := ownershipAfter.Owns(joining.id, tenant, table, shard, IndexesSync)
		require.NoError(t, err)
		if !ownsForSync {
			continue
		}
		rsBefore, err := ownershipBefore.Owners(tenant, table, shard, IndexesSync)
		require.NoError(t, err)
		rsAfterSync, err := ownershipAfter.Owners(tenant, table, shard, IndexesSync)
		require.NoError(t, err)
		for _, addr := range rsBefore.GetAddresses() {
			if rsAfterSync.Includes(addr) {
				continue
			}
			// addr lost the shard for loading, but still serves it until the
			// joining instance is ACTIVE, so it keeps the shard's file
			checked++
			f := NewIndexOwnershipFilter(rAfter, addr, tables)
			name := "1760000000000000000-compactor-1-10-ff-" + shard.String() + ".tsdb.gz"
			kept, err := f.FilterFiles(table, tenant, []storage.IndexFile{{Name: name}})
			require.NoError(t, err)
			require.Len(t, kept, 1, "%s should keep %s while %s is joining", addr, name, joining.id)
		}
	}
	require.Greater(t, checked, 0, "the joining instance should take over at least one shard")
}

func containsString(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func TestMaximumFingerprintOverlapsLastShard(t *testing.T) {
	require.Equal(t, []index.ShardAnnotation{index.NewShard(3, 4)}, ShardsOverlapping(4, math.MaxUint64, math.MaxUint64))
}
