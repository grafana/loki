package index

import (
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_BitPrefixGetShards(t *testing.T) {
	for _, tt := range []struct {
		total    uint32
		filter   bool
		shard    *logql.Shard
		expected []uint32
	}{
		// equal factors
		{16, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 16}).Ptr(), []uint32{0}},
		{16, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 4, Of: 16}).Ptr(), []uint32{4}},
		{16, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 15, Of: 16}).Ptr(), []uint32{15}},

		// idx factor a larger factor of 2
		{32, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 16}).Ptr(), []uint32{0, 1}},
		{32, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 4, Of: 16}).Ptr(), []uint32{8, 9}},
		{32, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 15, Of: 16}).Ptr(), []uint32{30, 31}},
		{64, false, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 15, Of: 16}).Ptr(), []uint32{60, 61, 62, 63}},

		// // idx factor a smaller factor of 2
		{8, true, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 16}).Ptr(), []uint32{0}},
		{8, true, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 4, Of: 16}).Ptr(), []uint32{2}},
		{8, true, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 15, Of: 16}).Ptr(), []uint32{7}},
	} {
		t.Run(tt.shard.String()+fmt.Sprintf("_total_%d", tt.total), func(t *testing.T) {
			ii, err := NewBitPrefixWithShards(tt.total)
			require.Nil(t, err)
			res, filter := ii.getShards(tt.shard)
			resInt := []uint32{}
			for _, r := range res {
				resInt = append(resInt, r.shard)
			}
			require.Equal(t, tt.filter, filter)
			require.Equal(t, tt.expected, resInt)
		})
	}
}

func Test_BitPrefixGetShards_Bounded(t *testing.T) {
	for _, tt := range []struct {
		total    uint32
		shard    *logql.Shard
		expected []uint32
	}{
		{
			4,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0b01 << 62,
						Max: 0b10 << 62,
					},
				},
			).Ptr(),
			[]uint32{1, 2},
		},
		{
			4,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0b10 << 62,
						Max: 0b11 << 62,
					},
				},
			).Ptr(),
			[]uint32{2, 3},
		},
		{
			8,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0b00 << 62,
						Max: 0b101 << 61,
					},
				},
			).Ptr(),
			[]uint32{0, 1, 2, 3, 4, 5},
		},
		{
			8,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0b00 << 62,
						Max: 0b110 << 61,
					},
				},
			).Ptr(),
			[]uint32{0, 1, 2, 3, 4, 5, 6},
		},
		{
			8,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0b00 << 62,
						Max: 0b111 << 61,
					},
				},
			).Ptr(),
			[]uint32{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			8,
			logql.NewBoundedShard(
				logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 0,
						Max: math.MaxUint64,
					},
				},
			).Ptr(),
			[]uint32{0, 1, 2, 3, 4, 5, 6, 7},
		},
	} {
		t.Run(tt.shard.String()+fmt.Sprintf("_total_%d", tt.total), func(t *testing.T) {
			ii, err := NewBitPrefixWithShards(tt.total)
			require.Nil(t, err)
			res, filter := ii.getShards(tt.shard)
			require.True(t, filter) // always need to filter bounded shards
			resInt := []uint32{}
			for _, r := range res {
				resInt = append(resInt, r.shard)
			}
			require.Equal(t, tt.expected, resInt)
		})
	}

}

func Test_BitPrefixValidateShards(t *testing.T) {
	ii, err := NewBitPrefixWithShards(32)
	require.Nil(t, err)
	require.NoError(t, ii.validateShard(logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 1, Of: 16}).Ptr()))
	require.Error(t, ii.validateShard(logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 1, Of: 15}).Ptr()))
}

func Test_BitPrefixCreation(t *testing.T) {
	// non factor of 2 shard factor
	_, err := NewBitPrefixWithShards(6)
	require.Error(t, err)

	// valid shard factor
	_, err = NewBitPrefixWithShards(4)
	require.Nil(t, err)
}

func Test_BitPrefixDeleteAddLoopkup(t *testing.T) {
	index, err := NewBitPrefixWithShards(DefaultIndexShards)
	require.Nil(t, err)
	lbs := []logproto.LabelAdapter{
		{Name: "foo", Value: "foo"},
		{Name: "bar", Value: "bar"},
		{Name: "buzz", Value: "buzz"},
	}
	sort.Sort(logproto.FromLabelAdaptersToLabels(lbs))

	index.Add(lbs, model.Fingerprint((logproto.FromLabelAdaptersToLabels(lbs).Hash())))
	index.Delete(logproto.FromLabelAdaptersToLabels(lbs), model.Fingerprint(logproto.FromLabelAdaptersToLabels(lbs).Hash()))
	ids, err := index.Lookup([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
	}, nil)
	require.NoError(t, err)
	require.Len(t, ids, 0)
}

