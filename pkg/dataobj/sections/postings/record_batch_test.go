package postings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
)

func TestNewRecordBatch(t *testing.T) {
	t.Run("sets the field of the type of every column", func(t *testing.T) {
		columns := []*Column{
			{Type: ColumnTypeKind},
			{Type: ColumnTypeObjectPath},
			{Type: ColumnTypeShardBuckets},
			{Type: ColumnTypeSectionIndex},
			{Type: ColumnTypeColumnName},
			{Type: ColumnTypeLabelValue},
			{Type: ColumnTypeBloomFilter},
			{Type: ColumnTypeStreamIDBitmap},
			{Type: ColumnTypeUncompressedSize},
			{Type: ColumnTypeMinTimestamp},
			{Type: ColumnTypeMaxTimestamp},
			{Type: ColumnTypeMinShardBucket},
			{Type: ColumnTypeMaxShardBucket},
		}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			columnartest.Array(t, types.KindInt64, nil, 1),
			columnartest.Array(t, types.KindUTF8, nil, "object-path"),
			columnartest.Array(t, types.KindInt64, nil, 3),
			columnartest.Array(t, types.KindInt64, nil, 4),
			columnartest.Array(t, types.KindUTF8, nil, "column-name"),
			columnartest.Array(t, types.KindUTF8, nil, "label-value"),
			columnartest.Array(t, types.KindUTF8, nil, "bloom-filter"),
			columnartest.Array(t, types.KindUTF8, nil, "stream-id-bitmap"),
			columnartest.Array(t, types.KindInt64, nil, 9),
			columnartest.Array(t, types.KindInt64, nil, 10),
			columnartest.Array(t, types.KindInt64, nil, 11),
			columnartest.Array(t, types.KindInt64, nil, 12),
			columnartest.Array(t, types.KindInt64, nil, 13),
		})

		batch, err := newRecordBatch(columns, rb)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)

		require.Equal(t, int64(1), batch.Kind.Get(0))
		require.Equal(t, "object-path", string(batch.ObjectPath.Get(0)))
		require.Equal(t, int64(3), batch.ShardBuckets.Get(0))
		require.Equal(t, int64(4), batch.SectionIndex.Get(0))
		require.Equal(t, "column-name", string(batch.ColumnName.Get(0)))
		require.Equal(t, "label-value", string(batch.LabelValue.Get(0)))
		require.Equal(t, "bloom-filter", string(batch.BloomFilter.Get(0)))
		require.Equal(t, "stream-id-bitmap", string(batch.StreamIDBitmap.Get(0)))
		require.Equal(t, int64(9), batch.UncompressedSize.Get(0))
		require.Equal(t, int64(10), batch.MinTimestamp.Get(0))
		require.Equal(t, int64(11), batch.MaxTimestamp.Get(0))
		require.Equal(t, int64(12), batch.MinShardBucket.Get(0))
		require.Equal(t, int64(13), batch.MaxShardBucket.Get(0))
	})

	t.Run("leaves the field of a column that it does not hold nil", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{columnartest.Array(t, types.KindUTF8, nil, "env")})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeColumnName}}, rb)
		require.NoError(t, err)
		require.NotNil(t, batch.ColumnName)
		require.Nil(t, batch.Kind)
		require.Nil(t, batch.ObjectPath)
		require.Nil(t, batch.LabelValue)
		require.Nil(t, batch.BloomFilter)
		require.Nil(t, batch.StreamIDBitmap)
		require.Nil(t, batch.MinTimestamp)
	})

	t.Run("keeps the null elements of an array", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{columnartest.Array(t, types.KindUTF8, nil, nil, "prod")})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeLabelValue}}, rb)
		require.NoError(t, err)
		require.True(t, batch.LabelValue.IsNull(0))
		require.False(t, batch.LabelValue.IsNull(1))
	})

	t.Run("returns an error when the batch has no row", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 0, []columnar.Array{columnartest.Array(t, types.KindInt64, nil)})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeKind}}, rb)
		require.Nil(t, batch)
		require.EqualError(t, err, "record batch has no row")
	})

	t.Run("returns an error when the number of arrays is not the number of columns", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{columnartest.Array(t, types.KindInt64, nil, 1)})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeKind}, {Type: ColumnTypeSectionIndex}}, rb)
		require.Nil(t, batch)
		require.EqualError(t, err, "record batch has 1 columns, want 2")
	})

	t.Run("returns an error when an array does not have one element for each row", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{columnartest.Array(t, types.KindInt64, nil, 1)})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeKind}}, rb)
		require.Nil(t, batch)
		require.EqualError(t, err, "array of kind has 1 elements, want 2")
	})

	t.Run("returns an error when an array has the wrong type", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			column ColumnType
			array  columnar.Array
			want   string
		}{
			{"returns an error for a string array in a number column", ColumnTypeKind, columnartest.Array(t, types.KindUTF8, nil, "a"), "invalid array type *columnar.UTF8 for kind"},
			{"returns an error for a number array in a string column", ColumnTypeObjectPath, columnartest.Array(t, types.KindInt64, nil, 1), "invalid array type *columnar.Number[int64] for object_path"},
			{"returns an error for an unsigned array in a number column", ColumnTypeSectionIndex, columnartest.Array(t, types.KindUint64, nil, uint64(1)), "invalid array type *columnar.Number[uint64] for section_index"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{tc.array})

				batch, err := newRecordBatch([]*Column{{Type: tc.column}}, rb)
				require.Nil(t, batch)
				require.EqualError(t, err, tc.want)
			})
		}
	})

	t.Run("returns an error when two columns have the same type", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			column ColumnType
			kind   types.Kind
			want   string
		}{
			{"returns an error for two string columns", ColumnTypeObjectPath, types.KindUTF8, "more than one object_path column"},
			{"returns an error for two number columns", ColumnTypeMinTimestamp, types.KindInt64, "more than one min_timestamp column"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var first, second any = "a", "b"
				if tc.kind == types.KindInt64 {
					first, second = 1, 2
				}
				rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
					columnartest.Array(t, tc.kind, nil, first),
					columnartest.Array(t, tc.kind, nil, second),
				})

				batch, err := newRecordBatch([]*Column{{Type: tc.column}, {Type: tc.column}}, rb)
				require.Nil(t, batch)
				require.EqualError(t, err, tc.want)
			})
		}
	})

	t.Run("returns an error when a column has an unknown type", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{columnartest.Array(t, types.KindInt64, nil, 1)})

		batch, err := newRecordBatch([]*Column{{Type: ColumnTypeInvalid}}, rb)
		require.Nil(t, batch)
		require.EqualError(t, err, "unsupported column type invalid")
	})
}
