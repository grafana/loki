package querier

import (
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logql"
)

// shardBucketFilter is the inclusive [from,to] range of streams-section shard buckets a query shard can
// hold. exact reports whether that range is exactly the shard, so the per-stream fingerprint check can be
// skipped; when false the range over-fetches and the caller must keep the recheck.
type shardBucketFilter struct {
	from, to uint64
	exact    bool
}

// shardBucketRange maps a query shard to its shardBucketFilter. ok is false when the shard does not
// restrict streams (nil, or a shard of fewer than two), so no bucket predicate should be pushed.
//
// Buckets and power-of-two query shards both derive from the high bits of labels.StableHash, so a shard
// maps to a contiguous bucket range: for Of <= streams.ShardFactor the range is exactly the shard; for a
// larger Of, or a bounded shard whose boundaries are not bit-aligned, the range over-fetches.
func shardBucketRange(s *logql.Shard) (shardBucketFilter, bool) {
	if s == nil {
		return shardBucketFilter{}, false
	}

	var exact bool
	if s.Variant() == logql.PowerOfTwoVersion {
		if s.PowerOfTwo == nil || s.PowerOfTwo.Of < 2 {
			return shardBucketFilter{}, false // matches every stream; nothing to prune
		}
		exact = s.PowerOfTwo.Of <= streams.ShardFactor
	}

	// GetFromThrough returns a half-open fingerprint range [from, through); the last included fingerprint
	// is through-1, whose bucket is the inclusive upper bound.
	fpFrom, fpThrough := s.GetFromThrough()
	return shardBucketFilter{
		from:  uint64(fpFrom) >> (64 - streams.ShardBits),
		to:    uint64(fpThrough-1) >> (64 - streams.ShardBits),
		exact: exact,
	}, true
}
