package querier

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func powerOfTwo(shard, of uint32) *logql.Shard {
	return &logql.Shard{PowerOfTwo: &index.ShardAnnotation{Shard: shard, Of: of}}
}

func bounded(minFP, maxFP uint64) *logql.Shard {
	return &logql.Shard{Bounded: &logproto.Shard{Bounds: logproto.FPBounds{Min: model.Fingerprint(minFP), Max: model.Fingerprint(maxFP)}}}
}

func TestShardBucketRange(t *testing.T) {
	tests := []struct {
		name              string
		shard             *logql.Shard
		wantFrom, wantTo  uint64
		wantExact, wantOK bool
	}{
		{"nil is not sharded", nil, 0, 0, false, false},
		{"1-of-1 matches all", powerOfTwo(0, 1), 0, 0, false, false},

		// Of <= 32: exact, each shard is a contiguous block of 32/Of buckets.
		{"0 of 2", powerOfTwo(0, 2), 0, 15, true, true},
		{"1 of 2", powerOfTwo(1, 2), 16, 31, true, true},
		{"0 of 4", powerOfTwo(0, 4), 0, 7, true, true},
		{"3 of 4", powerOfTwo(3, 4), 24, 31, true, true},
		{"5 of 8", powerOfTwo(5, 8), 20, 23, true, true},
		{"0 of 32", powerOfTwo(0, 32), 0, 0, true, true},
		{"17 of 32", powerOfTwo(17, 32), 17, 17, true, true},
		{"31 of 32", powerOfTwo(31, 32), 31, 31, true, true},

		// Of > 32: over-fetch, a shard maps to the single bucket shard>>(k-5).
		{"0 of 64", powerOfTwo(0, 64), 0, 0, false, true},
		{"1 of 64", powerOfTwo(1, 64), 0, 0, false, true},
		{"2 of 64", powerOfTwo(2, 64), 1, 1, false, true},
		{"63 of 64", powerOfTwo(63, 64), 31, 31, false, true},
		{"3 of 128", powerOfTwo(3, 128), 0, 0, false, true},
		{"127 of 128", powerOfTwo(127, 128), 31, 31, false, true},

		// Bounded: over-fetch, the inclusive fingerprint range maps to its bucket range.
		{"bounded within one bucket", bounded(0, (1<<59)-1), 0, 0, false, true},
		{"bounded spanning buckets 0..1", bounded(0, 1<<59), 0, 1, false, true},
		{"bounded top bucket", bounded(uint64(31)<<59, ^uint64(0)), 31, 31, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sb, ok := shardBucketRange(tc.shard)
			require.Equal(t, tc.wantOK, ok, "ok")
			if !ok {
				return
			}
			require.Equal(t, tc.wantFrom, sb.from, "from")
			require.Equal(t, tc.wantTo, sb.to, "to")
			require.Equal(t, tc.wantExact, sb.exact, "exact")
		})
	}
}

// TestShardBucketRange_ExactMatchesFingerprintShard verifies that for an exact power-of-two
// shard, a fingerprint's membership in the bucket range equals its membership in the shard
// (i.e. the shard bucket predicate alone is a correct shard test).
func TestShardBucketRange_ExactMatchesFingerprintShard(t *testing.T) {
	// Cover every power-of-two shard count that is exact-eligible (2..streams.ShardFactor), and probe
	// every bucket value (the top streams.ShardBits fingerprint bits) — no shard-factor value is assumed.
	for of := uint32(2); of <= streams.ShardFactor; of *= 2 {
		for shard := uint32(0); shard < of; shard++ {
			s := powerOfTwo(shard, of)
			sb, ok := shardBucketRange(s)
			require.True(t, ok)
			require.Truef(t, sb.exact, "Of=%d must be exact", of)

			for bucket := uint64(0); bucket < streams.ShardFactor; bucket++ {
				fp := bucket << (64 - streams.ShardBits) // lowest fingerprint whose top bits select this bucket
				inBucket := bucket >= sb.from && bucket <= sb.to
				inShard := s.Match(model.Fingerprint(fp))
				require.Equalf(t, inShard, inBucket, "of=%d shard=%d bucket=%d: bucket range and shard disagree", of, shard, bucket)
			}
		}
	}
}

// TestShardBucketRange_OverFetchIsSuperset verifies that for a power-of-two shard larger than
// streams.ShardFactor, the bucket range is a superset of the shard: every fingerprint the shard matches
// falls inside the range, so no in-shard stream is ever pruned (the recheck removes the extras). An
// under-fetch here would silently under-count.
func TestShardBucketRange_OverFetchIsSuperset(t *testing.T) {
	const probeBits = 8 // >= log2(max Of below): every shard-distinguishing prefix is covered
	for _, of := range []uint32{2 * streams.ShardFactor, 4 * streams.ShardFactor, 8 * streams.ShardFactor} {
		for shard := uint32(0); shard < of; shard++ {
			s := powerOfTwo(shard, of)
			sb, ok := shardBucketRange(s)
			require.True(t, ok)
			require.Falsef(t, sb.exact, "Of=%d must over-fetch", of)

			for top := uint64(0); top < 1<<probeBits; top++ {
				fp := top << (64 - probeBits)
				if !s.Match(model.Fingerprint(fp)) {
					continue
				}
				bucket := fp >> (64 - streams.ShardBits)
				require.Truef(t, bucket >= sb.from && bucket <= sb.to,
					"of=%d shard=%d: in-shard fp bucket %d not in range [%d,%d]", of, shard, bucket, sb.from, sb.to)
			}
		}
	}
}
