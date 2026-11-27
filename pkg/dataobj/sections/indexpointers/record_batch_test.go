package indexpointers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
)

func TestNewRecordBatch(t *testing.T) {
	var (
		paths    = columnartest.Array(t, types.KindUTF8, nil, "path1", nil, "path3")
		numbers  = columnartest.Array(t, types.KindInt64, nil, 10, nil, 30)
		bounds   = columnartest.Array(t, types.KindUTF8, nil, "min", "max", nil)
		uint64s  = columnartest.Array(t, types.KindUint64, nil, 1, 2, 3)
		shorter  = columnartest.Array(t, types.KindInt64, nil, 10, 20)
		newBatch = func(rows int64, arrays ...columnar.Array) *columnar.RecordBatch {
			return columnar.NewRecordBatch(nil, rows, arrays)
		}
	)

	t.Run("sets the field of every column type to the array of that column", func(t *testing.T) {
		columns := []*Column{
			{Type: ColumnTypePath},
			{Type: ColumnTypeMinTimestamp},
			{Type: ColumnTypeMaxTimestamp},
			{Type: ColumnTypeRunID},
			{Type: ColumnTypeShardBuckets},
			{Type: ColumnTypeRunLength},
			{Type: ColumnTypeMinPostingKey},
			{Type: ColumnTypeMaxPostingKey},
		}
		arrays := []columnar.Array{
			columnartest.Array(t, types.KindUTF8, nil, "path1", "path2", "path3"),
			columnartest.Array(t, types.KindInt64, nil, 11, 12, 13),
			columnartest.Array(t, types.KindInt64, nil, 21, 22, 23),
			columnartest.Array(t, types.KindInt64, nil, 31, 32, 33),
			columnartest.Array(t, types.KindInt64, nil, 41, 42, 43),
			columnartest.Array(t, types.KindInt64, nil, 51, 52, 53),
			columnartest.Array(t, types.KindUTF8, nil, "min1", "min2", "min3"),
			columnartest.Array(t, types.KindUTF8, nil, "max1", "max2", "max3"),
		}

		batch, err := newRecordBatch(columns, newBatch(3, arrays...))
		require.NoError(t, err)
		require.Equal(t, 3, batch.Rows)
		require.Same(t, arrays[0], batch.Path)
		require.Same(t, arrays[1], batch.MinTimestamp)
		require.Same(t, arrays[2], batch.MaxTimestamp)
		require.Same(t, arrays[3], batch.RunID)
		require.Same(t, arrays[4], batch.ShardBuckets)
		require.Same(t, arrays[5], batch.RunLength)
		require.Same(t, arrays[6], batch.MinPostingKey)
		require.Same(t, arrays[7], batch.MaxPostingKey)
	})

	t.Run("keeps the null elements of an array", func(t *testing.T) {
		batch, err := newRecordBatch([]*Column{{Type: ColumnTypePath}}, newBatch(3, paths))
		require.NoError(t, err)
		require.False(t, batch.Path.IsNull(0))
		require.True(t, batch.Path.IsNull(1))
		require.False(t, batch.Path.IsNull(2))
	})

	t.Run("leaves the field of a column that is not projected nil", func(t *testing.T) {
		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeMinTimestamp}}, newBatch(3, numbers))
		require.NoError(t, err)
		require.NotNil(t, batch.MinTimestamp)
		require.Nil(t, batch.Path)
		require.Nil(t, batch.MaxTimestamp)
		require.Nil(t, batch.RunID)
		require.Nil(t, batch.ShardBuckets)
		require.Nil(t, batch.RunLength)
		require.Nil(t, batch.MinPostingKey)
		require.Nil(t, batch.MaxPostingKey)
	})

	for _, tt := range []struct {
		name    string
		columns []*Column
		batch   *columnar.RecordBatch
		wantErr string
	}{
		{
			name:    "returns an error when the batch has no row",
			columns: []*Column{{Type: ColumnTypePath}},
			batch:   newBatch(0, columnartest.Array(t, types.KindUTF8, nil)),
			wantErr: "record batch has no row",
		},
		{
			name:    "returns an error when the batch has more arrays than columns",
			columns: []*Column{{Type: ColumnTypePath}},
			batch:   newBatch(3, paths, numbers),
			wantErr: "record batch has 2 columns, want 1",
		},
		{
			name:    "returns an error when an array has a different length than the batch",
			columns: []*Column{{Type: ColumnTypePath}, {Type: ColumnTypeMinTimestamp}},
			batch:   newBatch(3, paths, shorter),
			wantErr: "array of min_timestamp has 2 elements, want 3",
		},
		{
			name:    "returns an error when the array of a string column is not a string array",
			columns: []*Column{{Type: ColumnTypePath}},
			batch:   newBatch(3, numbers),
			wantErr: "invalid array type *columnar.Number[int64] for path",
		},
		{
			name:    "returns an error when the array of a number column is not an int64 array",
			columns: []*Column{{Type: ColumnTypeRunID}},
			batch:   newBatch(3, uint64s),
			wantErr: "invalid array type *columnar.Number[uint64] for run_id",
		},
		{
			name:    "returns an error when two columns of strings have the same type",
			columns: []*Column{{Type: ColumnTypeMinPostingKey}, {Type: ColumnTypeMinPostingKey}},
			batch:   newBatch(3, bounds, bounds),
			wantErr: "more than one min_posting_key column",
		},
		{
			name:    "returns an error when two columns of numbers have the same type",
			columns: []*Column{{Type: ColumnTypeMinTimestamp}, {Type: ColumnTypeMinTimestamp}},
			batch:   newBatch(3, numbers, numbers),
			wantErr: "more than one min_timestamp column",
		},
		{
			name:    "returns an error when a column type is not supported",
			columns: []*Column{{Type: ColumnTypeInvalid}},
			batch:   newBatch(3, numbers),
			wantErr: "unsupported column type invalid",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			batch, err := newRecordBatch(tt.columns, tt.batch)
			require.Nil(t, batch)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