func Test_BitPrefix_hash_mapping(t *testing.T) {
	lbs := labels.Labels{
		labels.Label{Name: "compose_project", Value: "loki-tsdb-storage-s3"},
		labels.Label{Name: "compose_service", Value: "ingester-2"},
		labels.Label{Name: "container_name", Value: "loki-tsdb-storage-s3_ingester-2_1"},
		labels.Label{Name: "filename", Value: "/var/log/docker/790fef4c6a587c3b386fe85c07e03f3a1613f4929ca3abaa4880e14caadb5ad1/json.log"},
		labels.Label{Name: "host", Value: "docker-desktop"},
		labels.Label{Name: "source", Value: "stderr"},
	}

	// for _, shard := range []uint32{2, 4, 8, 16, 32, 64, 128} {
	for _, shard := range []uint32{2} {
		t.Run(fmt.Sprintf("%d", shard), func(t *testing.T) {
			ii, err := NewBitPrefixWithShards(shard)
			require.Nil(t, err)

			requestedFactor := 16

			fp := model.Fingerprint(lbs.Hash())
			ii.Add(logproto.FromLabelsToLabelAdapters(lbs), fp)

			requiredBits := index.NewShard(0, uint32(requestedFactor)).RequiredBits()
			expShard := uint32(lbs.Hash() >> (64 - requiredBits))

			res, err := ii.Lookup(
				[]*labels.Matcher{{Type: labels.MatchEqual,
					Name:  "compose_project",
					Value: "loki-tsdb-storage-s3"}},
				logql.NewPowerOfTwoShard(index.ShardAnnotation{
					Shard: expShard,
					Of:    uint32(requestedFactor),
				}).Ptr(),
			)
			require.NoError(t, err)
			require.Len(t, res, 1)
			require.Equal(t, fp, res[0])
		})
	}
}

func Test_BitPrefixNoMatcherLookup(t *testing.T) {
	lbs := labels.Labels{
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "hi", Value: "hello"},
	}
	// with no shard param
	ii, err := NewBitPrefixWithShards(16)
	require.Nil(t, err)
	fp := model.Fingerprint(lbs.Hash())
	ii.Add(logproto.FromLabelsToLabelAdapters(lbs), fp)
	ids, err := ii.Lookup(nil, nil)
	require.Nil(t, err)
	require.Equal(t, fp, ids[0])

	// with shard param
	ii, err = NewBitPrefixWithShards(16)
	require.Nil(t, err)
	expShard := uint32(fp >> (64 - index.NewShard(0, 16).RequiredBits()))
	ii.Add(logproto.FromLabelsToLabelAdapters(lbs), fp)
	ids, err = ii.Lookup(nil, logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: expShard, Of: 16}).Ptr())
	require.Nil(t, err)
	require.Equal(t, fp, ids[0])
}

func Test_BitPrefixConsistentMapping(t *testing.T) {
	a, err := NewBitPrefixWithShards(16)
	require.Nil(t, err)
	b, err := NewBitPrefixWithShards(32)
	require.Nil(t, err)

	for i := 0; i < 100; i++ {
		lbs := labels.Labels{
			labels.Label{Name: "foo", Value: "bar"},
			labels.Label{Name: "hi", Value: fmt.Sprint(i)},
		}

		fp := model.Fingerprint(lbs.Hash())
		a.Add(logproto.FromLabelsToLabelAdapters(lbs), fp)
		b.Add(logproto.FromLabelsToLabelAdapters(lbs), fp)
	}

	shardMax := uint32(8)
	for i := uint32(0); i < shardMax; i++ {
		shard := logql.NewPowerOfTwoShard(index.ShardAnnotation{
			Shard: i,
			Of:    shardMax,
		}).Ptr()

		aIDs, err := a.Lookup([]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
		}, shard)
		require.Nil(t, err)

		bIDs, err := b.Lookup([]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
		}, shard)
		require.Nil(t, err)

		sorter := func(xs []model.Fingerprint) {
			sort.Slice(xs, func(i, j int) bool {
				return xs[i] < xs[j]
			})
		}
		sorter(aIDs)
		sorter(bIDs)

		require.Equal(t, aIDs, bIDs, "incorrect shard mapping for shard %v", shard)
	}

}
