package logs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestCompareForSortSchema(t *testing.T) {
	schemas := []streams.SortKey{
		{}, // [0] unused
		{ShardBucket: 1, SchemaKey: "a", Hash: 2},
		{ShardBucket: 0, SchemaKey: "z", Hash: 9},
		{ShardBucket: 1, SchemaKey: "a", Hash: 1},
	}
	less := CompareByStreamSchema(schemas)
	row := func(id, ts int64) result.Result[dataset.Row] {
		return result.Value(dataset.Row{
			Values: []dataset.Value{dataset.Int64Value(id), dataset.Int64Value(ts)},
		})
	}

	// shard 0 (id 2) precedes shard 1
	require.True(t, less(row(2, 1), row(3, 1)))
	// same shard+key: lower hash (id 3) precedes higher hash (id 1)
	require.True(t, less(row(3, 1), row(1, 1)))
	require.False(t, less(row(1, 1), row(3, 1)))
	// same stream: later timestamp first
	require.True(t, less(row(1, 20), row(1, 10)))
	// sentinel never wins
	require.False(t, less(row(math.MaxInt64, 0), row(2, 1)))
	require.True(t, less(row(2, 1), row(math.MaxInt64, 0)))
}

func TestStreamSortCompare(t *testing.T) {
	a := streams.SortKey{ShardBucket: 0, SchemaKey: "z", Hash: 9}
	b := streams.SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 1}
	c := streams.SortKey{ShardBucket: 1, SchemaKey: "a", Hash: 2}

	require.Negative(t, a.Compare(b))
	require.Negative(t, b.Compare(c))
	require.Zero(t, a.Compare(a))
	require.Positive(t, c.Compare(b))
}
