package stats_test

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReader_ReadRecordBatch(t *testing.T) {
	t.Run("returns an error when the reader is not open", func(t *testing.T) {
		r := stats.NewReader(stats.ReaderOptions{})

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reader not opened")
	})

	for _, tc := range []struct {
		name      string
		batchSize int
	}{
		{name: "returns an error when the batch size is zero", batchSize: 0},
		{name: "returns an error when the batch size is negative", batchSize: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := openReader(t, buildSection(t, stats.Stat{ObjectPath: "/obj"}))

			batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), tc.batchSize)
			require.Nil(t, batch)
			require.EqualError(t, err, fmt.Sprintf("reading record batch: batch size must be positive, got %d", tc.batchSize))
		})
	}

	t.Run("returns an error when the allocator is nil", func(t *testing.T) {
		r := openReader(t, buildSection(t, stats.Stat{ObjectPath: "/obj"}))

		batch, err := r.ReadRecordBatch(t.Context(), nil, 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reading record batch: allocator is nil")
	})

	t.Run("returns the values of every column of the section", func(t *testing.T) {
		r := openReader(t, buildSection(t, stats.Stat{
			ObjectPath:       "/obj",
			SectionIndex:     3,
			SortSchema:       "label:service_name",
			Labels:           map[string]string{"service_name": "svc"},
			MinTimestamp:     100,
			MaxTimestamp:     200,
			RowCount:         5,
			UncompressedSize: 50,
		}))

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)
		require.Equal(t, "/obj", string(batch.ObjectPath.Get(0)))
		require.Equal(t, int64(3), batch.SectionIndex.Get(0))
		require.Equal(t, "label:service_name", string(batch.SortSchema.Get(0)))
		require.Equal(t, int64(0), batch.ShardBucket.Get(0))
		require.Equal(t, int64(100), batch.MinTimestamp.Get(0))
		require.Equal(t, int64(200), batch.MaxTimestamp.Get(0))
		require.Equal(t, int64(5), batch.RowCount.Get(0))
		require.Equal(t, int64(50), batch.UncompressedSize.Get(0))
		require.Len(t, batch.Labels, 1)
		require.Equal(t, "service_name", batch.Labels[0].Name)
		require.Equal(t, "svc", string(batch.Labels[0].Values.Get(0)))
	})

	t.Run("returns a zero value that the section stores as a value, not as null", func(t *testing.T) {
		r := openReader(t, buildSection(t, stats.Stat{}))

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.False(t, batch.ObjectPath.IsNull(0))
		require.False(t, batch.SectionIndex.IsNull(0))
		require.False(t, batch.ShardBucket.IsNull(0))
		require.False(t, batch.MinTimestamp.IsNull(0))
		require.False(t, batch.MaxTimestamp.IsNull(0))
		require.False(t, batch.RowCount.IsNull(0))
		require.False(t, batch.UncompressedSize.IsNull(0))
	})

	t.Run("returns only the arrays of the projected columns", func(t *testing.T) {
		sec := buildSection(t, stats.Stat{ObjectPath: "/obj", RowCount: 5})
		r := stats.NewReader(stats.ReaderOptions{Columns: []*stats.Column{
			columnOfType(t, sec, stats.ColumnTypeRowCount),
			columnOfType(t, sec, stats.ColumnTypeObjectPath),
		}})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, int64(5), batch.RowCount.Get(0))
		require.Equal(t, "/obj", string(batch.ObjectPath.Get(0)))
		require.Nil(t, batch.SortSchema)
		require.Nil(t, batch.SectionIndex)
		require.Empty(t, batch.Labels)
	})

	t.Run("returns batches of one to batch size rows, then nil and io.EOF on every later call", func(t *testing.T) {
		const batchSize = 2
		var section []stats.Stat
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			section = append(section, stats.Stat{ObjectPath: path})
		}
		r := openReader(t, buildSection(t, section...))
		alloc := newAllocator(t)

		var total int
		for total < len(section) {
			alloc.Reclaim()
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.NoError(t, err)
			require.GreaterOrEqual(t, batch.Rows, 1)
			require.LessOrEqual(t, batch.Rows, batchSize)
			total += batch.Rows
		}
		require.Equal(t, len(section), total)

		for range 2 {
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.Nil(t, batch)
			require.Equal(t, io.EOF, err)
		}
	})

	t.Run("maps the arrays to the columns that the options held, when the caller changes its slice later", func(t *testing.T) {
		sec := buildSection(t, stats.Stat{ObjectPath: "/obj", RowCount: 5})
		columns := []*stats.Column{
			columnOfType(t, sec, stats.ColumnTypeRowCount),
			columnOfType(t, sec, stats.ColumnTypeObjectPath),
		}
		r := stats.NewReader(stats.ReaderOptions{Columns: columns})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		columns[0], columns[1] = columns[1], columns[0]

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, int64(5), batch.RowCount.Get(0))
		require.Equal(t, "/obj", string(batch.ObjectPath.Get(0)))
	})
}

// newAllocator returns an allocator that the test frees when it ends.
func newAllocator(t *testing.T) *memory.Allocator {
	t.Helper()

	alloc := memory.NewAllocator(nil)
	t.Cleanup(alloc.Free)
	return alloc
}

// buildSection returns a stats section that holds rows. A row with no sort
// schema gets "label:service_name".
func buildSection(t *testing.T, rows ...stats.Stat) *stats.Section {
	t.Helper()

	const sortSchema = "label:service_name"
	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000), testLayout(sortSchema))
	b.SetTenant("test-tenant")
	for _, row := range rows {
		if row.SortSchema == "" {
			row.SortSchema = sortSchema
		}
		b.Append(row)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err := stats.Open(t.Context(), s)
		require.NoError(t, err)
		return sec
	}
	require.FailNow(t, "the object has no stats section")
	return nil
}

// openReader returns an open Reader of all the columns of sec.
func openReader(t *testing.T, sec *stats.Section) *stats.Reader {
	t.Helper()

	r := stats.NewReader(stats.ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

// columnOfType returns the column of sec with the given type.
func columnOfType(t *testing.T, sec *stats.Section, columnType stats.ColumnType) *stats.Column {
	t.Helper()

	for _, c := range sec.Columns() {
		if c.Type == columnType {
			return c
		}
	}
	require.FailNow(t, "the section has no column of the type", columnType.String())
	return nil
}
