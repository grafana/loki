package metastore

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestAddSectionDescriptors_Merge(t *testing.T) {
	t.Parallel()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "stream_id.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "stream_id_ref.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "min_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: "max_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: "row_count.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "uncompressed_size.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	sectionB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDRefB := array.NewInt64Builder(memory.DefaultAllocator)
	minTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	maxTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	rowCountB := array.NewInt64Builder(memory.DefaultAllocator)
	sizeB := array.NewInt64Builder(memory.DefaultAllocator)

	// Two rows pointing to the same (path,section) but different stream_id_ref and different ranges.
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(10 * time.Minute)
	t2 := t0.Add(30 * time.Minute)
	t3 := t0.Add(40 * time.Minute)

	pathB.AppendValues([]string{"obj-A", "obj-A"}, nil)
	sectionB.AppendValues([]int64{7, 7}, nil)
	streamIDB.AppendValues([]int64{111, 111}, nil)
	streamIDRefB.AppendValues([]int64{10, 11}, nil)
	minTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(t1.UnixNano()), arrow.Timestamp(t0.UnixNano())}, nil)
	maxTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(t2.UnixNano()), arrow.Timestamp(t3.UnixNano())}, nil)
	rowCountB.AppendValues([]int64{3, 5}, nil)
	sizeB.AppendValues([]int64{100, 250}, nil)

	cols := []arrow.Array{
		pathB.NewArray(),
		sectionB.NewArray(),
		streamIDB.NewArray(),
		streamIDRefB.NewArray(),
		minTsB.NewArray(),
		maxTsB.NewArray(),
		rowCountB.NewArray(),
		sizeB.NewArray(),
	}

	rec := array.NewRecordBatch(schema, cols, 2)

	got := map[SectionKey]*DataobjSectionDescriptor{}
	require.NoError(t, addSectionDescriptors(rec, got, nil))
	require.Len(t, got, 1)

	desc := got[SectionKey{ObjectPath: "obj-A", SectionIdx: 7}]
	require.NotNil(t, desc)
	require.ElementsMatch(t, []int64{10, 11}, desc.StreamIDs)
	require.Equal(t, 8, desc.RowCount)
	require.Equal(t, int64(350), desc.Size)
	require.Equal(t, t0.UnixNano(), desc.Start.UnixNano())
	require.Equal(t, t3.UnixNano(), desc.End.UnixNano())
}
