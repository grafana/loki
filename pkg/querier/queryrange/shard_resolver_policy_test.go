package queryrange

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	valid "github.com/grafana/loki/v3/pkg/validation"
)

func TestChunkShardingPolicy_powerOfTwoShards(t *testing.T) {
	const bytes, maxBytesPerShard = 1 << 30, 1 << 20
	tenants := []string{"1"}
	limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard}}
	policy := newChunkShardingPolicy(limits)

	t.Run("returns the shard factor of the chunk logic when it sets no cap", func(t *testing.T) {
		require.Equal(t, 1024, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 0))
	})

	t.Run("returns the cap of the chunk logic when the factor is above it", func(t *testing.T) {
		require.Equal(t, 8, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 8))
	})

	t.Run("uses the smallest limit among the tenants", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard, "2": 4 * maxBytesPerShard}}

		got := newChunkShardingPolicy(limits).powerOfTwoShards([]string{"2", "1"}, stats.Stats{Bytes: bytes}, 0)

		require.Equal(t, 1024, got)
	})

	t.Run("uses the limit below the default when another tenant has the default limit", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard}}

		got := newChunkShardingPolicy(limits).powerOfTwoShards([]string{"2", "1"}, stats.Stats{Bytes: bytes}, 0)

		require.Equal(t, 1024, got)
	})

	t.Run("uses the default limit when a tenant has a limit of 0", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": 0, "2": maxBytesPerShard}}
		indexStats := stats.Stats{Bytes: 4 * valid.DefaultTSDBMaxBytesPerShard}

		got := newChunkShardingPolicy(limits).powerOfTwoShards([]string{"1", "2"}, indexStats, 0)

		require.Equal(t, 4, got)
	})

	t.Run("ignores the limit of data objects", func(t *testing.T) {
		limits := bytesPerShardLimits{
			tsdbBytesPerShard:    map[string]int{"1": maxBytesPerShard},
			dataObjBytesPerShard: map[string]uint64{"1": 4 * maxBytesPerShard},
		}

		got := newChunkShardingPolicy(limits).powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 0)

		require.Equal(t, 1024, got)
	})
}

func TestChunkShardingPolicy_boundedShards(t *testing.T) {
	t.Run("returns the bounded shards and chunk refs it receives", func(t *testing.T) {
		shards, chunks := boundedShards(100, 10)

		gotShards, gotChunks := newChunkShardingPolicy(fakeLimits{}).boundedShards(shards, chunks)

		require.Equal(t, shards, gotShards)
		require.Equal(t, chunks, gotChunks)
	})
}

func TestDataObjShardingPolicy_shouldUse(t *testing.T) {
	tenants := []string{"1"}
	expr := syntax.MustParseExpr(streamFirstQuery)
	insideStart, insideEnd := dataObjRoutingNow.Add(-24*time.Hour), dataObjRoutingNow.Add(-12*time.Hour)

	newPolicy := func(cfg Config) *dataObjShardingPolicy {
		cfg.now = func() time.Time { return dataObjRoutingNow }
		return newDataObjShardingPolicy(cfg, dataObjTenantLimits("1"))
	}
	on := Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag}

	t.Run("returns false when the frontend config turns data objects off", func(t *testing.T) {
		require.False(t, newPolicy(Config{DataObjStorageLag: dataObjRoutingLag}).shouldUse(tenants, expr, insideStart, insideEnd))
	})

	t.Run("returns true for an expression that prefers data objects", func(t *testing.T) {
		require.True(t, newPolicy(on).shouldUse(tenants, expr, insideStart, insideEnd))
	})

	t.Run("returns false for an expression without the stream-first shape", func(t *testing.T) {
		rate := syntax.MustParseExpr(`rate({app="a"}[1m])`)

		require.False(t, newPolicy(on).shouldUse(tenants, rate, insideStart, insideEnd))
	})

	t.Run("returns false for a range outside the held range", func(t *testing.T) {
		require.False(t, newPolicy(on).shouldUse(tenants, expr, dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)))
	})

	t.Run("uses the storage lag of the frontend config", func(t *testing.T) {
		start, end := dataObjRoutingNow.Add(-150*time.Minute), dataObjRoutingNow.Add(-120*time.Minute)
		shortLag := Config{DataObjEnabled: true, DataObjStorageLag: time.Hour}

		require.False(t, newPolicy(on).shouldUse(tenants, expr, start, end))
		require.True(t, newPolicy(shortLag).shouldUse(tenants, expr, start, end))
	})
}

