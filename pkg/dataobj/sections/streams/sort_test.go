package streams

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestSortKeyCompare(t *testing.T) {
	tests := []struct {
		name string
		a    SortKey
		b    SortKey
		want int
	}{
		{
			name: "shard bucket takes precedence",
			a:    SortKey{ShardBucket: 1, SchemaKey: "z", Hash: 20},
			b:    SortKey{ShardBucket: 2, SchemaKey: "a", Hash: 10},
			want: -1,
		},
		{
			name: "schema key takes precedence over hash",
			a:    SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 20},
			b:    SortKey{ShardBucket: 1, SchemaKey: "b", Hash: 10},
			want: -1,
		},
		{
			name: "hash breaks equal schema keys",
			a:    SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 10},
			b:    SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 20},
			want: -1,
		},
		{
			name: "equal prefix",
			a:    SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 10},
			b:    SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 10},
			want: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.a.Compare(test.b))
			require.Equal(t, -test.want, test.b.Compare(test.a))
		})
	}
}

func TestCompareSortKeyUsesLabelsAsTieBreaker(t *testing.T) {
	prefix := SortKey{ShardBucket: 1, SchemaKey: "prod", Hash: 42}
	a := prefix
	a.Labels = labels.FromStrings("app", "api")
	b := prefix
	b.Labels = labels.FromStrings("app", "worker")

	require.Zero(t, a.Compare(b))
	require.Negative(t, CompareSortKey(a, b))
	require.Positive(t, CompareSortKey(b, a))
	require.Zero(t, CompareSortKey(a, a))
}

func TestSortPrefixBounds(t *testing.T) {
	largest, err := ShardLayoutFromBuckets(1 << maxShardBits)
	require.NoError(t, err)
	streamLabels := []labels.Labels{
		labels.EmptyLabels(),
		labels.FromStrings("app", "a"),
		labels.FromStrings("app", "\xff\xff\xff"),
	}

	t.Run("min sort prefix is at or below every stream prefix", func(t *testing.T) {
		for _, ls := range streamLabels {
			prefix := largest.NewSortKey(ls, ls.Get("app")).Prefix()
			require.LessOrEqual(t, MinSortPrefix().Compare(prefix), 0)
		}
	})

	t.Run("sort prefix limit is above the highest bucket of the largest layout", func(t *testing.T) {
		highest := SortPrefix{ShardBucket: largest.Buckets() - 1, SchemaKey: "\xff\xff\xff\xff"}
		require.Positive(t, SortPrefixLimit().Compare(highest))
		for _, ls := range streamLabels {
			require.Positive(t, SortPrefixLimit().Compare(largest.NewSortKey(ls, ls.Get("app")).Prefix()))
		}
	})
}
