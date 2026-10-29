package dataobjread

import (
	"math"
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

// QueryShard is the shard assignment of a query. It maps the assignment to the shard buckets of
// one shard layout at a time.
type QueryShard struct {
	assignment *logql.Shard
}

// NewQueryShard wraps a shard assignment. It returns nil for a nil assignment, so an unsharded
// query carries no shard at all.
func NewQueryShard(assignment *logql.Shard) *QueryShard {
	if assignment == nil {
		return nil
	}
	return &QueryShard{assignment: assignment}
}

// hashRange returns the hash range to push into metastore sections resolution. It is nil for a nil
// shard, and for a shard that has no range to push down.
func (q *QueryShard) hashRange() *streams.HashRange {
	if q == nil {
		return nil
	}
	hashes, ok := shardHashRangeFor(q.assignment)
	if !ok {
		return nil
	}
	return &hashes
}

// bucketRange returns the bucket range to push into a streams read of an object that has the given
// layout. It is nil in three cases: a nil shard, a shard with no range to push down, and a range
// that covers every bucket of the layout. The last range prunes nothing.
func (q *QueryShard) bucketRange(layout streams.ShardLayout) *shardBucketRange {
	if q == nil {
		return nil
	}

	// Derive the range for each call and never store it. A tenant can change its shard layout, so
	// one query reads objects of different layouts. A range from another layout hides streams of
	// the shard or admits streams of another shard. The planner skips the stream-hash check for an
	// exact range, so the second case over-counts.
	buckets, ok := shardBucketRangeFor(q.assignment, layout)
	if !ok {
		return nil
	}
	return &buckets
}

// shardBucketRange is the inclusive range of streams-section shard buckets a query shard can hold
// in the objects of one shard layout.
//
// from and to are buckets of that layout. The fields are uint32 because that is the width
// [streams.ShardLayout.BucketFromHash] produces, which also keeps the widening to the predicate's
// int64 column value lossless.
//
// exact reports whether the range is the shard and nothing more. When it is false the range
// covers streams outside the shard, so the caller must keep the per-stream stream-hash check.
type shardBucketRange struct {
	from, to uint32
	exact    bool
}

// isExact reports whether the range is the shard and nothing more. A nil range is not, so the
// caller keeps the stream-hash check.
func (b *shardBucketRange) isExact() bool {
	return b != nil && b.exact
}

// shardHashRangeFor maps a query shard to the stream hashes it can hold. ok is false when the
// shard can hold every hash, so nothing should be pushed down. That is the case for a
// power-of-two shard count below two and for a bounded shard of the whole fingerprint space.
//
// ok is also false for a power-of-two shard with no annotation. That shard is malformed, and
// GetFromThrough would dereference a nil pointer.
func shardHashRangeFor(shard *logql.Shard) (hashes streams.HashRange, ok bool) {
	if shard == nil {
		return streams.HashRange{}, false
	}

	var from, to uint64
	if shard.Variant() == logql.BoundedVersion {
		from, to = uint64(shard.Bounded.Bounds.Min), uint64(shard.Bounded.Bounds.Max)
	} else {
		annotation := shard.PowerOfTwo
		if annotation == nil || annotation.Of < 2 {
			// The shard covers every hash.
			return streams.HashRange{}, false
		}

		// through is the exclusive end of the fingerprint range, except for the last shard, where
		// it saturates at MaxUint64 and is inclusive.
		fromFingerprint, throughFingerprint := annotation.GetFromThrough()
		from, to = uint64(fromFingerprint), uint64(throughFingerprint)-1
		if annotation.Shard+1 == annotation.Of {
			to = math.MaxUint64
		}
	}

	if from == 0 && to == math.MaxUint64 {
		// The shard covers every hash.
		return streams.HashRange{}, false
	}
	return streams.HashRange{From: from, To: to}, true
}

// shardBucketRangeFor maps a query shard to the shard buckets of layout that it can hold. ok is
// false when the shard bucket range cannot be determined, or when it covers every shard bucket of
// the layout.
func shardBucketRangeFor(shard *logql.Shard, layout streams.ShardLayout) (buckets shardBucketRange, ok bool) {
	hashes, ok := shardHashRangeFor(shard)
	if !ok {
		return shardBucketRange{}, false
	}

	// Shard buckets and power-of-two query shards both come from the high bits of labels.StableHash.
	// A shard therefore maps to a contiguous bucket range. The range is the shard exactly when the
	// shard count is a power of two and at most the bucket count of the layout. A larger count makes
	// several shards share a bucket, and a bounded shard is never exact, so both keep the stream-hash
	// check.
	var exact bool
	if shard.Variant() == logql.PowerOfTwoVersion {
		// A shard count that is not a power of two makes the annotation's own arithmetic
		// overflow, so its last shard reports the whole fingerprint space while matching
		// nothing. Calling that exact would skip the stream-hash check and hand every stream to
		// every shard. Nothing on this path validates the count, so check it here.
		powerOfTwo := shard.PowerOfTwo.Of&(shard.PowerOfTwo.Of-1) == 0
		exact = powerOfTwo && shard.PowerOfTwo.Of <= layout.Buckets()
	}

	from, to := layout.BucketRange(hashes)
	if layout.CoversAllShardBuckets(from, to) {
		return shardBucketRange{}, false
	}
	return shardBucketRange{from: from, to: to, exact: exact}, true
}
