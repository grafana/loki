package stats

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
)

func TestNewRecordBatch(t *testing.T) {
	t.Run("stores each array in the field of its column", func(t *testing.T) {
		cols := []*Column{
			{Type: ColumnTypeObjectPath},
			{Type: ColumnTypeSectionIndex},
			{Type: ColumnTypeSortSchema},
			{Type: ColumnTypeShardBucket},
			{Type: ColumnTypeMinTimestamp},
			{Type: ColumnTypeMaxTimestamp},
			{Type: ColumnTypeRowCount},
			{Type: ColumnTypeUncompressedSize},
		}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			utf8Array(t, "/obj"), int64Array(t, 1), utf8Array(t, "label:app"), int64Array(t, 2),
			int64Array(t, 3), int64Array(t, 4), int64Array(t, 5), int64Array(t, 6),
		})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)
		require.Equal(t, "/obj", string(batch.ObjectPath.Get(0)))
		require.Equal(t, int64(1), batch.SectionIndex.Get(0))
		require.Equal(t, "label:app", string(batch.SortSchema.Get(0)))
		require.Equal(t, int64(2), batch.ShardBucket.Get(0))
		require.Equal(t, int64(3), batch.MinTimestamp.Get(0))
		require.Equal(t, int64(4), batch.MaxTimestamp.Get(0))
		require.Equal(t, int64(5), batch.RowCount.Get(0))
		require.Equal(t, int64(6), batch.UncompressedSize.Get(0))
		require.Empty(t, batch.Labels)
	})

	t.Run("leaves the fields of the columns that are not projected nil", func(t *testing.T) {
		cols := []*Column{{Type: ColumnTypeRowCount}}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{int64Array(t, 5)})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.NotNil(t, batch.RowCount)
		require.Nil(t, batch.ObjectPath)
		require.Nil(t, batch.SortSchema)
		require.Nil(t, batch.SectionIndex)
		require.Nil(t, batch.ShardBucket)
		require.Nil(t, batch.MinTimestamp)
		require.Nil(t, batch.MaxTimestamp)
		require.Nil(t, batch.UncompressedSize)
	})

	t.Run("keeps a null element null", func(t *testing.T) {
		cols := []*Column{{Type: ColumnTypeObjectPath}, {Type: ColumnTypeRowCount}, {Type: ColumnTypeLabel, Name: "app"}}
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			utf8Array(t, "/a", nil), int64Array(t, nil, 7), utf8Array(t, nil, ""),
		})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.False(t, batch.ObjectPath.IsNull(0))
		require.True(t, batch.ObjectPath.IsNull(1))
		require.True(t, batch.RowCount.IsNull(0))
		require.False(t, batch.RowCount.IsNull(1))
		require.True(t, batch.Labels[0].Values.IsNull(0))
		require.False(t, batch.Labels[0].Values.IsNull(1), "an empty label value is not null")
	})

	t.Run("keeps the label columns in the order of the columns", func(t *testing.T) {
		cols := []*Column{
			{Type: ColumnTypeLabel, Name: "service_name"},
			{Type: ColumnTypeObjectPath},
			{Type: ColumnTypeLabel, Name: "namespace"},
			{Type: ColumnTypeLabel, Name: "cluster"},
		}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{utf8Array(t, "a"), utf8Array(t, "/obj"), utf8Array(t, "b"), utf8Array(t, "c")})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.Len(t, batch.Labels, 3)
		for i, want := range []struct{ name, value string }{
			{"service_name", "a"},
			{"namespace", "b"},
			{"cluster", "c"},
		} {
			require.Equal(t, want.name, batch.Labels[i].Name)
			require.Equal(t, want.value, string(batch.Labels[i].Values.Get(0)))
		}
	})

	t.Run("keeps a label name that contains a dot", func(t *testing.T) {
		cols := []*Column{{Type: ColumnTypeLabel, Name: "app.kubernetes.io/name"}}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{utf8Array(t, "loki")})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.Len(t, batch.Labels, 1)
		require.Equal(t, "app.kubernetes.io/name", batch.Labels[0].Name)
		require.Equal(t, "loki", string(batch.Labels[0].Values.Get(0)))
	})

	t.Run("keeps two label columns that have the same name", func(t *testing.T) {
		cols := []*Column{{Type: ColumnTypeLabel, Name: "app"}, {Type: ColumnTypeLabel, Name: "app"}}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{utf8Array(t, "a"), utf8Array(t, "b")})

		batch, err := newRecordBatch(cols, rb)
		require.NoError(t, err)
		require.Len(t, batch.Labels, 2)
		require.Equal(t, "a", string(batch.Labels[0].Values.Get(0)))
		require.Equal(t, "b", string(batch.Labels[1].Values.Get(0)))
	})

	for _, tc := range []struct {
		name    string
		cols    []*Column
		rows    int64
		arrays  []columnar.Array
		wantErr string
	}{
		{
			name:    "returns an error when the batch has no row",
			cols:    []*Column{{Type: ColumnTypeObjectPath}},
			rows:    0,
			arrays:  []columnar.Array{utf8Array(t)},
			wantErr: "record batch has no row",
		},
		{
			name:    "returns an error when the batch has fewer arrays than columns",
			cols:    []*Column{{Type: ColumnTypeObjectPath}, {Type: ColumnTypeRowCount}},
			rows:    1,
			arrays:  []columnar.Array{utf8Array(t, "/a")},
			wantErr: "record batch has 1 columns, want 2",
		},
		{
			name:    "returns an error when an array has another length than the batch",
			cols:    []*Column{{Type: ColumnTypeObjectPath}, {Type: ColumnTypeRowCount}},
			rows:    2,
			arrays:  []columnar.Array{utf8Array(t, "/a", "/b"), int64Array(t, 1)},
			wantErr: "array of row_count has 1 elements, want 2",
		},
		{
			name:    "returns an error when an array has the wrong type",
			cols:    []*Column{{Type: ColumnTypeObjectPath}},
			rows:    1,
			arrays:  []columnar.Array{int64Array(t, 1)},
			wantErr: "invalid array type *columnar.Number[int64] for object_path",
		},
		{
			name:    "returns an error when a number array has the wrong type",
			cols:    []*Column{{Type: ColumnTypeRowCount}},
			rows:    1,
			arrays:  []columnar.Array{utf8Array(t, "x")},
			wantErr: "invalid array type *columnar.UTF8 for row_count",
		},
		{
			name:    "returns an error when a label array has the wrong type",
			cols:    []*Column{{Type: ColumnTypeLabel, Name: "app"}},
			rows:    1,
			arrays:  []columnar.Array{int64Array(t, 1)},
			wantErr: "invalid array type *columnar.Number[int64] for label",
		},
		{
			name:    "returns an error when two string columns have the same type",
			cols:    []*Column{{Type: ColumnTypeObjectPath}, {Type: ColumnTypeObjectPath}},
			rows:    1,
			arrays:  []columnar.Array{utf8Array(t, "/a"), utf8Array(t, "/b")},
			wantErr: "more than one object_path column",
		},
		{
			name:    "returns an error when two number columns have the same type",
			cols:    []*Column{{Type: ColumnTypeMinTimestamp}, {Type: ColumnTypeMinTimestamp}},
			rows:    1,
			arrays:  []columnar.Array{int64Array(t, 1), int64Array(t, 2)},
			wantErr: "more than one min_timestamp column",
		},
		{
			name:    "returns an error for a column of an unknown type",
			cols:    []*Column{{Type: ColumnTypeInvalid}},
			rows:    1,
			arrays:  []columnar.Array{utf8Array(t, "/a")},
			wantErr: "unsupported column type invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch, err := newRecordBatch(tc.cols, columnar.NewRecordBatch(nil, tc.rows, tc.arrays))
			require.Nil(t, batch)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}
