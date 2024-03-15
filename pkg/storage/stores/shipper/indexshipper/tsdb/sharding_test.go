package tsdb

import (
	"math"
	"sort"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestSizedFPs_Sort(t *testing.T) {
	xs := sizedFPs{
		{fp: 3},
		{fp: 1},
		{fp: 6},
		{fp: 10},
		{fp: 2},
		{fp: 0},
		{fp: 4},
		{fp: 5},
		{fp: 7},
		{fp: 9},
		{fp: 8},
	}

	sort.Sort(xs)
	exp := []model.Fingerprint{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	for i, x := range xs {
		require.Equal(t, exp[i], x.fp)
	}
}

func TestSizedFPs_ShardsFor(t *testing.T) {
	mkShard := func(min, max model.Fingerprint, streams, chks, entries, bytes uint64) logproto.Shard {
		return logproto.Shard{
			Bounds: logproto.FPBounds{
				Min: min,
				Max: max,
			},
			Stats: &stats.Stats{
				Streams: streams,
				Chunks:  chks,
				Entries: entries,
				Bytes:   bytes,
			},
		}
	}

	mkFP := func(fp model.Fingerprint, chks, entries, bytes uint64) sizedFP {
		return sizedFP{
			fp: fp,
			stats: stats.Stats{
				Chunks:  chks,
				Entries: entries,
				Bytes:   bytes,
			},
		}
	}

	for _, tc := range []struct {
		desc             string
		xs               sizedFPs
		exp              []logproto.Shard
		targetShardBytes uint64
	}{
		{
			desc:             "empty",
			targetShardBytes: 100,
			xs:               sizedFPs{},
			exp: []logproto.Shard{
				mkShard(0, math.MaxUint64, 0, 0, 0, 0),
			},
		},
		{
			desc:             "single stream",
			targetShardBytes: 100,
			xs: sizedFPs{
				mkFP(1, 1, 1, 1),
			},
			exp: []logproto.Shard{
				mkShard(0, math.MaxUint64, 1, 1, 1, 1),
			},
		},
		{
			desc:             "single stream too large",
			targetShardBytes: 100,
			xs: sizedFPs{
				mkFP(1, 1, 1, 201),
			},
			exp: []logproto.Shard{
				mkShard(0, math.MaxUint64, 1, 1, 1, 201),
			},
		},
		{
			desc:             "4 streams 2 shards",
			targetShardBytes: 100,
			xs: sizedFPs{
				// each has 45 bytes; can only fit 2 in a shard
				mkFP(1, 1, 1, 45),
				mkFP(2, 1, 1, 45),
				mkFP(3, 1, 1, 45),
				mkFP(4, 1, 1, 45),
			},
			exp: []logproto.Shard{
				mkShard(0, 2, 2, 2, 2, 90),
				mkShard(3, math.MaxUint64, 2, 2, 2, 90),
			},
		},
		{
			desc:             "5 streams 3 shards (one leftover)",
			targetShardBytes: 100,
			xs: sizedFPs{
				// each has 45 bytes; can only fit 2 in a shard
				mkFP(1, 1, 1, 45),
				mkFP(2, 1, 1, 45),
				mkFP(3, 1, 1, 45),
				mkFP(4, 1, 1, 45),
				mkFP(5, 1, 1, 45),
			},
			exp: []logproto.Shard{
				mkShard(0, 2, 2, 2, 2, 90),
				mkShard(3, 4, 2, 2, 2, 90),
				mkShard(5, math.MaxUint64, 1, 1, 1, 45),
			},
		},
		{
			desc:             "allowed overflow",
			targetShardBytes: 100,
			xs: sizedFPs{
				// each has 40 bytes; can fit 3 in a shard
				// since overflow == underflow
				mkFP(1, 1, 1, 40),
				mkFP(2, 1, 1, 40),
				mkFP(3, 1, 1, 40),
				mkFP(4, 1, 1, 40),
				mkFP(5, 1, 1, 40),
			},
			exp: []logproto.Shard{
				mkShard(0, 3, 3, 3, 3, 120),
				mkShard(4, math.MaxUint64, 2, 2, 2, 80),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.xs.ShardsFor(tc.targetShardBytes))
		})
	}
}