func TestDataObjShardingPolicy_powerOfTwoShards(t *testing.T) {
	const bytes, maxBytesPerShard = 1 << 30, 1 << 20
	tenants := []string{"1"}
	limits := bytesPerShardLimits{dataObjBytesPerShard: map[string]uint64{"1": maxBytesPerShard}}
	policy := newDataObjShardingPolicy(Config{}, limits)

	t.Run("caps the shard count at 8 times the number of shard buckets when the chunk logic sets no cap", func(t *testing.T) {
		require.Equal(t, 256, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 0))
	})

	t.Run("caps the shard count at 8 times the number of shard buckets when the chunk cap is higher", func(t *testing.T) {
		require.Equal(t, 256, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 512))
	})

	t.Run("keeps a chunk cap that is between the number of shard buckets and the data-object cap", func(t *testing.T) {
		require.Equal(t, 64, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 64))
	})

	t.Run("keeps a chunk cap that is lower than the number of shard buckets", func(t *testing.T) {
		require.Equal(t, 8, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 8))
	})

	t.Run("returns the same shard count as the chunk logic below the cap when the limits are equal", func(t *testing.T) {
		const smallBytes = 5 << 20
		limits := bytesPerShardLimits{
			tsdbBytesPerShard:    map[string]int{"1": maxBytesPerShard},
			dataObjBytesPerShard: map[string]uint64{"1": maxBytesPerShard},
		}
		chunkPolicy := newChunkShardingPolicy(limits)
		dataObjPolicy := newDataObjShardingPolicy(Config{}, limits)

		require.Equal(t, chunkPolicy.powerOfTwoShards(tenants, stats.Stats{Bytes: smallBytes}, 0), dataObjPolicy.powerOfTwoShards(tenants, stats.Stats{Bytes: smallBytes}, 0))
		require.Equal(t, 8, dataObjPolicy.powerOfTwoShards(tenants, stats.Stats{Bytes: smallBytes}, 0))
	})

	t.Run("does not shard a query that fits in one shard", func(t *testing.T) {
		require.Zero(t, policy.powerOfTwoShards(tenants, stats.Stats{Bytes: maxBytesPerShard}, 0))
	})

	t.Run("uses the limit of data objects and not the limit of chunks", func(t *testing.T) {
		const smallBytes = 5 << 20
		limits := bytesPerShardLimits{
			tsdbBytesPerShard:    map[string]int{"1": 1 << 10},
			dataObjBytesPerShard: map[string]uint64{"1": maxBytesPerShard},
		}

		got := newDataObjShardingPolicy(Config{}, limits).powerOfTwoShards(tenants, stats.Stats{Bytes: smallBytes}, 0)

		require.Equal(t, 8, got)
	})

	t.Run("uses the smallest limit among the tenants", func(t *testing.T) {
		const smallBytes = 5 << 20
		limits := bytesPerShardLimits{dataObjBytesPerShard: map[string]uint64{"1": maxBytesPerShard, "2": 4 * maxBytesPerShard}}

		got := newDataObjShardingPolicy(Config{}, limits).powerOfTwoShards([]string{"2", "1"}, stats.Stats{Bytes: smallBytes}, 0)

		require.Equal(t, 8, got)
	})

	t.Run("uses the default limit of data objects for a tenant without an override", func(t *testing.T) {
		indexStats := stats.Stats{Bytes: 4 * valid.DefaultDataObjMaxBytesPerShard}

		got := newDataObjShardingPolicy(Config{}, bytesPerShardLimits{}).powerOfTwoShards(tenants, indexStats, 0)

		require.Equal(t, 4, got)
	})
}

