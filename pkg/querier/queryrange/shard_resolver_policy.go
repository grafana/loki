package queryrange

import (
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// shardingPolicy holds the parts of dynamic shard resolution that depend on where a query reads
// its data from.
type shardingPolicy interface {
	// powerOfTwoShards returns the number of shards of the power-of-two strategy for a query
	// with the given index stats. The policy takes the byte limit of one shard from the limits of
	// the tenants. A positive maxShards caps the result. A policy can also apply a cap of its
	// own. A result of 0 means no sharding.
	powerOfTwoShards(tenantIDs []string, indexStats stats.Stats, maxShards int) int

	// boundedShards reshapes the shards that the index returns for the bounded strategy.
	// chunks is empty, or holds one group of chunk refs for each shard, in shard order.
	// The result has the same form: no chunk refs, or one group for each shard it returns.
	boundedShards(shards []logproto.Shard, chunks []logproto.ChunkRefGroup) ([]logproto.Shard, []logproto.ChunkRefGroup)
}

var _ shardingPolicy = chunkShardingPolicy{}

// chunkShardingPolicy is the policy of queries that read chunks.
type chunkShardingPolicy struct {
	limits Limits
}

func newChunkShardingPolicy(limits Limits) chunkShardingPolicy {
	return chunkShardingPolicy{limits: limits}
}

func (p chunkShardingPolicy) powerOfTwoShards(tenantIDs []string, indexStats stats.Stats, maxShards int) int {
	maxBytesPerShard := validation.SmallestPositiveIntPerTenant(tenantIDs, p.limits.TSDBMaxBytesPerShard)
	return sharding.GuessShardFactor(indexStats.Bytes, uint64(maxBytesPerShard), maxShards)
}

func (chunkShardingPolicy) boundedShards(shards []logproto.Shard, chunks []logproto.ChunkRefGroup) ([]logproto.Shard, []logproto.ChunkRefGroup) {
	return shards, chunks
}
