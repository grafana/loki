package dataobjread

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logql"
)

// QueryParams is the query-level input every section read in one plan shares.
type QueryParams struct {
	Start, End time.Time
	Matchers   []*labels.Matcher
	Shard      *QueryShard
	Projection ProjectionPlan
}

// QueryShard is a query's shard assignment together with the shard buckets that shard can
// hold.
//
// [NewQueryShard] derives the two together, so the buckets can never belong to a different
// shard. That matters because the planner trusts an exact range enough to skip the per-stream
// stream-hash check: a mismatched pair would admit another shard's streams and over-count.
type QueryShard struct {
	assignment *logql.Shard

	// buckets is the bucket range the assignment maps to, or nil when the shard restricts no
	// stream.
	buckets *shardBucketRange
}

// NewQueryShard pairs a shard assignment with its bucket range. It returns nil for a nil
// assignment, so an unsharded query carries no shard at all.
func NewQueryShard(assignment *logql.Shard) *QueryShard {
	if assignment == nil {
		return nil
	}
	shard := &QueryShard{assignment: assignment}
	if buckets, ok := shardBucketRangeFor(assignment); ok {
		shard.buckets = &buckets
	}
	return shard
}

// bucketRange returns the bucket range to push into a streams read. It is nil for a nil shard,
// and for a shard that restricts no stream.
func (q *QueryShard) bucketRange() *shardBucketRange {
	if q == nil {
		return nil
	}
	return q.buckets
}

// prunes reports whether the shard narrows a streams read to a bucket range.
func (q *QueryShard) prunes() bool {
	return q.bucketRange() != nil
}

// resolvesExactly reports whether the bucket range is the shard and nothing more, so the
// per-stream stream-hash check would drop no further stream.
func (q *QueryShard) resolvesExactly() bool {
	bucketRange := q.bucketRange()
	return bucketRange != nil && bucketRange.exact
}

// shardBucketRange is the inclusive range of streams-section shard buckets a query shard can
// hold.
//
// from and to are valid shard buckets, in [0, streams.ShardFactor). The fields are uint32
// because that is the width [streams.ShardBucketFromHash] produces, which also keeps the
// widening to the predicate's int64 column value lossless.
//
// exact reports whether the range is the shard and nothing more. When it is false the range
// covers streams outside the shard, so the caller must keep the per-stream stream-hash check.
type shardBucketRange struct {
	from, to uint32
	exact    bool
}

// shardBucketRangeFor maps a query shard to the shard buckets it can hold. ok is false when
// nothing should be pushed down: a power-of-two shard of fewer than two restricts no stream,
// and one with no annotation is malformed, which would make GetFromThrough dereference a nil
// pointer.
//
// Shard buckets and power-of-two query shards both come from the high bits of
// labels.StableHash, so a shard maps to a contiguous bucket range. The range is the shard
// exactly when the shard count is at most [streams.ShardFactor]. A larger count makes several
// shards share a bucket, and a bounded shard is never exact, so both keep the stream-hash
// check.
func shardBucketRangeFor(shard *logql.Shard) (shardBucketRange, bool) {
	if shard == nil {
		return shardBucketRange{}, false
	}

	var exact bool
	if shard.Variant() == logql.PowerOfTwoVersion {
		if shard.PowerOfTwo == nil || shard.PowerOfTwo.Of < 2 {
			return shardBucketRange{}, false
		}
		// A shard count that is not a power of two makes the annotation's own arithmetic
		// overflow, so its last shard reports the whole fingerprint space while matching
		// nothing. Calling that exact would skip the stream-hash check and hand every stream to
		// every shard. Nothing on this path validates the count, so check it here.
		powerOfTwo := shard.PowerOfTwo.Of&(shard.PowerOfTwo.Of-1) == 0
		exact = powerOfTwo && shard.PowerOfTwo.Of <= streams.ShardFactor
	}

	// through is the exclusive end of the fingerprint range, except for the last shard of
	// either variant, where it saturates at MaxUint64 and is inclusive. Subtracting one is
	// still right there: only the top streams.ShardBits bits pick the bucket, so MaxUint64-1
	// and MaxUint64 share one.
	from, through := shard.GetFromThrough()
	return shardBucketRange{
		from:  streams.ShardBucketFromHash(uint64(from)),
		to:    streams.ShardBucketFromHash(uint64(through - 1)),
		exact: exact,
	}, true
}
