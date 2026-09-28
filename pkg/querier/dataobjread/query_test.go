package dataobjread

import (
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestShardBucketRangeFor(t *testing.T) {
	tests := map[string]struct {
		shard *logql.Shard

		wantOK    bool
		wantFrom  uint32
		wantTo    uint32
		wantExact bool
	}{
		"an unsharded query prunes nothing": {
			shard:  nil,
			wantOK: false,
		},
		"a shard of one prunes nothing because it matches every stream": {
			shard:  powerOfTwoShard(0, 1),
			wantOK: false,
		},
		"the first of two shards covers the lower half of the buckets exactly": {
			shard:     powerOfTwoShard(0, 2),
			wantOK:    true,
			wantFrom:  0,
			wantTo:    streams.ShardFactor/2 - 1,
			wantExact: true,
		},
		"the second of two shards covers the upper half of the buckets exactly": {
			shard:     powerOfTwoShard(1, 2),
			wantOK:    true,
			wantFrom:  streams.ShardFactor / 2,
			wantTo:    streams.ShardFactor - 1,
			wantExact: true,
		},
		"a shard count equal to the bucket count maps each shard to one exact bucket": {
			shard:     powerOfTwoShard(3, streams.ShardFactor),
			wantOK:    true,
			wantFrom:  3,
			wantTo:    3,
			wantExact: true,
		},
		"a shard count above the bucket count is inexact because several shards share a bucket": {
			shard:     powerOfTwoShard(3, streams.ShardFactor*2),
			wantOK:    true,
			wantFrom:  1,
			wantTo:    1,
			wantExact: false,
		},
		"a shard count that is not a power of two is inexact because the annotation's arithmetic overflows": {
			shard:     powerOfTwoShard(2, 3),
			wantOK:    true,
			wantFrom:  0,
			wantTo:    streams.ShardFactor - 1,
			wantExact: false,
		},
		"a bounded shard is never exact, so its stream hashes stay checked": {
			shard:     boundedShard(0, ^uint64(0)/2),
			wantOK:    true,
			wantFrom:  0,
			wantTo:    streams.ShardFactor/2 - 1,
			wantExact: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			buckets, ok := shardBucketRangeFor(test.shard)
			require.Equal(t, test.wantOK, ok)
			if !test.wantOK {
				return
			}
			require.Equal(t, test.wantFrom, buckets.from, "from")
			require.Equal(t, test.wantTo, buckets.to, "to")
			require.Equal(t, test.wantExact, buckets.exact, "exact")
		})
	}
}

// TestShardBucketRangeFor_CoversEveryMatchingStream asserts the invariant the pruning relies on:
// every stream hash a shard matches falls inside the bucket range that shard maps to. A
// matching hash outside the range would be a stream the pruned read dropped without saying so.
func TestShardBucketRangeFor_CoversEveryMatchingStream(t *testing.T) {
	for _, of := range []uint32{2, 3, 4, 6, streams.ShardFactor, streams.ShardFactor * 2} {
		t.Run(fmt.Sprintf("a shard count of %d keeps every matching streamHash inside its bucket range", of), func(t *testing.T) {
			for shard := uint32(0); shard < of; shard++ {
				assignment := powerOfTwoShard(shard, of)
				buckets, ok := shardBucketRangeFor(assignment)
				require.True(t, ok)

				for _, streamHash := range spreadStreamHashes() {
					if !assignment.Match(model.Fingerprint(streamHash)) {
						continue
					}
					bucket := streams.ShardBucketFromHash(streamHash)
					require.GreaterOrEqual(t, bucket, buckets.from, "shard %d of %d: streamHash %d is in bucket %d, below the range", shard, of, streamHash, bucket)
					require.LessOrEqual(t, bucket, buckets.to, "shard %d of %d: streamHash %d is in bucket %d, above the range", shard, of, streamHash, bucket)
				}
			}
		})
	}
}

func powerOfTwoShard(shard, of uint32) *logql.Shard {
	return logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: shard, Of: of}).Ptr()
}

func boundedShard(from, through uint64) *logql.Shard {
	return logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{
		Min: model.Fingerprint(from),
		Max: model.Fingerprint(through),
	}}).Ptr()
}

// spreadStreamHashes returns stream hashes spread across the whole uint64 space, including
// every bucket boundary, where an off-by-one in the mapping would show.
func spreadStreamHashes() []uint64 {
	const bucketWidth = uint64(1) << (64 - streams.ShardBits)

	out := []uint64{0, ^uint64(0)}
	for bucket := uint64(0); bucket < streams.ShardFactor; bucket++ {
		base := bucket * bucketWidth
		out = append(out, base, base+1, base+bucketWidth/2, base+bucketWidth-1)
	}
	return out
}