func TestDataObjShardingPolicy_boundedShards(t *testing.T) {
	policy := newDataObjShardingPolicy(Config{}, fakeLimits{})

	t.Run("returns 256 or fewer shards unchanged", func(t *testing.T) {
		shards, chunks := boundedShards(256, 10)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.Equal(t, shards, gotShards)
		require.Empty(t, gotChunks)
	})

	t.Run("returns no chunk refs for 256 or fewer shards", func(t *testing.T) {
		shards, chunks := boundedShards(256, 10)

		_, gotChunks := policy.boundedShards(shards, chunks)

		require.Empty(t, gotChunks)
	})

	t.Run("merges more than 256 shards into at most 256 shards", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.LessOrEqual(t, len(gotShards), 256)
		require.Empty(t, gotChunks)
	})

	t.Run("returns no chunk refs for more than 256 shards", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)

		_, gotChunks := policy.boundedShards(shards, chunks)

		require.Empty(t, gotChunks)
	})

	t.Run("keeps the bounds contiguous and covering every fingerprint", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.Equal(t, model.Fingerprint(0), gotShards[0].Bounds.Min)
		require.Equal(t, model.Fingerprint(math.MaxUint64), gotShards[len(gotShards)-1].Bounds.Max)
		for i := 1; i < len(gotShards); i++ {
			require.Equal(t, gotShards[i-1].Bounds.Max+1, gotShards[i].Bounds.Min, "gap before shard %d", i)
		}
		require.Empty(t, gotChunks)
	})

	t.Run("sums the stats of the merged shards", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		var got stats.Stats
		for _, shard := range gotShards {
			got.Streams += shard.Stats.Streams
			got.Chunks += shard.Stats.Chunks
			got.Bytes += shard.Stats.Bytes
			got.Entries += shard.Stats.Entries
		}
		require.Equal(t, stats.Stats{Streams: 1000, Chunks: 2000, Bytes: 10000, Entries: 3000}, got)
		require.Empty(t, gotChunks)
	})

	t.Run("merges more than 256 shards when the index returns no chunk refs", func(t *testing.T) {
		shards, _ := boundedShards(1000, 10)

		gotShards, gotChunks := policy.boundedShards(shards, nil)

		require.LessOrEqual(t, len(gotShards), 256)
		require.Empty(t, gotChunks)
	})

	t.Run("puts the empty shards after 256 full shards in the 256th group instead of a 257th", func(t *testing.T) {
		shards, chunks := boundedShards(264, 0)
		for i := 0; i < 256; i++ {
			shards[i].Stats.Bytes = 1000
		}

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.Len(t, gotShards, 256)
		require.Equal(t, uint64(1+8), gotShards[255].Stats.Streams)
		require.Empty(t, gotChunks)
	})

	t.Run("closes a group once it holds its share of the bytes", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.Len(t, gotShards, 250)
		for i, shard := range gotShards {
			require.Equal(t, uint64(4), shard.Stats.Streams, "group %d", i)
		}
		require.Empty(t, gotChunks)
	})

	t.Run("rounds the bytes of a group up when the total does not divide evenly", func(t *testing.T) {
		shards, chunks := boundedShards(257, 1)

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.Len(t, gotShards, 129)
		require.Equal(t, uint64(2), gotShards[0].Stats.Streams)
		require.Equal(t, uint64(1), gotShards[128].Stats.Streams)
		require.Empty(t, gotChunks)
	})

	t.Run("merges shards that have no stats", func(t *testing.T) {
		shards, chunks := boundedShards(1000, 10)
		for i := range shards {
			shards[i].Stats = nil
		}

		gotShards, gotChunks := policy.boundedShards(shards, chunks)

		require.LessOrEqual(t, len(gotShards), 256)
		require.Equal(t, model.Fingerprint(math.MaxUint64), gotShards[len(gotShards)-1].Bounds.Max)
		require.Empty(t, gotChunks)
	})
}

// boundedShards returns n adjacent shards that cover every fingerprint. Each shard holds the given
// bytes, and one reference to a chunk that carries the number of its shard.
func boundedShards(n int, bytes uint64) ([]logproto.Shard, []logproto.ChunkRefGroup) {
	step := math.MaxUint64 / uint64(n)
	shards := make([]logproto.Shard, n)
	chunks := make([]logproto.ChunkRefGroup, n)
	for i := range shards {
		minFP := uint64(i) * step
		maxFP := minFP + step - 1
		if i == n-1 {
			maxFP = math.MaxUint64
		}
		shards[i] = logproto.Shard{
			Bounds: logproto.FPBounds{Min: model.Fingerprint(minFP), Max: model.Fingerprint(maxFP)},
			Stats:  &stats.Stats{Streams: 1, Chunks: 2, Bytes: bytes, Entries: 3},
		}
		chunks[i] = logproto.ChunkRefGroup{Refs: []*logproto.ChunkRef{{Fingerprint: uint64(i)}}}
	}
	return shards, chunks
}

// bytesPerShardLimits gives each tenant its own limits on the bytes of a shard, for chunks and for
// data objects. A tenant that is not in a map has the limit of fakeLimits.
type bytesPerShardLimits struct {
	fakeLimits
	tsdbBytesPerShard    map[string]int
	dataObjBytesPerShard map[string]uint64
}

func (l bytesPerShardLimits) TSDBMaxBytesPerShard(tenantID string) int {
	if limit, ok := l.tsdbBytesPerShard[tenantID]; ok {
		return limit
	}
	return l.fakeLimits.TSDBMaxBytesPerShard(tenantID)
}

func (l bytesPerShardLimits) DataObjMaxBytesPerShard(tenantID string) uint64 {
	if limit, ok := l.dataObjBytesPerShard[tenantID]; ok {
		return limit
	}
	return l.fakeLimits.DataObjMaxBytesPerShard(tenantID)
}
