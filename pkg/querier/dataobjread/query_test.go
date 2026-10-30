package dataobjread

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/logql"
	"github.com/grafana/loki/v4/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestShardBucketRangeFor(t *testing.T) {
	defaultLayout := streams.DefaultShardLayout()

	tests := map[string]struct {
		shard  *logql.Shard
		layout streams.ShardLayout

		wantOK    bool
		wantFrom  uint32
		wantTo    uint32
		wantExact bool
	}{
		"an unsharded query prunes nothing": {
			shard:  nil,
			layout: defaultLayout,
			wantOK: false,
		},
		"a shard of one prunes nothing because it matches every stream": {
			shard:  powerOfTwoShard(0, 1),
			layout: defaultLayout,
			wantOK: false,
		},
		"the first of two shards covers the lower half of the buckets exactly": {
			shard:     powerOfTwoShard(0, 2),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  0,
			wantTo:    defaultLayout.Buckets()/2 - 1,
			wantExact: true,
		},
		"the second of two shards covers the upper half of the buckets exactly": {
			shard:     powerOfTwoShard(1, 2),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  defaultLayout.Buckets() / 2,
			wantTo:    defaultLayout.Buckets() - 1,
			wantExact: true,
		},
		"a shard count equal to the bucket count maps each shard to one exact bucket": {
			shard:     powerOfTwoShard(3, defaultLayout.Buckets()),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  3,
			wantTo:    3,
			wantExact: true,
		},
		"a shard count above the bucket count is inexact because several shards share a bucket": {
			shard:     powerOfTwoShard(3, defaultLayout.Buckets()*2),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  1,
			wantTo:    1,
			wantExact: false,
		},
		"the last of three shards wraps to the whole fingerprint space because three is not a power of two, so nothing is pushed down": {
			shard:  powerOfTwoShard(2, 3),
			layout: defaultLayout,
			wantOK: false,
		},
		"the last of six shards wraps to a partial fingerprint range because six is not a power of two, so the range is inexact": {
			shard:     powerOfTwoShard(5, 6),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  8,
			wantTo:    defaultLayout.Buckets() - 1,
			wantExact: false,
		},
		"a bounded shard is never exact, so its stream hashes stay checked": {
			shard:     boundedShard(0, ^uint64(0)/2),
			layout:    defaultLayout,
			wantOK:    true,
			wantFrom:  0,
			wantTo:    defaultLayout.Buckets()/2 - 1,
			wantExact: false,
		},
		"a bounded shard of the whole fingerprint space prunes nothing": {
			shard:  boundedShard(0, ^uint64(0)),
			layout: defaultLayout,
			wantOK: false,
		},
		"a layout with more buckets maps the same shard to two exact buckets": {
			shard:     powerOfTwoShard(3, 64),
			layout:    streams.MustShardLayoutFromBuckets(128),
			wantOK:    true,
			wantFrom:  6,
			wantTo:    7,
			wantExact: true,
		},
		"a layout with fewer buckets maps the same shard to one inexact bucket": {
			shard:     powerOfTwoShard(3, 64),
			layout:    streams.MustShardLayoutFromBuckets(8),
			wantOK:    true,
			wantFrom:  0,
			wantTo:    0,
			wantExact: false,
		},
		"a layout of one bucket has nothing to prune": {
			shard:  powerOfTwoShard(1, 2),
			layout: streams.MustShardLayoutFromBuckets(1),
			wantOK: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			buckets, ok := shardBucketRangeFor(test.shard, test.layout)
			require.Equal(t, test.wantOK, ok)
			if !test.wantOK {
				return
			}
			require.Equal(t, test.wantFrom, buckets.from, "from")
			require.Equal(t, test.wantTo, buckets.to, "to")
			require.Equal(t, test.wantExact, buckets.exact, "exact")
		})
	}

	t.Run("an exact range holds exactly the streams of the shard, so the planner can skip the stream-hash check", func(t *testing.T) {
		var checked int
		for _, buckets := range []uint32{2, 8, 32, 128} {
			layout := streams.MustShardLayoutFromBuckets(buckets)
			for _, of := range []uint32{2, 4, 8, 32, 64, 256} {
				for shard := uint32(0); shard < of; shard++ {
					assignment := powerOfTwoShard(shard, of)
					bucketRange, ok := shardBucketRangeFor(assignment, layout)
					if !ok || !bucketRange.exact {
						continue
					}
					checked++

					for _, streamHash := range spreadStreamHashes(layout) {
						bucket := layout.BucketFromHash(streamHash)
						inRange := bucket >= bucketRange.from && bucket <= bucketRange.to
						require.Equal(t, inRange, assignment.Match(model.Fingerprint(streamHash)), "%d buckets, shard %d of %d: streamHash %d in bucket %d", buckets, shard, of, streamHash, bucket)
					}
				}
			}
		}
		require.Positive(t, checked, "no exact range was checked")
	})

	t.Run("a power-of-two shard keeps every matching streamHash inside its bucket range, so the pruned read drops no stream of the shard", func(t *testing.T) {
		var checked int
		for _, buckets := range []uint32{2, 8, 32, 128} {
			layout := streams.MustShardLayoutFromBuckets(buckets)
			for _, of := range []uint32{2, 3, 4, 6, 32, 64, 256} {
				for shard := uint32(0); shard < of; shard++ {
					assignment := powerOfTwoShard(shard, of)
					bucketRange, ok := shardBucketRangeFor(assignment, layout)
					if !ok {
						continue
					}

					for _, streamHash := range spreadStreamHashes(layout) {
						if !assignment.Match(model.Fingerprint(streamHash)) {
							continue
						}
						checked++
						bucket := layout.BucketFromHash(streamHash)
						require.GreaterOrEqual(t, bucket, bucketRange.from, "%d buckets, shard %d of %d: streamHash %d is in bucket %d, below the range", buckets, shard, of, streamHash, bucket)
						require.LessOrEqual(t, bucket, bucketRange.to, "%d buckets, shard %d of %d: streamHash %d is in bucket %d, above the range", buckets, shard, of, streamHash, bucket)
					}
				}
			}
		}
		require.Positive(t, checked, "no matching streamHash was checked")
	})

	t.Run("a bounded shard keeps every matching streamHash inside its bucket range, so the pruned read drops no stream of the shard", func(t *testing.T) {
		var checked int
		for _, buckets := range []uint32{2, 8, 32} {
			layout := streams.MustShardLayoutFromBuckets(buckets)
			hashes := spreadStreamHashes(layout)
			for _, min := range hashes {
				for _, max := range hashes {
					assignment := boundedShard(min, max)
					bucketRange, ok := shardBucketRangeFor(assignment, layout)
					if !ok {
						continue
					}

					for _, streamHash := range hashes {
						if !assignment.Match(model.Fingerprint(streamHash)) {
							continue
						}
						checked++
						bucket := layout.BucketFromHash(streamHash)
						require.GreaterOrEqual(t, bucket, bucketRange.from, "%d buckets, bounded shard [%d, %d]: streamHash %d is below the range", buckets, min, max, streamHash)
						require.LessOrEqual(t, bucket, bucketRange.to, "%d buckets, bounded shard [%d, %d]: streamHash %d is above the range", buckets, min, max, streamHash)
					}
				}
			}
		}
		require.Positive(t, checked, "no matching streamHash was checked")
	})
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
// every bucket boundary of layout, where an off-by-one in the mapping would show.
func spreadStreamHashes(layout streams.ShardLayout) []uint64 {
	out := []uint64{0, ^uint64(0)}
	if layout.Bits() == 0 {
		return out
	}

	bucketWidth := uint64(1) << (64 - layout.Bits())
	for bucket := range uint64(layout.Buckets()) {
		base := bucket * bucketWidth
		out = append(out, base, base+1, base+bucketWidth/2, base+bucketWidth-1)
	}
	return out
}
