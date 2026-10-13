package queryrange

import (
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	valid "github.com/grafana/loki/v3/pkg/validation"
)

func TestChunkShardingPolicy_PowerOfTwoShards(t *testing.T) {
	const bytes, maxBytesPerShard = 1 << 30, 1 << 20
	tenants := []string{"1"}
	limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard}}
	policy := newChunkShardingPolicy(limits)

	t.Run("returns the shard factor of the chunk logic when it sets no cap", func(t *testing.T) {
		require.Equal(t, 1024, policy.PowerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 0))
	})

	t.Run("returns the cap of the chunk logic when the factor is above it", func(t *testing.T) {
		require.Equal(t, 8, policy.PowerOfTwoShards(tenants, stats.Stats{Bytes: bytes}, 8))
	})

	t.Run("uses the smallest limit among the tenants", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard, "2": 4 * maxBytesPerShard}}

		got := newChunkShardingPolicy(limits).PowerOfTwoShards([]string{"2", "1"}, stats.Stats{Bytes: bytes}, 0)

		require.Equal(t, 1024, got)
	})

	t.Run("uses the limit below the default when another tenant has the default limit", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": maxBytesPerShard}}

		got := newChunkShardingPolicy(limits).PowerOfTwoShards([]string{"2", "1"}, stats.Stats{Bytes: bytes}, 0)

		require.Equal(t, 1024, got)
	})

	t.Run("uses the default limit when a tenant has a limit of 0", func(t *testing.T) {
		limits := bytesPerShardLimits{tsdbBytesPerShard: map[string]int{"1": 0, "2": maxBytesPerShard}}
		indexStats := stats.Stats{Bytes: 4 * valid.DefaultTSDBMaxBytesPerShard}

		got := newChunkShardingPolicy(limits).PowerOfTwoShards([]string{"1", "2"}, indexStats, 0)

		require.Equal(t, 4, got)
	})
}

func TestChunkShardingPolicy_BoundedShards(t *testing.T) {
	t.Run("returns the bounded shards and chunk refs it receives", func(t *testing.T) {
		shards, chunks := boundedShards(100, 10)

		gotShards, gotChunks := newChunkShardingPolicy(fakeLimits{}).BoundedShards(shards, chunks)

		require.Equal(t, shards, gotShards)
		require.Equal(t, chunks, gotChunks)
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

// bytesPerShardLimits gives each tenant its own limit on the bytes of a shard. A tenant that is
// not in the map has the limit of fakeLimits.
type bytesPerShardLimits struct {
	fakeLimits
	tsdbBytesPerShard map[string]int
}

func (l bytesPerShardLimits) TSDBMaxBytesPerShard(tenantID string) int {
	if limit, ok := l.tsdbBytesPerShard[tenantID]; ok {
		return limit
	}
	return l.fakeLimits.TSDBMaxBytesPerShard(tenantID)
}
