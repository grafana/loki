package index

import (
	"fmt"
	"sort"
	"testing"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_GetShards(t *testing.T) {
	for _, tt := range []struct {
		total    uint32
		shard    *astmapper.ShardAnnotation
		expected []uint32
	}{
		// equal factors
		{16, &astmapper.ShardAnnotation{Shard: 0, Of: 16}, []uint32{0}},
		{16, &astmapper.ShardAnnotation{Shard: 4, Of: 16}, []uint32{4}},
		{16, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{15}},

		// idx factor a larger multiple of schema factor
		{32, &astmapper.ShardAnnotation{Shard: 0, Of: 16}, []uint32{0, 1}},
		{32, &astmapper.ShardAnnotation{Shard: 4, Of: 16}, []uint32{8, 9}},
		{32, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{30, 31}},
		{64, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{60, 61, 62, 63}},

		// schema factor is a larger multiple of idx factor
		{16, &astmapper.ShardAnnotation{Shard: 0, Of: 32}, []uint32{0}},
		{16, &astmapper.ShardAnnotation{Shard: 4, Of: 32}, []uint32{2}},
		{16, &astmapper.ShardAnnotation{Shard: 15, Of: 32}, []uint32{7}},

		// idx factor smaller but not a multiple of schema factor
		{4, &astmapper.ShardAnnotation{Shard: 0, Of: 5}, []uint32{0}},
		{4, &astmapper.ShardAnnotation{Shard: 1, Of: 5}, []uint32{0, 1}},
		{4, &astmapper.ShardAnnotation{Shard: 4, Of: 5}, []uint32{3}},

		// schema factor smaller but not a multiple of idx factor
		{8, &astmapper.ShardAnnotation{Shard: 0, Of: 5}, []uint32{0, 1}},
		{8, &astmapper.ShardAnnotation{Shard: 2, Of: 5}, []uint32{3, 4}},
		{8, &astmapper.ShardAnnotation{Shard: 3, Of: 5}, []uint32{4, 5, 6}},
		{8, &astmapper.ShardAnnotation{Shard: 4, Of: 5}, []uint32{6, 7}},
	} {
		tt := tt
		t.Run(tt.shard.String()+fmt.Sprintf("_total_%d", tt.total), func(t *testing.T) {
			ii := NewWithShards(tt.total)
			res := ii.getShards(tt.shard)
			resInt := []uint32{}
			for _, r := range res {
				resInt = append(resInt, r.shard)
			}
			require.Equal(t, tt.expected, resInt)
		})
	}
}

func Test_ValidateShards(t *testing.T) {
	require.NoError(t, validateShard(32, &astmapper.ShardAnnotation{Shard: 1, Of: 16}))
}

var (
	result uint32
	lbs    = []cortexpb.LabelAdapter{
		{Name: "foo", Value: "bar"},
	}
	buf = make([]byte, 0, 1024)
)

func BenchmarkHash(b *testing.B) {
	b.Run("sha256", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			result = labelsSeriesIDHash(cortexpb.FromLabelAdaptersToLabels(lbs)) % 16
		}
	})
	b.Run("xxash", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			var fp uint64
			fp, buf = cortexpb.FromLabelAdaptersToLabels(lbs).HashWithoutLabels(buf, []string(nil)...)
			result = util.HashFP(model.Fingerprint(fp)) % 16
		}
	})
}

func TestDeleteAddLoopkup(t *testing.T) {
	index := New()
	lbs := []cortexpb.LabelAdapter{
		{Name: "foo", Value: "foo"},
		{Name: "bar", Value: "bar"},
		{Name: "buzz", Value: "buzz"},
	}
	sort.Sort(cortexpb.FromLabelAdaptersToLabels(lbs))

	require.Equal(t, uint32(7), labelsSeriesIDHash(cortexpb.FromLabelAdaptersToLabels(lbs))%32)
	// make sure we consistent
	require.Equal(t, uint32(7), labelsSeriesIDHash(cortexpb.FromLabelAdaptersToLabels(lbs))%32)
	index.Add(lbs, model.Fingerprint((cortexpb.FromLabelAdaptersToLabels(lbs).Hash())))
	index.Delete(cortexpb.FromLabelAdaptersToLabels(lbs), model.Fingerprint(cortexpb.FromLabelAdaptersToLabels(lbs).Hash()))
	ids, err := index.Lookup([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
	}, nil)
	require.NoError(t, err)
	require.Len(t, ids, 0)
}
