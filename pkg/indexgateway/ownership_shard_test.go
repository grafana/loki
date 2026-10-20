package indexgateway

import (
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

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
