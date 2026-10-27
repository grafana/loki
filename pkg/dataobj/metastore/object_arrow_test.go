package metastore

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestAddSectionDescriptors_Merge(t *testing.T) {
	t.Parallel()

	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	sectionB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDRefB := array.NewInt64Builder(memory.DefaultAllocator)
	minTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	maxTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	rowCountB := array.NewInt64Builder(memory.DefaultAllocator)
	sizeB := array.NewInt64Builder(memory.DefaultAllocator)
	internalLabelsB := array.NewStringBuilder(memory.DefaultAllocator)

	// Two rows for the same (path,section) with different stream_id_ref and ranges.
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
	internalLabelsB.AppendValues([]string{"label1,label2", "label1,label2"}, nil)

	cols := []arrow.Array{
		pathB.NewArray(),
		sectionB.NewArray(),
		streamIDB.NewArray(),
		streamIDRefB.NewArray(),
		minTsB.NewArray(),
		maxTsB.NewArray(),
		rowCountB.NewArray(),
		sizeB.NewArray(),
		internalLabelsB.NewArray(),
	}

	rec := array.NewRecordBatch(postingsResultSchema, cols, 2)

	got := map[SectionKey]*DataobjSectionDescriptor{}
	require.NoError(t, addSectionDescriptors(rec, got))
	require.Len(t, got, 1)

	desc := got[SectionKey{ObjectPath: "obj-A", SectionIdx: 7}]
	require.NotNil(t, desc)
	require.Equal(t, []int64{10, 11}, desc.StreamIDs)
	require.Equal(t, 8, desc.RowCount)
	require.Equal(t, int64(350), desc.Size)
	require.Equal(t, t0.UnixNano(), desc.Start.UnixNano())
	require.Equal(t, t3.UnixNano(), desc.End.UnixNano())
	require.ElementsMatch(t, []string{"label1", "label2"}, desc.AmbiguousPredicates)
}

func TestResolvedRowsFromRecordBatch(t *testing.T) {
	t.Parallel()

	t.Run("decodes a row with every field set", func(t *testing.T) {
		names := "label1,label2"
		rows, err := resolvedRowsFromRecordBatch(newResultBatch(&names))
		require.NoError(t, err)
		require.Equal(t, []resolvedRow{{
			objectPath:       "obj-A",
			sectionIndex:     7,
			streamID:         10,
			minTimestamp:     100,
			maxTimestamp:     200,
			rowCount:         3,
			uncompressedSize: 250,
			ambiguousNames:   []string{"label1", "label2"},
		}}, rows)
	})

	t.Run("decodes a null ambiguous names value as no names", func(t *testing.T) {
		rows, err := resolvedRowsFromRecordBatch(newResultBatch(nil))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Nil(t, rows[0].ambiguousNames)
	})

	t.Run("round trips rows through sectionResultsToRecordBatch", func(t *testing.T) {
		rows := []resolvedRow{
			{objectPath: "obj-A", sectionIndex: 1, streamID: 2, minTimestamp: 3, maxTimestamp: 4, rowCount: 5, uncompressedSize: 6, ambiguousNames: []string{"x", "y"}},
			{objectPath: "obj-B", sectionIndex: 7, streamID: 8, minTimestamp: 9, maxTimestamp: 10, rowCount: 11, uncompressedSize: 12},
			{objectPath: "obj-C", sectionIndex: 13, streamID: 14, minTimestamp: 15, maxTimestamp: 16, rowCount: 17, uncompressedSize: 18, ambiguousNames: []string{"z"}},
		}

		got, err := resolvedRowsFromRecordBatch(sectionResultsToRecordBatch(rows))
		require.NoError(t, err)
		require.Equal(t, rows, got)
	})

	t.Run("returns an error when the batch has an extra field", func(t *testing.T) {
		fields := append(slices.Clone(postingsResultSchema.Fields()), arrow.Field{Name: "extra.int64", Type: arrow.PrimitiveTypes.Int64})

		_, err := resolvedRowsFromRecordBatch(newEmptyBatch(fields))
		require.EqualError(t, err, "schema does not match postingsResultSchema")
	})

	t.Run("returns an error when the batch lacks a field", func(t *testing.T) {
		fields := slices.Clone(postingsResultSchema.Fields())
		fields = fields[:len(fields)-1]

		_, err := resolvedRowsFromRecordBatch(newEmptyBatch(fields))
		require.EqualError(t, err, "schema does not match postingsResultSchema")
	})

	t.Run("returns an error when a field has another type", func(t *testing.T) {
		fields := slices.Clone(postingsResultSchema.Fields())
		fields[resultColPath].Type = arrow.PrimitiveTypes.Int64

		_, err := resolvedRowsFromRecordBatch(newEmptyBatch(fields))
		require.EqualError(t, err, "schema does not match postingsResultSchema")
	})

	t.Run("returns an error when two fields of the same type swap names", func(t *testing.T) {
		fields := slices.Clone(postingsResultSchema.Fields())
		fields[resultColMinTimestamp].Name, fields[resultColMaxTimestamp].Name = fields[resultColMaxTimestamp].Name, fields[resultColMinTimestamp].Name

		_, err := resolvedRowsFromRecordBatch(newEmptyBatch(fields))
		require.EqualError(t, err, "schema does not match postingsResultSchema")
	})

	t.Run("returns an error when a field differs only in nullability", func(t *testing.T) {
		fields := slices.Clone(postingsResultSchema.Fields())
		fields[resultColAmbiguousNames].Nullable = false

		_, err := resolvedRowsFromRecordBatch(newEmptyBatch(fields))
		require.EqualError(t, err, "schema does not match postingsResultSchema")
	})
}

// newResultBatch returns a one-row batch in postingsResultSchema. A nil
// ambiguousNames becomes a null value.
func newResultBatch(ambiguousNames *string) arrow.RecordBatch {
	b := array.NewRecordBuilder(memory.DefaultAllocator, postingsResultSchema)
	defer b.Release()

	b.Field(resultColPath).(*array.StringBuilder).Append("obj-A")
	b.Field(resultColSection).(*array.Int64Builder).Append(7)
	b.Field(resultColStreamID).(*array.Int64Builder).Append(111)
	b.Field(resultColStreamIDRef).(*array.Int64Builder).Append(10)
	b.Field(resultColMinTimestamp).(*array.TimestampBuilder).Append(100)
	b.Field(resultColMaxTimestamp).(*array.TimestampBuilder).Append(200)
	b.Field(resultColRowCount).(*array.Int64Builder).Append(3)
	b.Field(resultColUncompressedSize).(*array.Int64Builder).Append(250)
	if ambiguousNames == nil {
		b.Field(resultColAmbiguousNames).(*array.StringBuilder).AppendNull()
	} else {
		b.Field(resultColAmbiguousNames).(*array.StringBuilder).Append(*ambiguousNames)
	}
	return b.NewRecordBatch()
}

// newEmptyBatch returns a batch with the given fields and no rows.
func newEmptyBatch(fields []arrow.Field) arrow.RecordBatch {
	cols := make([]arrow.Array, len(fields))
	for i, f := range fields {
		cols[i] = array.MakeArrayOfNull(memory.DefaultAllocator, f.Type, 0)
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, 0)
}
