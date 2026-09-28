package streams

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestNewSortKey(t *testing.T) {
	streamLabels := labels.FromStrings("app", "api", "cluster", "prod")
	hash := labels.StableHash(streamLabels)

	key := NewSortKey(streamLabels, "fake-schema-key")

	require.Equal(t, ShardBucketFromHash(hash), key.ShardBucket)
	require.Equal(t, "fake-schema-key", key.SchemaKey)
	require.Equal(t, hash, key.Hash)
	require.Equal(t, streamLabels, key.Labels)
}

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
