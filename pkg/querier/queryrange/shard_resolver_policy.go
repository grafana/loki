package queryrange

import (
	"time"

	dataobjstreams "github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
	// A policy can read the limits of tenantIDs to size the result.
	boundedShards(tenantIDs []string, shards []logproto.Shard, chunks []logproto.ChunkRefGroup) ([]logproto.Shard, []logproto.ChunkRefGroup)
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

func (chunkShardingPolicy) boundedShards(_ []string, shards []logproto.Shard, chunks []logproto.ChunkRefGroup) ([]logproto.Shard, []logproto.ChunkRefGroup) {
	return shards, chunks
}

// dataObjMaxOverReadFactor is the target for the largest ratio of shards to shard buckets in a
// query that reads data objects.
//
// When a query has more shards than a data object has buckets, several shards read the same
// bucket. The query then over-reads data by up to that ratio. We accept the over-read, to get more
// shards and run the query faster.
const dataObjMaxOverReadFactor = 8

// dataObjMaxShards is the most shards that a query that reads data objects can have, whatever the
// shard layout of its tenants.
const dataObjMaxShards = dataObjMaxOverReadFactor * dataobjstreams.DefaultShardBuckets

// dataObjShardingPolicy is the policy of queries that read data objects.
type dataObjShardingPolicy struct {
	cfg    Config
	limits Limits
}

func newDataObjShardingPolicy(cfg Config, limits Limits) *dataObjShardingPolicy {
	return &dataObjShardingPolicy{cfg: cfg, limits: limits}
}

// shouldUse reports whether the queries that run expr should use this policy. They should when they
// are eligible and read mostly from data objects, over the request range from start to end.
func (p *dataObjShardingPolicy) shouldUse(tenantIDs []string, expr syntax.Expr, start, end time.Time) bool {
	return dataObjResolve(p.cfg, p.limits, tenantIDs, expr, start, end).prefers()
}

// maxShards returns the largest number of shards for a query of the given tenants.
func (p *dataObjShardingPolicy) maxShards(tenantIDs []string) int {
	var buckets uint32
	for _, tenantID := range tenantIDs {
		// The tenant with the largest shard buckets count wins, because a smaller one would cap the shards
		// of the tenant with the most buckets. This means that a tenant with fewer buckets over-reads by more
		// than dataObjMaxOverReadFactor in the same query.
		//
		// The result is an estimate. The setting of a tenant is not always the layout of the data in the
		// range of the query. The querier reads each object correctly whatever its layout.
		buckets = max(buckets, p.limits.DataObjShardLayout(tenantID).Buckets())
	}
	return min(dataObjMaxOverReadFactor*int(buckets), dataObjMaxShards)
}

func (p *dataObjShardingPolicy) powerOfTwoShards(tenantIDs []string, indexStats stats.Stats, maxShards int) int {
	maxShardsCap := p.maxShards(tenantIDs)
	if maxShards <= 0 || maxShards > maxShardsCap {
		maxShards = maxShardsCap
	}

	maxBytesPerShard := validation.SmallestPositiveIntPerTenant(tenantIDs, p.limits.DataObjMaxBytesPerShard)
	return sharding.GuessShardFactor(indexStats.Bytes, maxBytesPerShard, maxShards)
}

// boundedShards merges the shards that the index returns into at most dataObjMaxOverReadFactor
// times the largest bucket count of the tenants, and never more than dataObjMaxShards. It returns
// no chunk refs.
func (p *dataObjShardingPolicy) boundedShards(tenantIDs []string, shards []logproto.Shard, _ []logproto.ChunkRefGroup) ([]logproto.Shard, []logproto.ChunkRefGroup) {
	maxShards := p.maxShards(tenantIDs)

	// The query reads most of its range from data objects, which have no chunks. The chunk refs of
	// the index would only add weight to each request. The querier resolves the chunks of the part
	// that it reads from the chunk store itself.
	if len(shards) <= maxShards {
		return shards, nil
	}

	var total uint64
	for _, shard := range shards {
		total += shardBytes(shard)
	}
	// The bytes of one group, rounded up. A value rounded down closes groups too early: it is 0
	// when total is below maxShards, and then every shard closes a group on its own.
	targetBytes := (total + uint64(maxShards) - 1) / uint64(maxShards)

	merged := make([]logproto.Shard, 0, maxShards)
	closeGroup := func(from, to int) {
		merged = append(merged, mergeShards(shards[from:to]))
	}

	// Walk the shards in order and close a group once it holds its share of the total bytes. The
	// last group never closes, so it takes the rest and the result never exceeds maxShards.
	from, groupBytes := 0, uint64(0)
	for i, shard := range shards {
		groupBytes += shardBytes(shard)
		isLastGroup := len(merged) == maxShards-1
		if !isLastGroup && groupBytes >= targetBytes {
			closeGroup(from, i+1)
			from, groupBytes = i+1, 0
		}
	}
	if from < len(shards) {
		closeGroup(from, len(shards))
	}

	return merged, nil
}

func shardBytes(shard logproto.Shard) uint64 {
	if shard.Stats == nil {
		return 0
	}
	return shard.Stats.Bytes
}

// mergeShards returns one shard that covers the bounds of the given adjacent shards, and holds
// the sum of their stats.
func mergeShards(shards []logproto.Shard) logproto.Shard {
	merged := logproto.Shard{
		Bounds: logproto.FPBounds{Min: shards[0].Bounds.Min, Max: shards[len(shards)-1].Bounds.Max},
		Stats:  &stats.Stats{},
	}
	for _, shard := range shards {
		if shard.Stats == nil {
			continue
		}
		merged.Stats.Streams += shard.Stats.Streams
		merged.Stats.Chunks += shard.Stats.Chunks
		merged.Stats.Bytes += shard.Stats.Bytes
		merged.Stats.Entries += shard.Stats.Entries
	}
	return merged
}
