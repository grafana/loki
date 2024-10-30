package queryrangebase

import (
	"math"
	"sort"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/astmapper"
)

func TestGenLabelsCorrectness(t *testing.T) {
	ls := genLabels([]string{"a", "b"}, 2)
	for _, set := range ls {
		sort.Sort(set)
	}
	expected := []labels.Labels{
		{
			labels.Label{
				Name:  "a",
				Value: "0",
			},
			labels.Label{
				Name:  "b",
				Value: "0",
			},
		},
		{
			labels.Label{
				Name:  "a",
				Value: "0",
			},
			labels.Label{
				Name:  "b",
				Value: "1",
			},
		},
		{
			labels.Label{
				Name:  "a",
				Value: "1",
			},
			labels.Label{
				Name:  "b",
				Value: "0",
			},
		},
		{
			labels.Label{
				Name:  "a",
				Value: "1",
			},
			labels.Label{
				Name:  "b",
				Value: "1",
			},
		},
	}
	require.Equal(t, expected, ls)
}

func TestGenLabelsSize(t *testing.T) {
	for _, tc := range []struct {
		set     []string
		buckets int
	}{
		{
			set:     []string{"a", "b"},
			buckets: 5,
		},
		{
			set:     []string{"a", "b", "c"},
			buckets: 10,
		},
	} {
		sets := genLabels(tc.set, tc.buckets)
		require.Equal(
			t,
			math.Pow(float64(tc.buckets), float64(len(tc.set))),
			float64(len(sets)),
		)
	}
}

func TestNewMockShardedqueryable(t *testing.T) {
	for _, tc := range []struct {
		shards, nSamples, labelBuckets int
		labelSet                       []string
	}{
		{
			nSamples:     100,
			shards:       1,
			labelBuckets: 3,
			labelSet:     []string{"a", "b", "c"},
		},
		{
			nSamples:     0,
			shards:       2,
			labelBuckets: 3,
			labelSet:     []string{"a", "b", "c"},
		},
	} {
		q := NewMockShardedQueryable(tc.nSamples, tc.labelSet, tc.labelBuckets, 0)
		expectedSeries := int(math.Pow(float64(tc.labelBuckets), float64(len(tc.labelSet))))

		seriesCt := 0
		var iter chunkenc.Iterator
		for i := 0; i < tc.shards; i++ {

			set := q.Select(ctx, false, nil, &labels.Matcher{
				Type: labels.MatchEqual,
				Name: astmapper.ShardLabel,
				Value: astmapper.ShardAnnotation{
					Shard: i,
					Of:    tc.shards,
				}.String(),
			})

			require.Nil(t, set.Err())

			for set.Next() {
				seriesCt++
				iter = set.At().Iterator(iter)
				samples := 0
				for iter.Next() != chunkenc.ValNone {
					samples++
				}
				require.Equal(t, tc.nSamples, samples)
			}

		}
		require.Equal(t, expectedSeries, seriesCt)
	}
}
